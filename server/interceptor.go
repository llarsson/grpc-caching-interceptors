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

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// A ValidityEstimator hooks into the server side, and performs estimation of
// how long responses may be stored in cache.
type ValidityEstimator interface {
	// EstimateMaxAge estimates how long a given request/response should be
	// possible to cache (in seconds).
	EstimateMaxAge(fullMethod string, req interface{}, resp interface{}) (int, error)
	// UnaryServerInterceptor returns the gRPC Interceptor for Unary operations
	// that uses the EstimateMaxAge function on the request/response objects.
	UnaryServerInterceptor() grpc.UnaryServerInterceptor
}

// ConfigurableValidityEstimator is a configurable ValidityEstimator.
type ConfigurableValidityEstimator struct {
}

// EstimateMaxAge estimates the cache validity of the specified
// request/response pair for the given method. The result is given
// in seconds.
func (e *ConfigurableValidityEstimator) EstimateMaxAge(fullMethod string, req interface{}, resp interface{}) (int, error) {
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

		maxAge, err := e.EstimateMaxAge(info.FullMethod, req, resp)
		if err == nil && maxAge > 0 {
			grpc.SetHeader(ctx, metadata.Pairs("cache-control", fmt.Sprintf("must-revalidate, max-age=%d", maxAge)))
		}

		log.Printf("%s hit upstream and maxAge set to %d", info.FullMethod, maxAge)

		return resp, err
	}
}
