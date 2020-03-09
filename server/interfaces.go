package server

import (
	"time"

	"google.golang.org/grpc"
)

type estimationStrategy interface {
	initialize()
	determineInterval(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error)
	determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation, ninetyFithPercentileResponseTime time.Duration) (time.Duration, error)
}

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
