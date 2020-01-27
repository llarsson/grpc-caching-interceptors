package server

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type verification struct {
	reply     proto.Message
	timestamp time.Time
}

type estimation struct {
	validity  int
	timestamp time.Time
}

type interval struct {
	duration  time.Duration
	timestamp time.Time
}

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

	csvLog *log.Logger
}

type estimationStrategy interface {
	initialize()
	determineInterval(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error)
	determineEstimation(intervals *[]interval, verifications *[]verification, estimations *[]estimation) (time.Duration, error)
}

var _ Verifier = verifier{}

func (v verifier) logEstimation(log *log.Logger, source string) error {
	estimation, err := v.estimateMaxAge()
	if err != nil {
		return err
	}
	log.Printf("%d,%s,%s,%d\n", time.Now().UnixNano(), v.String(), source, estimation)
	return nil
}

// String is a string representation of a given verifier.
func (v verifier) String() string {
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

		csvLog: csvLog,

		done: done,
	}

	err = v.update(resp)
	if err != nil {
		log.Printf("Unable to create verifier for %s", v.String())
		return nil, err
	}

	go v.run()

	return &v, nil
}

// run the verifier goroutine.
func (v verifier) run() {
	// good housekeeping to close the grpc.ClientConn when this goroutine
	// finishes.
	defer v.cc.Close()

	for {
		if len(v.intervals) == 0 {
			time.Sleep(time.Duration(500 * time.Millisecond))
			continue
		}

		delay := v.intervals[len(v.intervals)-1].duration
		log.Printf("%s scheduled for verification in %s (expires %s)", v.String(), delay, v.expiration)

		time.Sleep(delay)

		if v.finished() {
			log.Printf("%s needs no further verification", v.String())
			break
		}

		newReply, err := v.fetch()
		if err != nil {
			log.Printf("Upstream fetch %s failed: %v", v.String(), err)
			continue
		}

		v.update(newReply)
	}

	// signal that we are done and can be deleted.
	v.done <- hash(v.method, v.req)
	return
}

// update internal data structures and estimations based on new data.
func (v verifier) update(reply proto.Message) error {
	if v.finished() {
		return status.Errorf(codes.Internal, "Verifier %s finished, cannot be updated anymore", v.String())
	}

	now := time.Now()

	// record new data
	v.verifications = append(v.verifications, verification{reply: proto.Clone(reply), timestamp: now})

	// update estimations
	estimate, verificationInterval, err := v.estimate()
	if err != nil {
		log.Printf("Error estimating for %s <- %s", v.String(), reply)
		return err
	}
	v.estimations = append(v.estimations, estimation{validity: estimate, timestamp: now})

	// update sleep interval
	v.intervals = append(v.intervals, interval{duration: verificationInterval, timestamp: now})

	// FIXME Should we need to interrupt the sleeping goroutine, or do we not care?

	return nil
}

// finished is a predicate that indicates if this verifier has completed its work.
func (v verifier) finished() bool {
	return time.Now().After(v.expiration)
}

// fetch new reply from upstream service.
func (v verifier) fetch() (proto.Message, error) {
	reply := proto.Clone(v.verifications[0].reply)
	err := v.cc.Invoke(context.Background(), v.method, v.req, reply)
	if err != nil {
		log.Printf("Failed to invoke call over established connection %v", err)
		return nil, err
	}

	v.logEstimation(v.csvLog, "verifier")

	return reply, nil
}

// estimateMaxAge returns the number of seconds that the current verifier
// estimates is reasonable to cache response objects. If no estimation
// has been made yet, an error is returned (and the reasonable thing to
// do would likely be to set the max-age cache header to 0 or not include
// it at all).
func (v verifier) estimateMaxAge() (int, error) {
	if len(v.estimations) == 0 {
		return -1, status.Errorf(codes.Internal, "No estimation found yet.")
	}

	return v.estimations[len(v.estimations)-1].validity, nil
}

// verify all replies against each other and return the duration until
// we should verify again.
func (v verifier) estimate() (estimatedMaxAge int, verificationInterval time.Duration, err error) {
	estimatedMaxAge, err = v.produceEstimation()
	if err != nil {
		log.Printf("Failed to determine max-age for %s", v.String())
		return -1, time.Duration(0), err
	}

	verificationInterval, err = v.strategy.determineInterval(&v.intervals, &v.verifications, &v.estimations)
	if err != nil {
		log.Printf("Failed to determine verification interval for %s", v.String())
		return -1, time.Duration(0), err
	}

	return estimatedMaxAge, verificationInterval, err
}

func (v verifier) produceEstimation() (estimate int, err error) {
	value, present := os.LookupEnv("PROXY_MAX_AGE")

	if !present {
		// It is not an error to not have the proxy max age key present in environment. We just act as if we were in passthrough mode.
		return -1, nil
	}

	switch value {
	case "dynamic":
		{
			dur, err := v.strategy.determineEstimation(&v.intervals, &v.verifications, &v.estimations)
			if err != nil {
				log.Printf("Failed to estimate max-age for %s due to %v", v.String(), err)
				return -1, err
			}
			return int(dur.Seconds()), nil
		}
	case "passthrough":
		{
			return -1, nil
		}
	default:
		maxAge, err := strconv.Atoi(value)
		if err != nil {
			log.Printf("Failed to parse PROXY_MAX_AGE (%s) into integer", value)
			return -1, err
		}
		return maxAge, nil
	}
}
