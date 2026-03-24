package httpauth

import (
	"context"
	"strings"
)

type contextKey struct{}

func WithBearerToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, contextKey{}, token)
}

func BearerTokenFromContext(ctx context.Context) (string, bool) {
	token, ok := ctx.Value(contextKey{}).(string)
	if !ok {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	return token, true
}

func ExtractBearerToken(authHeader string) (string, bool) {
	header := strings.TrimSpace(authHeader)
	if header == "" {
		return "", false
	}

	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return "", false
	}
	if !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	if strings.ContainsAny(token, " \t\n\r") {
		return "", false
	}
	return token, true
}
