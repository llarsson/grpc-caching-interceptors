// Package server contains the server-side interceptor for caching. The
// Interceptor here estimates for how long an object should be possible
// to cache, based on how often responses to queries seem to generate
// different responses. The intended use is for a reverse proxy, or
// embedded into a process which serves data that is amenable for
// caching.
package server

import (
	"fmt"
	"log"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hashicorp/terraform/helper/hashcode"
	"github.com/patrickmn/go-cache"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Initialize new ConfigurableValidityEstimator.
func (e *ConfigurableValidityEstimator) Initialize(csvLog *log.Logger) {
	e.verifiers = cache.New(maxVerifierLifetime, time.Duration(maxVerifierLifetime)*2)
	e.done = make(chan string, 1000)
	e.csvLog = csvLog
	e.csvLog.Printf("timestamp,source,method,estimate\n")

	// clean up finished verifiers
	go func() {
		for {
			finishedVerifier := <-e.done
			log.Printf("Verifier %s finished (currently %d) in set", finishedVerifier, e.verifiers.ItemCount())
			e.verifiers.Delete(finishedVerifier)
		}
	}()
}

// estimateMaxAge estimates the cache validity of the specified
// request/response pair for the given method. The result is given
// in seconds.
func (e *ConfigurableValidityEstimator) estimateMaxAge(fullMethod string, req interface{}, resp interface{}) (time.Duration, error) {
	value, found := e.verifiers.Get(hash(fullMethod, req))

	if found {
		verifier := value.(*verifier)
		err := verifier.update(resp.(proto.Message), clientSource)
		if err != nil {
			log.Printf("Unable to update verifier %s", verifier.string())
			return -1, err
		}

		maxAge, err := verifier.estimate()
		if err != nil {
			return -1, err
		}

		return maxAge, nil
	}

	// No estimation at this time is not an error. But that means that caching
	// should not occur, either.
	return 0, nil
}

// UnaryServerInterceptor creates the server-side gRPC Unary Interceptor
// that is used to inject the cache-control header and the estimated
// maximum age of the response object.
func (e *ConfigurableValidityEstimator) UnaryServerInterceptor() grpc.UnaryServerInterceptor {

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			log.Printf("Upstream call failed with error %v", err)
			return resp, err
		}

		// Only upstream call failures constitute true errors, so we only log others.
		var maxAgeMessage string
		if e.blacklisted(info.FullMethod) {
			maxAgeMessage = fmt.Sprintf(", but method %s blacklisted from caching", info.FullMethod)
		} else {
			maxAge, err := e.estimateMaxAge(info.FullMethod, req, resp)
			if err == nil {
				ttl := int(math.Round(maxAge.Seconds()))
				grpc.SetHeader(ctx, metadata.Pairs("cache-control", fmt.Sprintf("must-revalidate, max-age=%d", ttl)))
				maxAgeMessage = fmt.Sprintf(" and cache max-age set to %d", ttl)
			} else {
				maxAgeMessage = ", but an error occurred estimating max-age"
			}
		}

		requestHash := hashcode.String((req.(proto.Message).String()))
		log.Printf("%s(%d) hit upstream%s", info.FullMethod, requestHash, maxAgeMessage)

		return resp, nil
	}
}

func (e *ConfigurableValidityEstimator) blacklisted(method string) bool {
	if blacklistExpression, found := os.LookupEnv("PROXY_CACHE_BLACKLIST"); found {
		blacklisted, err := regexp.Match(blacklistExpression, []byte(method))
		if err == nil && blacklisted {
			return true
		}
	}
	return false
}

func (e *ConfigurableValidityEstimator) verificationNeeded(method string, req interface{}) (bool, time.Duration) {
	// TODO Take into consideration, e.g., how often we have been asked to
	// verify this one particular method and its request. Just to filter
	// the verification process a bit, keeping the number of verifiers
	// down.

	if e.blacklisted(method) {
		return false, -1
	}

	hash := hash(method, req)
	_, expiration, found := e.verifiers.GetWithExpiration(hash)
	if found {
		if expiration.IsZero() || time.Now().Before(expiration) {
			return false, -1
		}
		return true, maxVerifierLifetime
	}
	return true, maxVerifierLifetime
}

func hash(method string, req interface{}) string {
	reqMessage := req.(proto.Message)
	hash := hashcode.Strings([]string{method, reqMessage.String()})

	return hash
}

// UnaryClientInterceptor catches outgoing calls and stores information
// about them to enable verification of estimated cache validity
// times.
func (e *ConfigurableValidityEstimator) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// TODO(llarsson): store headers as well
		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			log.Printf("Failure to invoke upstream %s(%s): %v", method, req, err)
			return err
		}

		if needed, expiration := e.verificationNeeded(method, req); needed {
			hash := hash(method, req)
			now := time.Now()

			strategy := initializeStrategy()
			requestMessage := req.(proto.Message)
			replyMessage := req.(proto.Message)
			verifier, err := newVerifier(cc.Target(), method, requestMessage, replyMessage, now.Add(expiration), strategy, e.csvLog, e.done)
			if err != nil {
				log.Printf("Unable to create verifier for %s(%d): %v", method, hashcode.String(requestMessage.String()), err)
				return err
			}

			// expiration is manually handled by our use of the "done" channel
			err = e.verifiers.Add(hash, verifier, time.Duration(0))
			if err != nil {
				log.Printf("Failed to store verifier for %s: %v", verifier.string(), err)
				return err
			}

			log.Printf("Stored %s for verification", verifier.string())
		}

		return nil
	}
}

func initializeStrategy() estimationStrategy {
	var strategy estimationStrategy

	proxyMaxAge, found := os.LookupEnv("PROXY_MAX_AGE")
	if !found {
		log.Printf("PROXY_MAX_AGE not found, acting in passthrough mode")
		return nil
	}

	if strings.HasPrefix(proxyMaxAge, "dynamic-") {
		dynamicStrategySpecifiers := strings.Split(proxyMaxAge, "-")
		strategyName := strings.Split(proxyMaxAge, "-")[1]
		switch strategyName {
		case "adaptive":
			alphaStr := dynamicStrategySpecifiers[2]
			alpha, err := strconv.ParseFloat(alphaStr, 64)
			if err != nil {
				log.Printf("Failed to parse alpha parameter for Adaptive strategy (%s), acting in passthrough mode", alphaStr)
				return nil
			}

			strategy = &adaptiveStrategy{alpha: alpha}
		case "updaterisk":
			rhoStr := dynamicStrategySpecifiers[2]
			rho, err := strconv.ParseFloat(rhoStr, 64)
			if err != nil {
				log.Printf("Failed to parse rho parameter for Update-risk Based strategy (%s), acting in passthrough mode", rhoStr)
				return nil
			}

			strategy = &updateRiskBasedStrategy{rho: rho}
		default:
			log.Printf("Unknown dynamic strategy (%s), acting passthrough mode", strategyName)
			return nil
		}
	} else if strings.HasPrefix(proxyMaxAge, "static-") {
		ageSpecifier := strings.Split(proxyMaxAge, "-")[1]
		maxAge, err := strconv.Atoi(ageSpecifier)
		if err != nil {
			log.Printf("Failed to parse PROXY_MAX_AGE (%s) into integer, acting in passthrough mode", ageSpecifier)
			return nil
		}
		strategy = &staticStrategy{ttl: time.Duration(maxAge) * time.Second}
	} else {
		log.Printf("Unknown value for PROXY_MAX_AGE=%s, acting in passthrough mode", proxyMaxAge)
		return nil
	}

	strategy.initialize()

	return strategy
}
