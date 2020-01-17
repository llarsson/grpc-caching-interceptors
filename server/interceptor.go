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
	"strconv"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hashicorp/terraform/helper/hashcode"
	"github.com/patrickmn/go-cache"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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

// ConfigurableValidityEstimator is a configurable ValidityEstimator.
type ConfigurableValidityEstimator struct {
	// We abuse the cache data structure here, s.t. it is used as a handy
	// place to store items that expire and are then garbage collected.
	verifiers *cache.Cache
	// A channel where verifiers can specify their ID as being done.
	done chan string
}

// Initialize new ConfigurableValidityEstimator.
func (e *ConfigurableValidityEstimator) Initialize() {
	e.verifiers = cache.New(time.Duration(MaximumCacheValidity)*time.Second, time.Duration(MaximumCacheValidity)*10*time.Second)
	e.done = make(chan string, 1000)

	// clean up finished verifiers
	go func() {
		for {
			finishedVerifier := <-e.done
			e.verifiers.Delete(finishedVerifier)
		}
	}()
}

// estimateMaxAge estimates the cache validity of the specified
// request/response pair for the given method. The result is given
// in seconds.
func (e *ConfigurableValidityEstimator) estimateMaxAge(fullMethod string, req interface{}, resp interface{}) (int, error) {
	value, present := os.LookupEnv("PROXY_MAX_AGE")

	if !present {
		// It is not an error to not have the proxy max age key present in environment. We just act as if we were in passthrough mode.
		return -1, nil
	}

	switch value {
	case "dynamic":
		{
			return -1, status.Errorf(codes.Unimplemented, "Dynamic validity not implemented yet")
		}
	case "passthrough":
		{
			return -1, nil
		}
	default:
		maxAge, err := strconv.Atoi(value)
		if err != nil {
			log.Printf("Failed to parse PROXY_MAX_AGE (%s) into integer", value)
			return -1, err
		}
		return maxAge, nil
	}
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

		maxAge, err := e.estimateMaxAge(info.FullMethod, req, resp)
		if err == nil && maxAge > 0 {
			grpc.SetHeader(ctx, metadata.Pairs("cache-control", fmt.Sprintf("must-revalidate, max-age=%d", maxAge)))
		}

		log.Printf("%s hit upstream and maxAge set to %d", info.FullMethod, maxAge)

		return resp, err
	}
}

func (e *ConfigurableValidityEstimator) verificationNeeded(method string, req interface{}) (bool, int) {
	// TODO Take into consideration, e.g., how often we have been asked to
	// verify this one particular method and its request. Just to filter
	// the verification process a bit, keeping the number of verifiers
	// down.

	hash := hash(method, req)
	_, expiration, found := e.verifiers.GetWithExpiration(hash)
	if found {
		if expiration.IsZero() || time.Now().Before(expiration) {
			log.Printf("Verification of object %s not needed, object not expired yet (%s)", hash, expiration)
			return false, -1
		}
		log.Printf("Object %s found, but expired. Verification needed.", hash)
		return true, 5
	}
	log.Printf("Object %s not found, verification needed", hash)
	return true, 5
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
			return err
		}

		if needed, interval := e.verificationNeeded(method, req); needed {
			hash := hash(method, req)
			expiration := time.Duration(MaximumCacheValidity) * time.Second
			now := time.Now()
			md := verifierMetadata{
				method:        method,
				req:           req,
				target:        cc.Target(),
				previousReply: reply,
				interval:      (time.Duration(interval) * time.Second),
				timestamp:     now,
				expiration:    now.Add(expiration),
			}

			verifier, err := newVerifier(md, e.done)
			if err != nil {
				log.Printf("Unable to create verifier: %v", err)
			}

			err = e.verifiers.Add(hash, verifier, expiration)
			if err != nil {
				log.Printf("Failed to store verifier: %v", err)
				return err
			}

			log.Printf("Stored %s for verification", verifier.String())
		} else {
			log.Printf("No verification for call to %s(%s) needed", method, req.(proto.Message))
		}

		return nil
	}
}

// func (e *ConfigurableValidityEstimator) verifyEstimations() {
// 	for key, value := range e.verifiers.Items() {
// 		md := value.Object.(verifierMetadata)
//
// 		// No estimation needed yet for this object
// 		if time.Now().Before(md.timestamp.Add(md.interval)) {
// 			continue
// 		}
//
// 		opts := []grpc.DialOption{grpc.WithDefaultCallOptions(), grpc.WithInsecure()}
// 		cc, err := grpc.Dial(md.target, opts...)
// 		if err != nil {
// 			log.Printf("Failed to dial %v", err)
// 			continue
// 		}
// 		defer cc.Close()
//
// 		requestMessage := md.req.(proto.Message)
// 		previousReplyMessage := md.previousReply.(proto.Message)
//
// 		reply := proto.Clone(previousReplyMessage)
// 		err = cc.Invoke(context.Background(), md.method, md.req, reply)
// 		if err != nil {
// 			log.Printf("Failed to invoke call over established connection %v", err)
// 			continue
// 		}
//
// 		verificationRequired, newInterval := verify(previousReplyMessage, reply, md.interval)
// 		log.Printf("Verified %s(%s)", md.method, requestMessage.String())
// 		if !verificationRequired {
// 			e.verifiers.Delete(key)
// 			continue
// 		}
//
// 		// FIXME(llarsson): There must be something pretty we can do here with
// 		// channels or whatnot, s.t. we can schedule these verifications
// 		// using such a primitive. It would make it all much nicer and smarter,
// 		// since we would not have to loop over the entire set and skip most of
// 		// the items every time.
//
// 		md.previousReply = reply
// 		md.interval = newInterval
// 		md.timestamp = time.Now()
// 		remaining := time.Duration((value.Expiration - time.Now().UnixNano())) * time.Nanosecond
// 		e.verifiers.Replace(key, md, remaining)
// 		log.Printf("Object %s(%s) verified again in %s, stays in memory %s", md.method, requestMessage.String(), md.interval, remaining)
// 	}
// }
//
// func verify(previousReply proto.Message, currentReply proto.Message, verificationInterval time.Duration) (bool, time.Duration) {
// 	// TODO(llarsson): actual verification and smartness goes here :)
// 	// TODO(llarsson): logic for determining if more verification is needed
// 	return true, verificationInterval
// }
//
