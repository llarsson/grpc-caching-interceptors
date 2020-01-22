package server

import (
	"time"

	"github.com/golang/protobuf/proto"
)

type simplisticStrategy struct {
}

func (strat *simplisticStrategy) initialize() {

}

func (strat *simplisticStrategy) determineInterval(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	return time.Duration(5 * time.Second), nil
}

func (strat *simplisticStrategy) determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error) {
	lastVerification := (*verifications)[len(*verifications)-1]

	var oldestVerification verification
	for i := len(*verifications) - 1; i >= 0; i-- {
		if proto.Equal((*verifications)[i].reply, lastVerification.reply) {
			oldestVerification = (*verifications)[i]
		} else {
			break // we no longer match, might as well quit early...
		}
	}
	unchanged := int(lastVerification.timestamp.Sub(oldestVerification.timestamp).Seconds())

	// claim that the TTL is half of the observed "unchanged" interval
	return time.Duration(unchanged/2) * time.Second, nil
}
