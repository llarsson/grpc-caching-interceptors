package server

import (
	"log"
	"math"
	"time"
)

const (
	defaultInterval = time.Duration(5 * time.Second)
)

type adaptiveStrategy struct {
	alpha float64
}

// compile-time check that we adhere to interface
var _ estimationStrategy = (*adaptiveStrategy)(nil)

func (strat *adaptiveStrategy) initialize() {
	log.Printf("Using Adaptive TTL strategy with alpha=%f", strat.alpha)
}

func (strat *adaptiveStrategy) determineInterval(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	estimate, err := lastEstimation(estimations)
	if err != nil {
		log.Printf("No previous estimations, relying on default interval")
		return defaultInterval, nil
	}

	bounded := math.Max(estimate.validity.Seconds()/2.0, defaultInterval.Seconds())

	return time.Duration(bounded) * time.Second, nil
}

func (strat *adaptiveStrategy) determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	var lastModification time.Time
	// just need the very last update, so K=1
	timestamps, updates := backwardsUpdateDistance(verifications, 1)
	if updates == 0 {
		// no value updates! use oldest known timestamp
		lastModification = (*verifications)[0].timestamp
	} else {
		// we have non-zero updates: use most recent
		lastModification = timestamps[0]
	}

	estimatedTTL := estimateTTL(lastModification, strat.alpha)
	return estimatedTTL, nil
}

func estimateTTL(lastModification time.Time, alpha float64) time.Duration {
	estimatedTTL := float64(time.Now().Sub(lastModification).Nanoseconds()) * alpha
	return time.Duration(int64(estimatedTTL))
}

func maxInt64(a int64, b int64) int64 {
	if a >= b {
		return a
	}

	return b
}
