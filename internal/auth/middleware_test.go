package auth

import (
	"context"
	"testing"

	"github.com/agynio/llm-proxy/internal/identity"
)

type stubBearerResolver struct {
	called    bool
	lastToken string
	result    identity.ResolvedIdentity
}

func (s *stubBearerResolver) ResolveFromToken(_ context.Context, accessToken string) (identity.ResolvedIdentity, error) {
	s.called = true
	s.lastToken = accessToken
	return s.result, nil
}

func TestResolveIdentityUsesAPIKeyHeader(t *testing.T) {
	resolver := &stubBearerResolver{result: identity.ResolvedIdentity{IdentityID: "user-1", IdentityType: identity.IdentityTypeUser}}

	ctx, err := resolveIdentity(context.Background(), "", "agyn_api", nil, resolver)
	if err != nil {
		t.Fatalf("resolve identity: %v", err)
	}
	if !resolver.called {
		t.Fatalf("expected resolver to be called")
	}
	if resolver.lastToken != "agyn_api" {
		t.Fatalf("expected token %q, got %q", "agyn_api", resolver.lastToken)
	}
	resolved, ok := identity.IdentityFromContext(ctx)
	if !ok {
		t.Fatalf("expected identity in context")
	}
	if resolved.IdentityID != "user-1" {
		t.Fatalf("expected identity id %q, got %q", "user-1", resolved.IdentityID)
	}
}

func TestResolveIdentityPrefersAuthorizationHeader(t *testing.T) {
	resolver := &stubBearerResolver{result: identity.ResolvedIdentity{IdentityID: "user-2", IdentityType: identity.IdentityTypeUser}}

	ctx, err := resolveIdentity(context.Background(), "Bearer agyn_auth", "agyn_api", nil, resolver)
	if err != nil {
		t.Fatalf("resolve identity: %v", err)
	}
	if !resolver.called {
		t.Fatalf("expected resolver to be called")
	}
	if resolver.lastToken != "agyn_auth" {
		t.Fatalf("expected token %q, got %q", "agyn_auth", resolver.lastToken)
	}
	resolved, ok := identity.IdentityFromContext(ctx)
	if !ok {
		t.Fatalf("expected identity in context")
	}
	if resolved.IdentityID != "user-2" {
		t.Fatalf("expected identity id %q, got %q", "user-2", resolved.IdentityID)
	}
}

func TestResolveIdentityRejectsBlankAPIKey(t *testing.T) {
	_, err := resolveIdentity(context.Background(), "", " \t ", nil, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Error() != "authorization required" {
		t.Fatalf("expected authorization required error, got %q", err.Error())
	}
}

func TestResolveIdentityRejectsAPIKeyWithoutPrefix(t *testing.T) {
	_, err := resolveIdentity(context.Background(), "", "api_token", nil, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Error() != "unsupported bearer token" {
		t.Fatalf("expected unsupported bearer token error, got %q", err.Error())
	}
}
