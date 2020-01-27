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
	"os"
	"regexp"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hashicorp/terraform/helper/hashcode"
	"github.com/patrickmn/go-cache"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	// MaximumCacheValidity is the highest number of seconds that an object
	// can be considered valid.
	MaximumCacheValidity = 45
)

// A ValidityEstimator hooks into the server side, and performs estimation of
// how long responses may be stored in cache.
type ValidityEstimator interface {
	// EstimateMaxAge estimates how long a given request/response should be
	// possible to cache (in seconds).
	estimateMaxAge(fullMethod string, req interface{}, resp interface{}) (int, error)
	// UnaryServerInterceptor returns the gRPC Interceptor for Unary operations
	// that uses the EstimateMaxAge function on the request/response objects.
	UnaryServerInterceptor() grpc.UnaryServerInterceptor
	// UnaryClientInterceptor creates a gRPC Interceptor for outgoing calls,
	// and is used for capturing information needed to make estimations
	// more accurate by polling the origin server.
	UnaryClientInterceptor() grpc.UnaryClientInterceptor
}

// Verifier verifies and estimates TTL for request/response objects.
type Verifier interface {
	run()
	update(reply proto.Message) error
	estimate() (estimatedMaxAge time.Duration, verificationInterval time.Duration, err error)
	logEstimation(log *log.Logger, source string) error
	String() string
}

// ConfigurableValidityEstimator is a configurable ValidityEstimator.
type ConfigurableValidityEstimator struct {
	// We abuse the cache data structure here, s.t. it is used as a handy
	// place to store items that expire and are then garbage collected.
	verifiers *cache.Cache
	// A channel where verifiers can specify their ID as being done.
	done chan string
	// Where to log CSV records
	csvLog *log.Logger
}

// Initialize new ConfigurableValidityEstimator.
func (e *ConfigurableValidityEstimator) Initialize(csvLog *log.Logger) {
	e.verifiers = cache.New(time.Duration(MaximumCacheValidity)*time.Second, time.Duration(MaximumCacheValidity)*10*time.Second)
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
		err := verifier.update(resp.(proto.Message))
		if err != nil {
			log.Printf("Unable to update verifier %s", verifier.String())
			return -1, err
		}

		maxAge, _, err := verifier.estimate()
		if err != nil {
			return -1, err
		}

		err = verifier.logEstimation(e.csvLog, "client")
		if err != nil {
			log.Printf("Failed to log CSV %v", err)
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

		maxAgeMessage := ", but an error occurred while estimating max-age"
		maxAge, err := e.estimateMaxAge(info.FullMethod, req, resp)
		if err == nil && maxAge.Seconds() > 0 {
			grpc.SetHeader(ctx, metadata.Pairs("cache-control", fmt.Sprintf("must-revalidate, max-age=%d", int(maxAge.Seconds()))))
			maxAgeMessage = fmt.Sprintf(" and cache max-age set to %d", maxAge)
		}

		log.Printf("%s hit upstream%s", info.FullMethod, maxAgeMessage)
		return resp, nil
	}
}

func (e *ConfigurableValidityEstimator) verificationNeeded(method string, req interface{}) (bool, int) {
	// TODO Take into consideration, e.g., how often we have been asked to
	// verify this one particular method and its request. Just to filter
	// the verification process a bit, keeping the number of verifiers
	// down.

	if blacklistExpression, found := os.LookupEnv("PROXY_CACHE_BLACKLIST"); found {
		blacklisted, err := regexp.Match(blacklistExpression, []byte(method))
		if err == nil && blacklisted {
			log.Printf("Method %s blacklisted from caching.", method)
			return false, -1
		}
	}

	hash := hash(method, req)
	_, expiration, found := e.verifiers.GetWithExpiration(hash)
	if found {
		if expiration.IsZero() || time.Now().Before(expiration) {
			log.Printf("Verification of object %s = %s(%s) not needed, object not expired yet (%s)", hash, method, req, expiration)
			return false, -1
		}
		log.Printf("Object %s = %s(%s) found, but expired. Verification needed.", hash, method, req)
		return true, MaximumCacheValidity
	}
	log.Printf("Object %s = %s(%s) not found, verification needed", hash, method, req)
	return true, MaximumCacheValidity
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
			verifier, err := newVerifier(cc.Target(), method, req.(proto.Message), reply.(proto.Message), now.Add(time.Duration(expiration)*time.Second), strategy, e.csvLog, e.done)
			if err != nil {
				log.Printf("Unable to create verifier for %s(%s): %v", method, req, err)
				return err
			}

			// expiration is manually handled by our use of the "done" channel
			err = e.verifiers.Add(hash, verifier, time.Duration(0))
			if err != nil {
				log.Printf("Failed to store verifier for %s: %v", verifier.String(), err)
				return err
			}

			log.Printf("Stored %s for verification", verifier.String())
		}

		return nil
	}
}

func initializeStrategy() estimationStrategy {
	var strategy estimationStrategy

	name, found := os.LookupEnv("PROXY_ESTIMATION_STRATEGY")

	if !found {
		log.Printf("PROXY_ESTIMATION_STRATEGY not set, using simplistic")
		strategy = &simplisticStrategy{}
	} else {
		switch name {
		case "tbg1":
			strategy = &dynamicTBG1Strategy{}
		case "simplistic":
			strategy = &simplisticStrategy{}
		default:
			log.Printf("Unknown PROXY_ESTIMATION_STRATEGY=%s specified, using simplistic", name)
			strategy = &simplisticStrategy{}
		}
	}

	strategy.initialize()
	return strategy
}
