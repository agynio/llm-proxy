package identity

import (
	"context"
	"fmt"
	"strings"
)

type IdentityType string

const (
	IdentityTypeUser    IdentityType = "user"
	IdentityTypeAgent   IdentityType = "agent"
	IdentityTypeChannel IdentityType = "channel"
	IdentityTypeRunner  IdentityType = "runner"
)

type ResolvedIdentity struct {
	IdentityID   string
	IdentityType IdentityType
}

func ParseIdentityType(value string) (IdentityType, error) {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case string(IdentityTypeUser):
		return IdentityTypeUser, nil
	case string(IdentityTypeAgent):
		return IdentityTypeAgent, nil
	case string(IdentityTypeChannel):
		return IdentityTypeChannel, nil
	case string(IdentityTypeRunner):
		return IdentityTypeRunner, nil
	default:
		return "", fmt.Errorf("unsupported identity type: %q", value)
	}
}

type contextKey struct{}

func WithIdentity(ctx context.Context, identity ResolvedIdentity) context.Context {
	return context.WithValue(ctx, contextKey{}, identity)
}

func IdentityFromContext(ctx context.Context) (ResolvedIdentity, bool) {
	identity, ok := ctx.Value(contextKey{}).(ResolvedIdentity)
	return identity, ok
}
