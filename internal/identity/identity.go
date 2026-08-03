package identity

import (
	"context"
	"fmt"
	"strings"
)

type IdentityType string

const (
	IdentityTypeUser IdentityType = "user"
	// IdentityTypeAgent names an agent class. It predates instances and is kept
	// because identities minted before the migration still present it.
	IdentityTypeAgent IdentityType = "agent"
	// IdentityTypeAgentInstance names one running instance of an agent class.
	// Agent workloads authenticate as this, so refusing it here refused every
	// turn they tried to take.
	IdentityTypeAgentInstance IdentityType = "agent_instance"
	IdentityTypeApp           IdentityType = "app"
	IdentityTypeRunner        IdentityType = "runner"
	IdentityTypeSandbox       IdentityType = "sandbox"
)

type ResolvedIdentity struct {
	IdentityID   string
	IdentityType IdentityType
	WorkloadID   string
	ZitiID       string
}

// SandboxID is the sandbox record a sandbox workload identity belongs to. A
// sandbox authenticates as its sandbox — Ziti Management registers the managed
// identity with the sandbox id as its identity id — so the two are one value,
// and callers that need the record read it from here rather than assuming an
// identity id doubles as one.
func (r ResolvedIdentity) SandboxID() string {
	if r.IdentityType != IdentityTypeSandbox {
		return ""
	}
	return strings.TrimSpace(r.IdentityID)
}

func ParseIdentityType(value string) (IdentityType, error) {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case string(IdentityTypeUser):
		return IdentityTypeUser, nil
	case string(IdentityTypeAgent):
		return IdentityTypeAgent, nil
	case string(IdentityTypeAgentInstance):
		return IdentityTypeAgentInstance, nil
	case string(IdentityTypeApp):
		return IdentityTypeApp, nil
	case string(IdentityTypeRunner):
		return IdentityTypeRunner, nil
	case string(IdentityTypeSandbox):
		return IdentityTypeSandbox, nil
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
