// Package client contains the client-side gRPC Interceptor for Unary RPC
// calls, intended for use in a caching reverse proxy implementation.
package client

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hashicorp/terraform/helper/hashcode"
	"github.com/patrickmn/go-cache"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// A CachingInterceptor intercepts incoming calls to a reverse proxy's server
// part, and outgoing calls from the reverse proxy's client part. It should,
// by contract, cache the responses.
type CachingInterceptor interface {
	// UnaryServerInterceptor creates the server interceptor part of the
	// reverse proxy.
	UnaryServerInterceptor() grpc.UnaryServerInterceptor
	// UnaryClientInterceptor creates the client interceptor part of the
	// reverse proxy.
	UnaryClientInterceptor() grpc.UnaryClientInterceptor
}

// InmemoryCachingInterceptor is an implementation of CachingInterceptor, which
// uses an in-memory cache to store objects.
type InmemoryCachingInterceptor struct {
	Cache cache.Cache
}

// UnaryServerInterceptor catches all incoming calls, verifies if a suitable
// response is already in cache, and if so, it just responds with it. If
// no such response is found, the call is allowed to continue as usual,
// via a client call (which should be intercepted also).
func (interceptor *InmemoryCachingInterceptor) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		reqMessage := req.(proto.Message)
		hash := hashcode.Strings([]string{info.FullMethod, reqMessage.String()})

		if value, found := interceptor.Cache.Get(hash); found {
			grpc.SendHeader(ctx, metadata.Pairs("x-cache", "hit"))
			log.Printf("Using cached response for call to %s(%s)", info.FullMethod, req)
			return value, nil
		}

		resp, err := handler(ctx, req)
		if err != nil {
			log.Printf("Failed to call upstream %s(%s): %v", info.FullMethod, req, err)
			return nil, err
		}

		return resp, nil
	}
}

// UnaryClientInterceptor catches outgoing calls, and inspects the response
// headers on the incoming response. If cache headers are set, the response
// is cached in the in-memory cache for as long as the header specifies.
// Subsequent matching operation invocations via the reverse proxy that uses
// these Interceptors will therefore be served from cache.
func (interceptor *InmemoryCachingInterceptor) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		reqMessage := req.(proto.Message)
		hash := hashcode.Strings([]string{method, reqMessage.String()})

		var header metadata.MD
		opts = append(opts, grpc.Header(&header))
		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			log.Printf("Error calling upstream: %v", err)
			return err
		}

		cacheStatus := "response not stored"

		expiration, _ := cacheExpiration(header.Get("cache-control"))
		if expiration > 0 {
			interceptor.Cache.Set(hash, reply, time.Duration(expiration)*time.Second)
			cacheStatus = fmt.Sprintf("response stored %d seconds", expiration)
		}

		grpc.SendHeader(ctx, metadata.Pairs("x-cache", "miss"))
		log.Printf("Fetched upstream response for call to %s(%s) (%s)", method, req, cacheStatus)
		return nil
	}
}

func cacheExpiration(cacheHeaders []string) (int, error) {
	for _, header := range cacheHeaders {
		for _, value := range strings.Split(header, ",") {
			value = strings.Trim(value, " ")
			if strings.HasPrefix(value, "max-age") {
				duration := strings.Split(value, "max-age=")[1]
				return strconv.Atoi(duration)
			}
		}
	}
	return -1, status.Errorf(codes.Internal, "No cache expiration set for the given object")
}
