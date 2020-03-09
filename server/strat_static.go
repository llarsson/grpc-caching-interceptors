package server

import (
	"fmt"
	"log"
	"time"
)

type staticStrategy struct {
	ttl time.Duration
}

// compile-time check that we adhere to interface
var _ estimationStrategy = (*staticStrategy)(nil)

func (strat *staticStrategy) initialize() {
	log.Printf("Using static TTL=%d for all non-blacklisted responses", int(strat.ttl.Seconds()))
}

func (strat *staticStrategy) determineInterval(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	return time.Duration(-1), fmt.Errorf("Static TTL=%d strategy does not need intervals", int(strat.ttl.Seconds()))
}

func (strat *staticStrategy) determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	return strat.ttl, nil
}
