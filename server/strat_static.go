package server

import (
	"log"
	"time"

	"github.com/golang/protobuf/proto"
)

type staticStrategy struct {
	ttl time.Duration
}

// compile-time check that we adhere to interface
var _ estimationStrategy = (*staticStrategy)(nil)

func (strat *staticStrategy) initialize() {
	log.Printf("Using static TTL=%d for all non-blacklisted responses", int(strat.ttl.Seconds()))
}

func (strat *staticStrategy) update(timestamp time.Time, reply proto.Message) {
	// Static does not concern iteself with updates :)
}

func (strat *staticStrategy) determineInterval() time.Duration {
	// Static also does not concern itself with verification intervals :)
	return time.Duration(-1)
}

func (strat *staticStrategy) determineEstimation() time.Duration {
	return strat.ttl
}
