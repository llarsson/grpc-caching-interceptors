package server

import (
	"fmt"
	"log"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hashicorp/terraform/helper/hashcode"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	verifierSource = "verifier"
	clientSource   = "client"
)

type verifier struct {
	target     string
	method     string
	req        proto.Message
	expiration time.Time
	strategy   estimationStrategy

	cc   *grpc.ClientConn
	done chan string

	responseArchetype proto.Message

	estimatedTTL time.Duration

	stringRepresentation string
	csvLog               *log.Logger
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
		target:               target,
		method:               method,
		req:                  req,
		expiration:           expiration,
		strategy:             strategy,
		cc:                   cc,
		responseArchetype:    proto.Clone(resp),
		estimatedTTL:         0,
		csvLog:               csvLog,
		done:                 done,
		stringRepresentation: fmt.Sprintf("%s(%d)", method, hashcode.String(req.String())),
	}

	err = v.update(resp, clientSource)
	if err != nil {
		log.Printf("Unable to create verifier for %s", v.method)
		return nil, err
	}

	go v.run()

	return &v, nil
}

func (v *verifier) string() string {
	return v.stringRepresentation
}

// run the verifier goroutine.
func (v *verifier) run() {
	// good housekeeping to close the grpc.ClientConn when this goroutine
	// finishes.
	defer v.cc.Close()

	for {
		delay := v.strategy.determineInterval()
		if delay <= 0 {
			time.Sleep(time.Duration(500 * time.Millisecond))
			continue
		}

		log.Printf("%s scheduled for verification in %s (expires %s)", v.string(), delay, v.expiration)

		time.Sleep(delay)

		if v.finished() {
			log.Printf("%s needs no further verification", v.string())
			break
		}

		// Research idea:
		//
		// Add a verification step here, where data is fetched from the
		// upstream service. Periodically polling the upstream data
		// source in a proactive manner should make it possible to
		// reduce data staleness.
		//
		// The code below shows how this could be added.
		//
		//		newReply, err := v.fetch()
		//		if err != nil {
		//			log.Printf("Upstream fetch %s failed: %v", v.string(), err)
		//			continue
		//		}

		// v.update(newReply, verifierSource)
	}

	// signal that we are done and can be deleted.
	v.done <- hash(v.method, v.req)
	return
}

// update internal data structures and estimations based on new data.
func (v *verifier) update(reply proto.Message, source string) error {
	if v.finished() {
		return status.Errorf(codes.Internal, "Verifier %s finished, cannot be updated anymore", v.string())
	}

	now := time.Now()
	v.strategy.update(now, reply)
	v.estimatedTTL = v.strategy.determineEstimation()

	v.csvLog.Printf("%d,%s,%s,%d\n", time.Now().UnixNano(), source, v.string(), int(v.estimatedTTL.Seconds()))

	return nil
}

// finished is a predicate that indicates if this verifier has completed its work.
func (v *verifier) finished() bool {
	return time.Now().After(v.expiration)
}

// This code is for illustration purposes only. Initial testing shows that it
// contains bugs, and cannot be used in its current state.
//
// fetch a new response from the upstream service (proactive operation).
// func (v *verifier) fetch() (proto.Message, error) {
// 	reply := proto.Clone(v.responseArchetype)
// 	reply.Reset()
//
// 	err := v.cc.Invoke(context.Background(), v.method, v.req, reply)
// 	if err != nil {
// 		log.Printf("Failed to invoke call over established connection %v", err)
// 		return nil, err
// 	}
//
// 	return reply, err
// }

func (v *verifier) estimate() (time.Duration, error) {
	return v.estimatedTTL, nil
}
