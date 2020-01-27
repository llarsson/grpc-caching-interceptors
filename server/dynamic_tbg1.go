package server

import (
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
	return (*estimations)[len(*estimations)].validity / 2, nil
}

func (strat *dynamicTBG1Strategy) determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	// Rerteive newest message
	newMessage := (*verifications)[len(*verifications)]

	// If there is difference between this and the previous sample, save time stamp
	if proto.Equal(newMessage.reply, strat.prevMessage.reply) {
		strat.deltaTimestamps = append(strat.deltaTimestamps, newMessage.timestamp)
	}

	// Run through all timestamps and estimate validity period
	avgDur := 0.0
	nbrDelta := len(strat.deltaTimestamps)
	for i := nbrDelta; i > 0; i-- {
		avgDur += (strat.deltaTimestamps[i]).Sub((strat.deltaTimestamps)[i-1]).Seconds() / float64(nbrDelta)
	}

	// Save the previous message.
	strat.prevMessage = newMessage

	// TTL is estimate, otherwise 0
	return time.Duration(int(avgDur)), nil
}
