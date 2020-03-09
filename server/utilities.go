package server

import (
	"fmt"
	"time"
)

// backwardsUpdateDistance computes backwards K-update distance as in Lee et al.
// "An Update-Risk Based Approach to TTL Estimation in Web Caching"
func backwardsUpdateDistance(verifications *[]verification, K int) ([]time.Time, int) {
	timestamps := make([]time.Time, K)

	var v verification
	var updates int

	// assume, as we must, that current value is the most recent and "true"
	v = (*verifications)[len(*verifications)-1]

	for i := len(*verifications) - 1; i >= 0 && updates < K; i-- {
		var current = (*verifications)[i]
		if !equalVerifications(v, current) {
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

// lastEstimation returns the last estimation in the list of estimations
// or raises an error if no estimations are found.
func lastEstimation(estimates *[]estimation) (estimation, error) {
	length := len(*estimates)
	if length > 0 {
		return (*estimates)[length-1], nil
	}

	return estimation{}, fmt.Errorf("List of estimations is empty")
}
