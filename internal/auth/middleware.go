package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/agynio/llm-proxy/internal/apitokenresolver"
	"github.com/agynio/llm-proxy/internal/httpauth"
	"github.com/agynio/llm-proxy/internal/identity"
	"github.com/agynio/llm-proxy/internal/ziticonn"
)

type IdentityResolver interface {
	ResolveIdentity(ctx context.Context, sourceIdentity string) (identity.ResolvedIdentity, error)
}

type BearerTokenResolver interface {
	ResolveFromToken(ctx context.Context, accessToken string) (identity.ResolvedIdentity, error)
}

func Middleware(zitiResolver IdentityResolver, apiTokenResolver BearerTokenResolver) func(http.Handler) http.Handler {
	if zitiResolver == nil && apiTokenResolver == nil {
		panic("at least one identity resolver is required")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, err := resolveIdentity(r.Context(), proxyAuthHeaders{
				authorization: r.Header.Get("Authorization"),
				xAPIKey:       r.Header.Get("x-api-key"),
			}, zitiResolver, apiTokenResolver)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type proxyAuthHeaders struct {
	authorization string
	xAPIKey       string
}

func resolveIdentity(ctx context.Context, headers proxyAuthHeaders, zitiResolver IdentityResolver, apiTokenResolver BearerTokenResolver) (context.Context, error) {
	accessToken, bearerOK := httpauth.ExtractBearerToken(headers.authorization)
	if bearerOK {
		return resolveAPIToken(ctx, accessToken, apiTokenResolver)
	}

	apiKey := strings.TrimSpace(headers.xAPIKey)
	if apiKey != "" {
		return resolveAPIToken(ctx, apiKey, apiTokenResolver)
	}

	sourceIdentity, ok := ziticonn.SourceIdentityFromContext(ctx)
	if ok {
		if zitiResolver == nil {
			return ctx, errors.New("ziti identity resolver is not configured")
		}
		resolved, err := zitiResolver.ResolveIdentity(ctx, sourceIdentity)
		if err != nil {
			return ctx, err
		}
		return identity.WithIdentity(ctx, resolved), nil
	}

	return ctx, errors.New("authorization required")
}

func resolveAPIToken(ctx context.Context, accessToken string, apiTokenResolver BearerTokenResolver) (context.Context, error) {
	if !apitokenresolver.HasPrefix(accessToken) {
		return ctx, errors.New("unsupported api token")
	}
	if apiTokenResolver == nil {
		return ctx, errors.New("api token resolver is not configured")
	}

	resolved, err := apiTokenResolver.ResolveFromToken(ctx, accessToken)
	if err != nil {
		return ctx, err
	}

	return identity.WithIdentity(ctx, resolved), nil
}
