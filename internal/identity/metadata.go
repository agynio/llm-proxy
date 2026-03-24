package identity

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/metadata"
)

const (
	MetadataKeyIdentityID   = "x-identity-id"
	MetadataKeyIdentityType = "x-identity-type"
)

func AppendToOutgoingContext(ctx context.Context) context.Context {
	resolved, ok := IdentityFromContext(ctx)
	if !ok {
		return ctx
	}

	return metadata.AppendToOutgoingContext(
		ctx,
		MetadataKeyIdentityID, resolved.IdentityID,
		MetadataKeyIdentityType, string(resolved.IdentityType),
	)
}

func IdentityFromIncomingContext(ctx context.Context) (ResolvedIdentity, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ResolvedIdentity{}, fmt.Errorf("missing identity metadata")
	}

	identityID, err := requiredMetadataValue(md, MetadataKeyIdentityID)
	if err != nil {
		return ResolvedIdentity{}, err
	}

	identityTypeValue, err := requiredMetadataValue(md, MetadataKeyIdentityType)
	if err != nil {
		return ResolvedIdentity{}, err
	}
	identityType, err := ParseIdentityType(identityTypeValue)
	if err != nil {
		return ResolvedIdentity{}, err
	}

	return ResolvedIdentity{
		IdentityID:   identityID,
		IdentityType: identityType,
	}, nil
}

func requiredMetadataValue(md metadata.MD, key string) (string, error) {
	values := md.Get(key)
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed, nil
		}
	}
	return "", fmt.Errorf("missing %s metadata", key)
}
