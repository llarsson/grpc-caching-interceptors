package server

import (
	"log"
	"math"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hashicorp/terraform/helper/hashcode"
)

type adaptiveStrategy struct {
	alpha float64

	lastModification time.Time
	responseHash     int

	lastEstimation time.Duration

	mux sync.Mutex
}

// compile-time check that we adhere to interface
var _ estimationStrategy = (*adaptiveStrategy)(nil)

func (strat *adaptiveStrategy) initialize() {
	log.Printf("Using Adaptive TTL strategy with alpha=%f", strat.alpha)

	strat.lastModification = time.Now()
	strat.responseHash = -1

	strat.lastEstimation = 0
}

func (strat *adaptiveStrategy) update(timestamp time.Time, reply proto.Message) {
	incomingHash := hashcode.String(reply.String())
	strat.mux.Lock()
	if incomingHash != strat.responseHash {
		strat.lastModification = timestamp
		strat.responseHash = incomingHash
	}
	strat.mux.Unlock()
}

func (strat *adaptiveStrategy) determineInterval() time.Duration {
	bounded := math.Max(strat.lastEstimation.Seconds()/2.0, defaultInterval.Seconds())
	return time.Duration(bounded) * time.Second
}

func (strat *adaptiveStrategy) determineEstimation() time.Duration {
	estimatedTTL := float64(time.Now().Sub(strat.lastModification).Nanoseconds()) * strat.alpha

	strat.mux.Lock()
	strat.lastEstimation = time.Duration(int64(estimatedTTL))
	strat.mux.Unlock()

	return strat.lastEstimation
}
