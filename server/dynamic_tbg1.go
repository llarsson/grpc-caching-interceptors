package server

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/golang/protobuf/proto"
)

type dynamicTBG1Strategy struct {
	prevMessage     verification
	deltaTimestamps []time.Time
}

// compile-time check that we adhere to interface
var _ estimationStrategy = (*dynamicTBG1Strategy)(nil)

func (strat *dynamicTBG1Strategy) initialize() {
	log.Printf("Using tbg1 strategy")
}

func (strat *dynamicTBG1Strategy) determineInterval(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	// Nyqvist sampling theorem, sample twice as fast as the observed frequency
	if len(*estimations) > 0 {
		lastEstimate := (*estimations)[len(*estimations)-1].validity
		if lastEstimate > 0 {
			return time.Duration(math.Max(500*float64(time.Millisecond), float64((*estimations)[len(*estimations)-1].validity.Nanoseconds())/2.0)), nil
		}
	}
	return time.Duration(-1), fmt.Errorf("No quite yet")
}

func (strat *dynamicTBG1Strategy) determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	validityEstimate := int64(0)

	// Rerteive newest message
	newMessage := (*verifications)[len(*verifications)-1]

	// If there is difference between this and the previous sample, save time stamp
	if !proto.Equal(newMessage.reply, strat.prevMessage.reply) {
		strat.deltaTimestamps = append(strat.deltaTimestamps, newMessage.timestamp)
	}

	// Save the previous message.
	strat.prevMessage = newMessage

	// Compute an estimate if there are more than two deltas.
	nbrDelta := int64(len(strat.deltaTimestamps))
	if nbrDelta >= 2 {

		sumDur := int64(0)

		// Run through all timestamps and estimate validity period
		for i := nbrDelta - 1; i > 0; i-- {
			sumDur += (strat.deltaTimestamps[i]).Sub((strat.deltaTimestamps)[i-1]).Nanoseconds()
		}

		validityEstimate = sumDur / nbrDelta
	}

	return time.Duration(validityEstimate), nil
}
