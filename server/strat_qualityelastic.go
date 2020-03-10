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

	inner updateRiskBasedStrategy
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
	strat.inner = updateRiskBasedStrategy{rho: 0.0, K: 2}
	strat.inner.initialize()
}

func (strat *qualityElasticStrategy) determineInterval(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	return strat.inner.determineInterval(intervals, verifications, estimations)
}

func (strat *qualityElasticStrategy) determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation, ninetyFithPercentileResponseTime time.Duration) (time.Duration, error) {
	strat.inner.rho = strat.calculateUpdateRisk(ninetyFithPercentileResponseTime)
	return strat.inner.determineEstimation(intervals, verifications, estimations, ninetyFithPercentileResponseTime)
}

func (strat *qualityElasticStrategy) calculateUpdateRisk(ninetyFithPercentileResponseTime time.Duration) float64 {
	fraction := float64(ninetyFithPercentileResponseTime.Nanoseconds() / strat.SLO.Nanoseconds())
	return math.Max(fraction*strat.dampening, 0.9)
}
