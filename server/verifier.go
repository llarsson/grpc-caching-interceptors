package server

import (
	"fmt"
	"log"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/montanaflynn/stats"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type verifier struct {
	target     string
	method     string
	req        proto.Message
	expiration time.Time
	strategy   estimationStrategy

	intervals     []interval
	verifications []verification
	cc            *grpc.ClientConn
	estimations   []estimation
	done          chan string

	responseTimes       []float64
	responseTimeCounter int
	responseTimesFilled bool

	csvLog *log.Logger
}

func (v *verifier) logEstimation(log *log.Logger, source string) error {
	if len(v.estimations) > 0 {
		estimation := v.estimations[len(v.estimations)-1]
		log.Printf("%d,%s,%s,%d\n", time.Now().UnixNano(), v.string(), source, int(estimation.validity.Seconds()))
		return nil
	}

	return fmt.Errorf("no estimations to log yet")
}

// String is a string representation of a given verifier.
func (v *verifier) string() string {
	return fmt.Sprintf("%s(%s)", v.method, v.req)
}

// newVerifier creates a new verifier and starts its goroutine. It attempts
// to establish a grpc.ClientConn to the upstream service. If that fails,
// an error is returned.
func newVerifier(target string, method string, req proto.Message, resp proto.Message, expiration time.Time, strategy estimationStrategy, csvLog *log.Logger, done chan string) (*verifier, error) {
	opts := []grpc.DialOption{grpc.WithDefaultCallOptions(), grpc.WithInsecure()}
	cc, err := grpc.Dial(target, opts...)
	if err != nil {
		log.Printf("Failed to dial %v", err)
		return nil, err
	}

	v := verifier{
		target:     target,
		method:     method,
		req:        req,
		expiration: expiration,
		strategy:   strategy,

		intervals:     make([]interval, 0),
		verifications: make([]verification, 0),
		estimations:   make([]estimation, 0),
		cc:            cc,

		responseTimes:       make([]float64, 256),
		responseTimeCounter: 0,
		responseTimesFilled: false,

		csvLog: csvLog,

		done: done,
	}

	err = v.update(resp, time.Duration(-1))
	if err != nil {
		log.Printf("Unable to create verifier for %s", v.string())
		return nil, err
	}

	go v.run()

	return &v, nil
}

// run the verifier goroutine.
func (v *verifier) run() {
	// good housekeeping to close the grpc.ClientConn when this goroutine
	// finishes.
	defer v.cc.Close()

	for {
		if len(v.intervals) == 0 {
			time.Sleep(time.Duration(500 * time.Millisecond))
			continue
		}

		delay := v.intervals[len(v.intervals)-1].duration
		log.Printf("%s scheduled for verification in %s (expires %s)", v.string(), delay, v.expiration)

		time.Sleep(delay)

		if v.finished() {
			log.Printf("%s needs no further verification", v.string())
			break
		}

		newReply, responseTime, err := v.fetch()
		if err != nil {
			log.Printf("Upstream fetch %s failed: %v", v.string(), err)
			continue
		}

		v.update(newReply, responseTime)
	}

	// signal that we are done and can be deleted.
	v.done <- hash(v.method, v.req)
	return
}

// update internal data structures and estimations based on new data.
func (v *verifier) update(reply proto.Message, responseTime time.Duration) error {
	if v.finished() {
		return status.Errorf(codes.Internal, "Verifier %s finished, cannot be updated anymore", v.string())
	}

	v.recordResponseTime(responseTime)

	now := time.Now()

	// record new data
	v.verifications = append(v.verifications, verification{reply: proto.Clone(reply), timestamp: now})

	// update estimations
	err := v.updateEstimations(reply)
	if err != nil {
		log.Printf("Error updating estimations for %s <- %s", v.string(), reply)
	}

	// update sleep interval
	err = v.updateIntervals(reply)
	if _, static := v.strategy.(*staticStrategy); !static && err != nil {
		log.Printf("Error updating intervals for %s=(%s)", v.string(), reply)
	}

	// FIXME Should we need to interrupt the sleeping goroutine, or do we not care?

	// The only true failure is if we are finished and yet were called.
	// The others do not matter.
	return nil
}

// finished is a predicate that indicates if this verifier has completed its work.
func (v *verifier) finished() bool {
	return time.Now().After(v.expiration)
}

// fetch new reply from upstream service.
func (v *verifier) fetch() (proto.Message, time.Duration, error) {
	reply := proto.Clone(v.verifications[0].reply)
	startTime := time.Now()
	err := v.cc.Invoke(context.Background(), v.method, v.req, reply)
	if err != nil {
		log.Printf("Failed to invoke call over established connection %v", err)
		return nil, time.Duration(-1), err
	}
	endTime := time.Now()

	v.recordResponseTime(endTime.Sub(startTime))

	err = v.logEstimation(v.csvLog, "verifier")
	if err != nil {
		log.Printf("Error printing to CSV log file: %v", err)
	}

	return reply, endTime.Sub(startTime), err
}

func (v *verifier) updateIntervals(reply proto.Message) error {
	if v.strategy != nil {
		duration, err := v.strategy.determineInterval(&v.intervals, &v.verifications, &v.estimations)
		if err != nil {
			return err
		}
		v.intervals = append(v.intervals, interval{duration: duration, timestamp: time.Now()})
	}

	return nil
}

func (v *verifier) updateEstimations(reply proto.Message) error {
	if v.strategy != nil {
		percentile, err := v.percentileResponseTime(0.95)
		if err != nil {
			return err
		}

		validity, err := v.strategy.determineEstimation(&v.intervals, &v.verifications, &v.estimations, percentile)
		if err != nil {
			return err
		}
		v.estimations = append(v.estimations, estimation{validity: validity, timestamp: time.Now()})
	}

	return nil
}

func (v *verifier) estimate() (estimate time.Duration, err error) {
	if len(v.estimations) == 0 {
		return 0, nil
	}
	return v.estimations[len(v.estimations)-1].validity, nil
}

func (v *verifier) recordResponseTime(responseTime time.Duration) {
	v.responseTimeCounter++

	// Avoid zeroed out values to influence our percentile calculation
	// (see also percentileResponseTime)
	if v.responseTimeCounter >= len(v.responseTimes) {
		v.responseTimesFilled = true
		v.responseTimeCounter = 0
	}

	v.responseTimes[v.responseTimeCounter] = float64(responseTime.Nanoseconds())
}

func (v *verifier) percentileResponseTime(percentile float64) (time.Duration, error) {
	// Avoid zeroed out values to influence our percentile calculation
	var recordedResponseTimes []float64
	if v.responseTimesFilled {
		recordedResponseTimes = v.responseTimes
	} else {
		recordedResponseTimes = v.responseTimes[0:v.responseTimeCounter]
	}

	respTime, err := stats.Percentile(recordedResponseTimes, percentile)
	if err != nil {
		return -1, err
	}

	return time.Duration(respTime), nil
}
