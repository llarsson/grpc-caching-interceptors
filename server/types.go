package server

import (
	"log"

	"github.com/patrickmn/go-cache"
)

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
