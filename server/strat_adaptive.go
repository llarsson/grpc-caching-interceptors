package server

import (
	"log"
	"math"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"

	"hash/fnv"
)

type adaptiveStrategy struct {
	alpha float64

	lastModification time.Time
	responseHash     uint32

	lastEstimation time.Duration

	mux sync.Mutex
}

// compile-time check that we adhere to interface
var _ estimationStrategy = (*adaptiveStrategy)(nil)

func (strat *adaptiveStrategy) initialize() {
	log.Printf("Using Adaptive TTL strategy with alpha=%f", strat.alpha)

	strat.lastModification = time.Now()
	strat.responseHash = 11

	strat.lastEstimation = 0
}

func hasher(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

func (strat *adaptiveStrategy) update(timestamp time.Time, reply proto.Message) {
	//incomingHash := hashcode.String(reply.String())
	incomingHash := hasher(reply.String())
	strat.mux.Lock()
	if incomingHash != strat.responseHash {
		log.Printf("Hash different (%v)!", incomingHash)

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
