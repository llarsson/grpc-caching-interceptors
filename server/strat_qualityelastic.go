package server

import (
	"log"
	"math"
	"time"
)

// This strategy leverages our understanding of the Update-Risk Based algorithm
// (see strat_updateriskbased.go) and, in a quality-elastic manner, modifies
// the update-risk parameter based on current response time metrics.
type qualityElasticStrategy struct {
	SLO       time.Duration
	dampening float64
}

// compile-time check that we adhere to interface
var _ estimationStrategy = (*qualityElasticStrategy)(nil)

func (strat *qualityElasticStrategy) initialize() {
	// Hopefully reasonable default values(?)
	if strat.dampening <= 0.0001 {
		strat.dampening = 0.1
	}
	if strat.SLO.Nanoseconds() <= 0 {
		strat.SLO = time.Duration(100 * time.Millisecond)
	}

	log.Printf("Using Quality-Elastic strategy (95th percentile response time SLO=%v, dampening=%v)", strat.SLO, strat.dampening)
}

func (strat *qualityElasticStrategy) determineInterval(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	estimate, err := lastEstimation(estimations)
	if err != nil {
		log.Printf("No previous estimations, relying on default interval")
		return defaultInterval, nil
	}

	bounded := math.Max(estimate.validity.Seconds()/2.0, defaultInterval.Seconds())

	return time.Duration(bounded) * time.Second, nil
}

func (strat *qualityElasticStrategy) determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation, ninetyFithPercentileResponseTime time.Duration) (time.Duration, error) {
	rho := strat.calculateUpdateRisk(ninetyFithPercentileResponseTime)
	mu := strat.averageUpdateFrequency(verifications)
	t := -1.0 / mu * math.Log(1.0-rho)
	return time.Duration(t) * time.Second, nil
}

func (strat *qualityElasticStrategy) averageUpdateFrequency(verifications *[]verification) float64 {
	timestamps, updates := backwardsUpdateDistance(verifications, 2)
	if updates == 0 {
		log.Printf("No observed value updates yet, using 1.0 as update frequency")
		return 1.0
	}

	timespan := time.Now().Sub(timestamps[updates-1])

	return float64(updates) / timespan.Seconds()
}

func (strat *qualityElasticStrategy) calculateUpdateRisk(ninetyFithPercentileResponseTime time.Duration) float64 {
	fraction := float64(ninetyFithPercentileResponseTime.Nanoseconds() / strat.SLO.Nanoseconds())
	return math.Max(fraction*strat.dampening, 1.0)
}
