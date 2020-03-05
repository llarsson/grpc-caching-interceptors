package server

import (
	"log"
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
	return time.Duration(5 * time.Second), nil
}

func (strat *adaptiveStrategy) determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	var lastModification time.Time
	// just need the very last update, so K=1
	timestamps, updates := BackwardsUpdateDistance(verifications, 1)
	if updates == 0 {
		// no value updates! use oldest known timestamp
		lastModification = (*verifications)[0].timestamp
	} else {
		// we have non-zero updates: use most recent
		lastModification = timestamps[0]
	}

	estimatedTTL := float64(time.Now().Sub(lastModification).Nanoseconds()) * strat.alpha

	return time.Duration(int64(estimatedTTL)), nil
}
