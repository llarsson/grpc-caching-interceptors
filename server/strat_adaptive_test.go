package server

import (
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
)

type mockMessage interface {
	String() string
}

type sample struct {
	value string
}

func (s sample) String() string {
	return s.value
}

func (s sample) ProtoMessage() {
}

func (s sample) Reset() {
}

func TestAdaptiveWithoutChange(test *testing.T) {
	var s mockMessage

	s = sample{value: "0"}
	strat := &adaptiveStrategy{alpha: 0.5}
	strat.initialize()

	var t time.Time
	t = time.Now().Add(-10 * time.Second)

	for i := 0; i < 10; i++ {
		strat.update(t, s.(proto.Message))
		t = t.Add(1 * time.Second)
	}

	got := strat.determineEstimation()
	if int(got.Seconds()) != 5 {
		test.Errorf("Wanted 5 second TTL, got %v", got)
	}
}

func TestAdaptiveWithoutChangeConservative(test *testing.T) {
	var s mockMessage

	s = sample{value: "0"}
	strat := &adaptiveStrategy{alpha: 0.1}
	strat.initialize()

	var t time.Time
	t = time.Now().Add(-10 * time.Second)

	for i := 0; i < 10; i++ {
		strat.update(t, s.(proto.Message))
		t = t.Add(1 * time.Second)
	}

	got := strat.determineEstimation()
	if int(got.Seconds()) != 1 {
		test.Errorf("Wanted 1 second TTL, got %v", got)
	}
}

func TestAdaptiveWithChange(test *testing.T) {
	var s mockMessage

	s = sample{value: "0"}
	strat := &adaptiveStrategy{alpha: 0.5}
	strat.initialize()

	var t time.Time
	t = time.Now().Add(-20 * time.Second)

	for i := 0; i < 10; i++ {
		strat.update(t, s.(proto.Message))
		t = t.Add(1 * time.Second)
	}
	s = sample{value: "1"}
	for i := 0; i < 10; i++ {
		strat.update(t, s.(proto.Message))
		t = t.Add(1 * time.Second)
	}

	got := strat.determineEstimation()
	if int(got.Seconds()) != 5 {
		test.Errorf("Wanted 5 second TTL, got %v", got)
	}
}
