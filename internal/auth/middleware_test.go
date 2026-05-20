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

func (r *stubIdentityResolver) ResolveIdentity(context.Context, string) (identity.ResolvedIdentity, error) {
	r.called = true
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

func TestResolveIdentityPrefersAuthorizationBearerOverZitiSourceIdentity(t *testing.T) {
	zitiResolver := &stubIdentityResolver{resolved: identity.ResolvedIdentity{IdentityID: "agent-1", IdentityType: identity.IdentityTypeAgent}}
	apiTokenResolver := &stubBearerResolver{resolved: identity.ResolvedIdentity{IdentityID: "user-1", IdentityType: identity.IdentityTypeUser}}

	ctx := ziticonn.WithSourceIdentity(context.Background(), "ziti-agent-identity")
	resolvedCtx, err := resolveIdentity(ctx, proxyAuthHeaders{authorization: "Bearer agyn_test-token"}, zitiResolver, apiTokenResolver)
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

func TestResolveIdentityUsesXAPIKeyWhenBearerMissing(t *testing.T) {
	zitiResolver := &stubIdentityResolver{resolved: identity.ResolvedIdentity{IdentityID: "agent-1", IdentityType: identity.IdentityTypeAgent}}
	apiTokenResolver := &stubBearerResolver{resolved: identity.ResolvedIdentity{IdentityID: "user-1", IdentityType: identity.IdentityTypeUser}}

	ctx := ziticonn.WithSourceIdentity(context.Background(), "ziti-agent-identity")
	resolvedCtx, err := resolveIdentity(ctx, proxyAuthHeaders{xAPIKey: " agyn_api-key-token "}, zitiResolver, apiTokenResolver)
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

func TestResolveIdentityPrefersAuthorizationBearerOverXAPIKey(t *testing.T) {
	apiTokenResolver := &stubBearerResolver{resolved: identity.ResolvedIdentity{IdentityID: "user-1", IdentityType: identity.IdentityTypeUser}}

	_, err := resolveIdentity(context.Background(), proxyAuthHeaders{
		authorization: "Bearer agyn_bearer-token",
		xAPIKey:       "not-agyn-token",
	}, nil, apiTokenResolver)
	if err != nil {
		t.Fatalf("resolve identity: %v", err)
	}
	if !apiTokenResolver.called {
		t.Fatal("expected api token resolver to be called")
	}
}

func TestResolveIdentityUsesZitiWhenHTTPAuthMissing(t *testing.T) {
	zitiResolver := &stubIdentityResolver{resolved: identity.ResolvedIdentity{IdentityID: "agent-1", IdentityType: identity.IdentityTypeAgent}}
	apiTokenResolver := &stubBearerResolver{resolved: identity.ResolvedIdentity{IdentityID: "user-1", IdentityType: identity.IdentityTypeUser}}

	ctx := ziticonn.WithSourceIdentity(context.Background(), "ziti-agent-identity")
	resolvedCtx, err := resolveIdentity(ctx, proxyAuthHeaders{}, zitiResolver, apiTokenResolver)
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
}

func TestResolveIdentityReturnsBearerTokenErrorBeforeZitiFallback(t *testing.T) {
	zitiResolver := &stubIdentityResolver{resolved: identity.ResolvedIdentity{IdentityID: "agent-1", IdentityType: identity.IdentityTypeAgent}}
	apiTokenResolver := &stubBearerResolver{err: errors.New("invalid token")}

	ctx := ziticonn.WithSourceIdentity(context.Background(), "ziti-agent-identity")
	_, err := resolveIdentity(ctx, proxyAuthHeaders{authorization: "Bearer agyn_invalid"}, zitiResolver, apiTokenResolver)
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

func TestResolveIdentityReturnsXAPIKeyErrorBeforeZitiFallback(t *testing.T) {
	zitiResolver := &stubIdentityResolver{resolved: identity.ResolvedIdentity{IdentityID: "agent-1", IdentityType: identity.IdentityTypeAgent}}
	apiTokenResolver := &stubBearerResolver{err: errors.New("invalid token")}

	ctx := ziticonn.WithSourceIdentity(context.Background(), "ziti-agent-identity")
	_, err := resolveIdentity(ctx, proxyAuthHeaders{xAPIKey: "agyn_invalid"}, zitiResolver, apiTokenResolver)
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
