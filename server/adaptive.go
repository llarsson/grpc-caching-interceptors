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
	modifications, err := BackwardsUpdateDistance(verifications, 1)
	if err != nil {
		if len((*verifications)) > 0 {
			lastModification = (*verifications)[0].timestamp
		} else {
			return 0, nil
		}
	} else {
		lastModification = modifications[0]
	}

	estimatedTTL := float64(time.Now().Sub(lastModification).Nanoseconds()) * strat.alpha

	return time.Duration(int64(estimatedTTL)), nil
}
