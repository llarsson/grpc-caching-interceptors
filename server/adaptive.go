package server

import (
	"log"
	"math"
	"time"
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

	estimatedTTL := strat.estimateTTL(lastModification)
	return estimatedTTL, nil
}

func (strat *adaptiveStrategy) estimateTTL(lastModification time.Time) time.Duration {
	estimatedTTL := float64(time.Now().Sub(lastModification).Nanoseconds()) * strat.alpha
	return time.Duration(int64(estimatedTTL))
}
