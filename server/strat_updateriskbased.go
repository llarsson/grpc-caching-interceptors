package server

import (
	"log"
	"math"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hashicorp/terraform/helper/hashcode"
)

// This implementation embodies (our understanding of) Lee et al.
// "An Update-Risk Based Approach to TTL Estimation in Web Caching", 2002.
// https://doi.org/10.1109/WISE.2002.1181640
//
// We use K = 2, because the paper found it to be optimal. That means that we
// save the two "last modification" times, and base our calculations on that.
type updateRiskBasedStrategy struct {
	rho float64

	olderModification time.Time
	newerModification time.Time

	responseHash int

	lastEstimation time.Duration

	observedUpdates int
}

// compile-time check that we adhere to interface
var _ estimationStrategy = (*updateRiskBasedStrategy)(nil)

func (strat *updateRiskBasedStrategy) initialize() {
	log.Printf("Using Update-Risk Based strategy (rho = %v)", strat.rho)

	strat.responseHash = -1

	now := time.Now()
	strat.olderModification = now
	strat.newerModification = now

	strat.lastEstimation = 0

	strat.observedUpdates = 0
}

func (strat *updateRiskBasedStrategy) update(timestamp time.Time, reply proto.Message) {
	incomingHash := hashcode.String(reply.String())

	if incomingHash != strat.responseHash {
		strat.olderModification = strat.newerModification
		strat.newerModification = timestamp

		strat.responseHash = incomingHash

		if strat.observedUpdates < 2 {
			strat.observedUpdates++
		}
	}
}

// This comes in no way from the original paper, but our interface demands it,
// so this should be a reasonable implementation of interval determination.
func (strat *updateRiskBasedStrategy) determineInterval() time.Duration {
	bounded := math.Max(strat.lastEstimation.Seconds()/2.0, defaultInterval.Seconds())
	return time.Duration(bounded) * time.Second
}

func (strat *updateRiskBasedStrategy) determineEstimation() time.Duration {
	mu := strat.averageUpdateFrequency()
	t := -1.0 / mu * math.Log(1.0-strat.rho)
	return time.Duration(t) * time.Second
}

func (strat *updateRiskBasedStrategy) averageUpdateFrequency() float64 {
	if strat.observedUpdates == 0 {
		log.Printf("No observed value updates yet, using 1.0 as update frequency")
		return 1.0
	}

	var lastModified time.Time

	if strat.observedUpdates == 1 {
		lastModified = strat.newerModification
	} else {
		lastModified = strat.olderModification
	}

	// We requested K updates back, but perhaps got less. So we must rely
	// on what we actually got back from the data.
	timespan := time.Now().Sub(lastModified)

	return float64(strat.observedUpdates) / timespan.Seconds()
}
