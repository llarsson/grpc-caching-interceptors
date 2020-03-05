package server

import (
	"time"

	"github.com/golang/protobuf/proto"
)

// EqualVerifications conveniently checks if two verification structs are equal.
func EqualVerifications(a verification, b verification) bool {
	return proto.Equal(a.reply, b.reply)
}

// BackwardsUpdateDistance computes backwards K-update distance as in Lee et al.
// "An Update-Risk Based Approach to TTL Estimation in Web Caching"
func BackwardsUpdateDistance(verifications *[]verification, K int) ([]time.Time, int) {
	timestamps := make([]time.Time, K)

	var v verification
	var updates int

	// assume, as we must, that current value is the most recent and "true"
	v = (*verifications)[len(*verifications)-1]

	for i := len(*verifications) - 1; i >= 0 && updates < K; i-- {
		var current = (*verifications)[i]
		if !EqualVerifications(v, current) {
			timestamps[updates] = current.timestamp
			updates++
			v = current
		}
	}

	if updates == 0 {
		return timestamps, updates
	}

	return timestamps, updates
}
