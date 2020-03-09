package server

import "time"

const (
	defaultInterval      = time.Duration(5 * time.Second)
	maximumCacheValidity = 1000
)