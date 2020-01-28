package server

import (
	"log"
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
}

func (strat *dynamicTBG1Strategy) determineInterval(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	// Nyqvist sampling theorem, sample twice as fast as the observed frequency
	if len(*estimations) > 0 {
		log.Printf(" - Interval : %s", (*estimations)[len(*estimations)-1].validity/2)
		return (*estimations)[len(*estimations)-1].validity / 2, nil
	}
	return time.Duration(1 * time.Second), nil
}

func (strat *dynamicTBG1Strategy) determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	validityEstimate := 0.0

	// Rerteive newest message
	newMessage := (*verifications)[len(*verifications)-1]

	// If there is difference between this and the previous sample, save time stamp
	if !proto.Equal(newMessage.reply, strat.prevMessage.reply) {
		strat.deltaTimestamps = append(strat.deltaTimestamps, newMessage.timestamp)
	}

	// Save the previous message.
	strat.prevMessage = newMessage

	// Compute an estimate if there are more than two deltas.
	nbrDelta := len(strat.deltaTimestamps)
	if nbrDelta >= 2 {

		sumDur := float64(0)

		// Run through all timestamps and estimate validity period
		for i := nbrDelta - 1; i > 0; i-- {
			sumDur += float64((strat.deltaTimestamps[i]).Sub((strat.deltaTimestamps)[i-1]).Nanoseconds())
		}

		log.Printf(" ########## Estimation: %v (%v, %v) ", (sumDur/float64(nbrDelta))/1000000000, sumDur/1000000000, nbrDelta)

		validityEstimate = sumDur / float64(nbrDelta)
	}

	return time.Duration(validityEstimate), nil
}
