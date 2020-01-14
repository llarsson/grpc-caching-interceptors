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
	MaximumCacheValidity = 300
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
	verifiers cache.Cache
	scheduler chan int
}

// Initialize new ConfigurableValidityEstimator.
func (e *ConfigurableValidityEstimator) Initialize() {
	e.verifiers = cache.Cache{}
	e.scheduler = make(chan int)
	go func() {
		delay := 10
		for {
			select {
			case e.scheduler <- delay:
				time.Sleep(time.Duration(delay) * time.Second)
				e.verifyEstimations()
			case <-e.scheduler:
				return
			}
		}
	}()

}

// Stop the validation process. This can be done only once, as the
// validation process will not continue.
func (e *ConfigurableValidityEstimator) Stop() {
	<-e.scheduler
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

func verificationNeeded(method string, req interface{}) bool {
	return true
}

type verifierMetadata struct {
	req           interface{}
	target        string
	previousReply interface{}
}

// UnaryClientInterceptor catches outgoing calls and stores information
// about them to enable verification of estimated cache validity
// times.
func (e *ConfigurableValidityEstimator) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {

		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			return err
		}

		reqMessage := req.(proto.Message)
		hash := hashcode.Strings([]string{method, reqMessage.String()})
		md := verifierMetadata{req: req, target: cc.Target(), previousReply: reply}
		e.verifiers.Add(hash, md, MaximumCacheValidity*time.Second)

		log.Printf("Storing call to %s(%s) for estimation verification", method, reqMessage)
		return nil
	}
}

func (e *ConfigurableValidityEstimator) verifyEstimations() {
	for method, value := range e.verifiers.Items() {
		md := value.Object.(verifierMetadata)
		opts := []grpc.DialOption{grpc.WithDefaultCallOptions(), grpc.WithInsecure()}
		cc, err := grpc.Dial(md.target, opts...)
		if err != nil {
			log.Printf("Failed to dial %v", err)
			continue
		}

		var reply interface{}
		err = cc.Invoke(context.Background(), method, md.req, reply)
		if err != nil {
			log.Printf("Failed to invoke call over established connection %v", err)
		}

		log.Printf("Verified %s(%s) = %s", method, md.req.(proto.Message).String(), reply.(proto.Message).String())
	}

	// Run again in 5 seconds time.
	e.scheduler <- 5
}
