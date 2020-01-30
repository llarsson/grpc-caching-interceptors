package server

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/golang/protobuf/proto"
)

type dynamicTBG1Strategy struct {
	prevMessage verification
	estimate    float64
	alpha       float64
	cnt         int64
	stage       int64
}

// compile-time check that we adhere to interface
var _ estimationStrategy = (*dynamicTBG1Strategy)(nil)

func (strat *dynamicTBG1Strategy) initialize() {
	log.Printf("Using TBG1 strategy")
	strat.alpha = 0.1
	strat.estimate = 0.0
	strat.stage = 0
}

func round(x, unit float64) float64 {
	return math.Round(x/unit) * unit
}

func (strat *dynamicTBG1Strategy) determineInterval(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	// Nyqvist sampling theorem, sample twice as fast as the observed frequency
	if strat.stage >= 1 { // Start verifying once we have an estimate
		return time.Duration(math.Max(0.25, round(strat.estimate/2.0, 0.5)) * 1e9), nil
	}
	return time.Duration(-1), fmt.Errorf("No quite yet")
}

func (strat *dynamicTBG1Strategy) determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	// Rerteive newest message
	newMessage := (*verifications)[len(*verifications)-1]

	// If there is difference between this and the previous sample, save time stamp
	if !proto.Equal(newMessage.reply, strat.prevMessage.reply) {

		if strat.stage == 0 { // Only one sample
			strat.stage++
		} else if strat.stage == 1 { // Two sampled
			strat.estimate = float64(newMessage.timestamp.Sub(strat.prevMessage.timestamp).Seconds())
			strat.stage++
		} else if strat.stage >= 2 { // More than two samples, start gradient descent
			strat.estimate = (1.0-strat.alpha)*strat.estimate + strat.alpha*float64(newMessage.timestamp.Sub(strat.prevMessage.timestamp).Seconds())
		}

		log.Printf(" - Estimate: %v s -> (%v) s", strat.estimate, round(strat.estimate, 1))

		// Save the previous message.
		strat.prevMessage = newMessage
	}

	return time.Duration(round(strat.estimate, 1) * 1e9), nil
}
