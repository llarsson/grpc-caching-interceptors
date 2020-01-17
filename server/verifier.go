package server

import (
	"fmt"
	"log"
	"time"

	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

type verifierMetadata struct {
	method        string
	req           interface{}
	target        string
	previousReply interface{}
	interval      time.Duration
	timestamp     time.Time
	expiration    time.Time
}

type verifier struct {
	metadata *verifierMetadata
	cc       *grpc.ClientConn
	done     chan string
}

// newVerifier creates a new verifier and starts its goroutine. It attempts
// to establish a grpc.ClientConn to the upstream service. If that fails,
// an error is returned.
func newVerifier(md verifierMetadata, done chan string) (*verifier, error) {
	opts := []grpc.DialOption{grpc.WithDefaultCallOptions(), grpc.WithInsecure()}
	cc, err := grpc.Dial(md.target, opts...)
	if err != nil {
		log.Printf("Failed to dial %v", err)
		return nil, err
	}

	v := verifier{metadata: &md, cc: cc, done: done}

	go v.run()

	return &v, nil
}

// run the verifier goroutine.
func (v *verifier) run() {
	// good housekeeping to close the grpc.ClientConn when this goroutine
	// finishes.
	defer v.cc.Close()

	for {
		log.Printf("Object %s scheduled for verification in %s (expires %s)", v.String(), v.metadata.interval, v.metadata.expiration)

		time.Sleep(v.metadata.interval)

		if v.finished() {
			log.Printf("Object %s needs no further verification", v.String())
			break
		}

		newReply, err := v.fetch()
		if err != nil {
			log.Printf("Upstream fetch failed: %v", err)
			continue
		}

		newInterval := v.verify(newReply)

		// Prepare for next iteration.
		v.metadata.interval = newInterval
		v.metadata.previousReply = newReply
	}

	// signal that we are done and can be deleted.
	v.done <- hash(v.metadata.method, v.metadata.req)
	return
}

// finished is a predicate that indicates if this verifier has completed its work.
func (v *verifier) finished() bool {
	return time.Now().After(v.metadata.expiration)
}

// fetch new reply from upstream service.
func (v *verifier) fetch() (interface{}, error) {
	previousReplyMessage := v.metadata.previousReply.(proto.Message)

	reply := proto.Clone(previousReplyMessage)
	err := v.cc.Invoke(context.Background(), v.metadata.method, v.metadata.req, reply)
	if err != nil {
		log.Printf("Failed to invoke call over established connection %v", err)
		return nil, err
	}

	return reply, nil
}

// verify the new reply against the old one and return the duration until
// we should verify again.
func (v *verifier) verify(newReply interface{}) time.Duration {
	// TODO Actual smartness goes here!
	return time.Duration(5 * time.Second)
}

// String is a string representation of this verifier.
func (v *verifier) String() string {
	return fmt.Sprintf("%s(%s)", v.metadata.method, v.metadata.req.(proto.Message))
}

// FIXME The "estimateMaxAge" functionality belongs here!
