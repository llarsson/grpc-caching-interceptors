package server

import "time"

const (
	defaultInterval     = time.Duration(5 * time.Second)
	maxVerifierLifetime = time.Duration(1800 * time.Second)
)
