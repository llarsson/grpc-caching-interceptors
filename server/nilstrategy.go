package server

import (
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type nilStrategy struct {
}

// compile-time check that we adhere to interface
var _ estimationStrategy = (*nilStrategy)(nil)

func (strat *nilStrategy) initialize() {

}

func (strat *nilStrategy) determineInterval(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	return 0, status.Errorf(codes.Internal, "This should never happen")
}

func (strat *nilStrategy) determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	return 0, status.Errorf(codes.Internal, "This should never happen")
}
