package ziticonn

import (
	"context"
	"log"
	"net"
	"strings"
)

type DialerIdentifiable interface {
	GetDialerIdentityId() string
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
	log.Printf("[DIAG] SourceIdentityFromConn called, conn type: %T", conn)
	if dialer, ok := conn.(DialerIdentifiable); ok {
		id := dialer.GetDialerIdentityId()
		log.Printf("[DIAG] GetDialerIdentityId() returned: %q", id)
		identity := strings.TrimSpace(id)
		if identity != "" {
			return identity, true
		}
	} else {
		log.Printf("[DIAG] conn does NOT implement DialerIdentifiable")
	}
	log.Printf("[DIAG] no identity found, returning false")
	return "", false
}
