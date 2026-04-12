package auth

import (
	"context"
	"errors"
	"net/http"

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
			ctx, err := resolveIdentity(r.Context(), r.Header.Get("Authorization"), zitiResolver, apiTokenResolver)
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func resolveIdentity(ctx context.Context, authHeader string, zitiResolver IdentityResolver, apiTokenResolver BearerTokenResolver) (context.Context, error) {
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

	accessToken, ok := httpauth.ExtractBearerToken(authHeader)
	if !ok {
		return ctx, errors.New("authorization required")
	}
	if !apitokenresolver.HasPrefix(accessToken) {
		return ctx, errors.New("unsupported bearer token")
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
