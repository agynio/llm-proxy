package apitokenresolver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	usersv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/users/v1"
	"github.com/agynio/llm-proxy/internal/identity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const apiTokenPrefix = "agyn_"

type Resolver struct {
	usersClient usersv1.UsersServiceClient
}

func NewResolver(usersClient usersv1.UsersServiceClient) *Resolver {
	return &Resolver{usersClient: usersClient}
}

func HasPrefix(token string) bool {
	return strings.HasPrefix(token, apiTokenPrefix)
}

func (r *Resolver) ResolveFromToken(ctx context.Context, accessToken string) (identity.ResolvedIdentity, error) {
	hash := sha256.Sum256([]byte(accessToken))
	tokenHash := hex.EncodeToString(hash[:])

	resp, err := r.usersClient.ResolveAPIToken(ctx, &usersv1.ResolveAPITokenRequest{TokenHash: tokenHash})
	if err != nil {
		switch status.Code(err) {
		case codes.NotFound, codes.Unauthenticated:
			return identity.ResolvedIdentity{}, status.Error(codes.Unauthenticated, "invalid api token")
		default:
			return identity.ResolvedIdentity{}, err
		}
	}
	if resp == nil {
		return identity.ResolvedIdentity{}, status.Error(codes.Internal, "api token response missing")
	}

	identityID := strings.TrimSpace(resp.GetIdentityId())
	if identityID == "" {
		return identity.ResolvedIdentity{}, status.Error(codes.Internal, "identity id missing")
	}

	return identity.ResolvedIdentity{IdentityID: identityID, IdentityType: identity.IdentityTypeUser}, nil
}
