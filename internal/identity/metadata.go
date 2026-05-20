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
	MetadataKeyWorkloadID   = "x-workload-id"
	MetadataKeyZitiID       = "x-ziti-identity-id"
)

func AppendToOutgoingContext(ctx context.Context) context.Context {
	resolved, ok := IdentityFromContext(ctx)
	if !ok {
		return ctx
	}

	pairs := []string{
		MetadataKeyIdentityID, resolved.IdentityID,
		MetadataKeyIdentityType, string(resolved.IdentityType),
	}
	if resolved.WorkloadID != "" {
		pairs = append(pairs, MetadataKeyWorkloadID, resolved.WorkloadID)
	}
	if resolved.ZitiID != "" {
		pairs = append(pairs, MetadataKeyZitiID, resolved.ZitiID)
	}

	return metadata.AppendToOutgoingContext(ctx, pairs...)
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
		WorkloadID:   optionalMetadataValue(md, MetadataKeyWorkloadID),
		ZitiID:       optionalMetadataValue(md, MetadataKeyZitiID),
	}, nil
}

func optionalMetadataValue(md metadata.MD, key string) string {
	values := md.Get(key)
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
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
