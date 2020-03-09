package server

import (
	"log"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/patrickmn/go-cache"
)

type verification struct {
	reply     proto.Message
	timestamp time.Time
}

type estimation struct {
	validity  time.Duration
	timestamp time.Time
}

type interval struct {
	duration  time.Duration
	timestamp time.Time
}

// equalVerifications determines equality between verifications
func equalVerifications(a verification, b verification) bool {
	return proto.Equal(a.reply, b.reply)
}

// ConfigurableValidityEstimator is a configurable ValidityEstimator.
type ConfigurableValidityEstimator struct {
	// We abuse the cache data structure here, s.t. it is used as a handy
	// place to store items that expire and are then garbage collected.
	verifiers *cache.Cache
	// A channel where verifiers can specify their ID as being done.
	done chan string
	// Where to log CSV records
	csvLog *log.Logger
}
