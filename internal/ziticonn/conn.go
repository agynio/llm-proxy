package ziticonn

import (
	"context"
	"net"
	"strings"
)

type SourceIdentifiable interface {
	SourceIdentifier() string
}

type contextKey struct{}

func WithSourceIdentity(ctx context.Context, identity string) context.Context {
	trimmed := strings.TrimSpace(identity)
	if trimmed == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, trimmed)
}

func SourceIdentityFromContext(ctx context.Context) (string, bool) {
	identity, ok := ctx.Value(contextKey{}).(string)
	return identity, ok
}

func SourceIdentityFromConn(conn net.Conn) (string, bool) {
	source, ok := conn.(SourceIdentifiable)
	if !ok {
		return "", false
	}
	identity := strings.TrimSpace(source.SourceIdentifier())
	if identity == "" {
		return "", false
	}
	return identity, true
}
