package grpcclient

import (
	"context"

	"github.com/agynio/llm-proxy/internal/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	identityIDMetadataKey   = "x-identity-id"
	identityTypeMetadataKey = "x-identity-type"
)

func identityUnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = appendIdentityMetadata(ctx)
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func identityStreamClientInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx = appendIdentityMetadata(ctx)
		return streamer(ctx, desc, cc, method, opts...)
	}
}

func appendIdentityMetadata(ctx context.Context) context.Context {
	resolved, ok := identity.IdentityFromContext(ctx)
	if !ok {
		return ctx
	}

	return metadata.AppendToOutgoingContext(
		ctx,
		identityIDMetadataKey,
		resolved.IdentityID,
		identityTypeMetadataKey,
		string(resolved.IdentityType),
	)
}
