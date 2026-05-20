package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/agynio/llm-proxy/internal/identity"
	"github.com/agynio/llm-proxy/internal/ziticonn"
)

type stubIdentityResolver struct {
	resolved identity.ResolvedIdentity
	called   bool
}

func (r *stubIdentityResolver) ResolveIdentity(_ context.Context, sourceIdentity string) (identity.ResolvedIdentity, error) {
	r.called = true
	r.resolved.ZitiID = sourceIdentity
	return r.resolved, nil
}

type stubBearerResolver struct {
	resolved identity.ResolvedIdentity
	called   bool
	err      error
}

func (r *stubBearerResolver) ResolveFromToken(context.Context, string) (identity.ResolvedIdentity, error) {
	r.called = true
	if r.err != nil {
		return identity.ResolvedIdentity{}, r.err
	}
	return r.resolved, nil
}

func TestResolveIdentityPrefersBearerTokenOverZitiSourceIdentity(t *testing.T) {
	zitiResolver := &stubIdentityResolver{resolved: identity.ResolvedIdentity{IdentityID: "agent-1", IdentityType: identity.IdentityTypeAgent}}
	apiTokenResolver := &stubBearerResolver{resolved: identity.ResolvedIdentity{IdentityID: "user-1", IdentityType: identity.IdentityTypeUser}}

	ctx := ziticonn.WithSourceIdentity(context.Background(), "ziti-agent-identity")
	resolvedCtx, err := resolveIdentity(ctx, "Bearer agyn_test-token", zitiResolver, apiTokenResolver)
	if err != nil {
		t.Fatalf("resolve identity: %v", err)
	}
	if !apiTokenResolver.called {
		t.Fatal("expected api token resolver to be called")
	}
	if zitiResolver.called {
		t.Fatal("expected ziti resolver not to be called")
	}

	resolved, ok := identity.IdentityFromContext(resolvedCtx)
	if !ok {
		t.Fatal("resolved identity missing from context")
	}
	if resolved.IdentityID != "user-1" || resolved.IdentityType != identity.IdentityTypeUser {
		t.Fatalf("unexpected identity: %+v", resolved)
	}
}

func TestResolveIdentityUsesZitiWhenBearerMissing(t *testing.T) {
	zitiResolver := &stubIdentityResolver{resolved: identity.ResolvedIdentity{IdentityID: "agent-1", IdentityType: identity.IdentityTypeAgent}}
	apiTokenResolver := &stubBearerResolver{resolved: identity.ResolvedIdentity{IdentityID: "user-1", IdentityType: identity.IdentityTypeUser}}

	ctx := ziticonn.WithSourceIdentity(context.Background(), "ziti-agent-identity")
	resolvedCtx, err := resolveIdentity(ctx, "", zitiResolver, apiTokenResolver)
	if err != nil {
		t.Fatalf("resolve identity: %v", err)
	}
	if !zitiResolver.called {
		t.Fatal("expected ziti resolver to be called")
	}
	if apiTokenResolver.called {
		t.Fatal("expected api token resolver not to be called")
	}

	resolved, ok := identity.IdentityFromContext(resolvedCtx)
	if !ok {
		t.Fatal("resolved identity missing from context")
	}
	if resolved.IdentityID != "agent-1" || resolved.IdentityType != identity.IdentityTypeAgent {
		t.Fatalf("unexpected identity: %+v", resolved)
	}
	if resolved.ZitiID != "ziti-agent-identity" {
		t.Fatalf("expected ziti id to be preserved, got %q", resolved.ZitiID)
	}
}

func TestResolveIdentityReturnsBearerTokenErrorBeforeZitiFallback(t *testing.T) {
	zitiResolver := &stubIdentityResolver{resolved: identity.ResolvedIdentity{IdentityID: "agent-1", IdentityType: identity.IdentityTypeAgent}}
	apiTokenResolver := &stubBearerResolver{err: errors.New("invalid token")}

	ctx := ziticonn.WithSourceIdentity(context.Background(), "ziti-agent-identity")
	_, err := resolveIdentity(ctx, "Bearer agyn_invalid", zitiResolver, apiTokenResolver)
	if err == nil || err.Error() != "invalid token" {
		t.Fatalf("expected invalid token error, got %v", err)
	}
	if !apiTokenResolver.called {
		t.Fatal("expected api token resolver to be called")
	}
	if zitiResolver.called {
		t.Fatal("expected ziti resolver not to be called")
	}
}
