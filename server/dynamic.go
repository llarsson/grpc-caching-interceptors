package server

import (
	"time"

	"github.com/golang/protobuf/proto"
)

type dynamicStrategy struct {
	prevMessage proto.Message
	deltaTimestamps []time.Time
}

func (strat *dynamicStrategy) initialize() {
}

func (strat *dynamicStrategy) determineInterval(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	// Nyqvist sampling theorem, sample twice as fast as the observed frequency
	return time.Duration((*estimations)[len(*estimations)]/2 * time.Second), nil
}

func (strat *dynamicStrategy) determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	// Rerteive newest message
	newMessage := (*verifications)[len(*verifications)]

	// If there is difference between this and the previous sample, save time stamp
	if proto.Equal(newMessage.reply, strat.prevMessage.reply) {
		append(stats.deltaTimestamps, newMessage.timestamp)
	}

	// Run through all timestamps and estimate validity period
	avgDur := 0
	nbrDelta := len(*strat.deltaTimestamps)
	for i := nbrDelta; i > 0; i-- {
		avgDur += ((*strat.deltaTimestamps)[i].Sub((*strat.deltaTimestamps)[i-1])/nbrDelta).Seconds()
	}

	// Save the previous message.
	strat.prevMessage = newMessage

	// TTL is estimate, otherwise 0
	return time.Duration(avgDur), nil
}
