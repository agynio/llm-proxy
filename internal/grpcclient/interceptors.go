package grpcclient

import (
	"context"

	"github.com/agynio/llm-proxy/internal/identity"
	"google.golang.org/grpc"
)

func identityUnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(identity.AppendToOutgoingContext(ctx), method, req, reply, cc, opts...)
	}
}

func identityStreamClientInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return streamer(identity.AppendToOutgoingContext(ctx), desc, cc, method, opts...)
	}
}
