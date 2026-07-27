package proxy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	agentsv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/agents/v1"
	"github.com/agynio/llm-proxy/internal/identity"
	"google.golang.org/grpc"
)

// ErrSandboxUnusable is returned when a sandbox identity authenticates but the
// record behind it cannot stand in for an organization and an owner.
var ErrSandboxUnusable = errors.New("sandbox is not usable")

// SandboxResolver reads the sandbox record behind a sandbox workload identity.
// It is the Agents service.
type SandboxResolver interface {
	GetSandbox(ctx context.Context, in *agentsv1.GetSandboxRequest, opts ...grpc.CallOption) (*agentsv1.GetSandboxResponse, error)
}

// sandboxPrincipal is what a sandbox identity resolves to: the record it is, the
// organization the usage is metered to, and the owner whose model access it
// borrows. The zero value means the caller is not a sandbox.
type sandboxPrincipal struct {
	sandboxID      string
	ownerID        string
	organizationID string
}

func (s sandboxPrincipal) isSandbox() bool {
	return s.sandboxID != ""
}

// resolveSandboxPrincipal turns a sandbox workload identity into the
// organization and owner behind it. A sandbox is not an organization member and
// holds no tuple of its own, so there is nothing to check it against directly —
// its record is the only thing that says which organization it belongs to and
// who is answerable for what it spends.
//
// The record is read on every call rather than cached: the call already makes a
// model resolution and an authorization check over the same mesh, and a cache
// would keep answering for a sandbox that has since been terminated.
func (h *Handler) resolveSandboxPrincipal(ctx context.Context, resolved identity.ResolvedIdentity) (sandboxPrincipal, error) {
	sandboxID := resolved.SandboxID()
	if sandboxID == "" {
		return sandboxPrincipal{}, nil
	}
	if h.sandboxResolver == nil {
		return sandboxPrincipal{}, fmt.Errorf("%w: no sandbox resolver configured", ErrSandboxUnusable)
	}

	response, err := h.sandboxResolver.GetSandbox(ctx, &agentsv1.GetSandboxRequest{
		Ref: &agentsv1.GetSandboxRequest_Id{Id: sandboxID},
	})
	if err != nil {
		return sandboxPrincipal{}, err
	}

	sandbox := response.GetSandbox()
	principal := sandboxPrincipal{
		sandboxID:      sandboxID,
		ownerID:        strings.TrimSpace(sandbox.GetOwnerId()),
		organizationID: strings.TrimSpace(sandbox.GetOrganizationId()),
	}
	if principal.ownerID == "" || principal.organizationID == "" {
		return sandboxPrincipal{}, fmt.Errorf("%w: sandbox %s has no organization or owner", ErrSandboxUnusable, sandboxID)
	}
	if sandbox.GetStatus() == agentsv1.SandboxStatus_SANDBOX_STATUS_TERMINATED {
		return sandboxPrincipal{}, fmt.Errorf("%w: sandbox %s is terminated", ErrSandboxUnusable, sandboxID)
	}

	return principal, nil
}
