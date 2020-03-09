package server

import (
	"log"
	"time"

	"github.com/golang/protobuf/proto"
)

type nyqvistishStrategy struct {
}

// compile-time check that we adhere to interface
var _ estimationStrategy = (*nyqvistishStrategy)(nil)

func (strat *nyqvistishStrategy) initialize() {
	log.Printf("Using Nyqvist-ish strategy")
}

func (strat *nyqvistishStrategy) determineInterval(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	if len(*estimations) > 0 {
		lastEstimate := (*estimations)[len(*estimations)-1]
		return lastEstimate.validity, nil
	}
	return time.Duration(1 * time.Second), nil
}

func (strat *nyqvistishStrategy) determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation, _ time.Duration) (time.Duration, error) {
	lastVerification := (*verifications)[len(*verifications)-1]

	var oldestVerification verification
	for i := len(*verifications) - 1; i >= 0; i-- {
		if proto.Equal((*verifications)[i].reply, lastVerification.reply) {
			oldestVerification = (*verifications)[i]
		} else {
			break // we no longer match, might as well quit early...
		}
	}
	unchanged := lastVerification.timestamp.Sub(oldestVerification.timestamp)

	// claim that the TTL is half of the observed "unchanged" interval
	return time.Duration(unchanged / 2), nil
}
