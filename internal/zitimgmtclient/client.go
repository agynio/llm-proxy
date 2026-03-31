package zitimgmtclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/agynio/llm-proxy/internal/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	identityv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/identity/v1"
	zitimgmtv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/ziti_management/v1"
)

type Client struct {
	conn   *grpc.ClientConn
	client zitimgmtv1.ZitiManagementServiceClient
}

func NewClient(target string) (*Client, error) {
	if strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("target is required")
	}

	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	return &Client{
		conn:   conn,
		client: zitimgmtv1.NewZitiManagementServiceClient(conn),
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) RequestServiceIdentity(ctx context.Context, serviceType zitimgmtv1.ServiceType) (string, []byte, error) {
	if serviceType == zitimgmtv1.ServiceType_SERVICE_TYPE_UNSPECIFIED {
		return "", nil, fmt.Errorf("service type is required")
	}

	response, err := c.client.RequestServiceIdentity(ctx, &zitimgmtv1.RequestServiceIdentityRequest{
		ServiceType: serviceType,
	})
	if err != nil {
		return "", nil, err
	}

	identityID := strings.TrimSpace(response.GetZitiIdentityId())
	if identityID == "" {
		return "", nil, fmt.Errorf("ziti identity id missing")
	}

	identityJSON := response.GetIdentityJson()
	if len(identityJSON) == 0 {
		return "", nil, fmt.Errorf("identity json missing")
	}

	return identityID, identityJSON, nil
}

func (c *Client) ExtendIdentityLease(ctx context.Context, zitiIdentityID string) error {
	trimmed := strings.TrimSpace(zitiIdentityID)
	if trimmed == "" {
		return fmt.Errorf("ziti identity id is required")
	}

	_, err := c.client.ExtendIdentityLease(ctx, &zitimgmtv1.ExtendIdentityLeaseRequest{
		ZitiIdentityId: trimmed,
	})
	return err
}

func (c *Client) ResolveIdentity(ctx context.Context, sourceIdentity string) (identity.ResolvedIdentity, error) {
	trimmed := strings.TrimSpace(sourceIdentity)
	if trimmed == "" {
		return identity.ResolvedIdentity{}, fmt.Errorf("source identity is required")
	}

	response, err := c.client.ResolveIdentity(ctx, &zitimgmtv1.ResolveIdentityRequest{ZitiIdentityId: trimmed})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			if resolved, ok := identity.ParseManagedIdentityName(trimmed); ok {
				return resolved, nil
			}
		}
		return identity.ResolvedIdentity{}, err
	}

	identityID := strings.TrimSpace(response.GetIdentityId())
	if identityID == "" {
		return identity.ResolvedIdentity{}, fmt.Errorf("identity id missing")
	}

	identityType, err := parseIdentityType(response.GetIdentityType())
	if err != nil {
		return identity.ResolvedIdentity{}, err
	}

	return identity.ResolvedIdentity{
		IdentityID:   identityID,
		IdentityType: identityType,
	}, nil
}

func parseIdentityType(identityType identityv1.IdentityType) (identity.IdentityType, error) {
	switch identityType {
	case identityv1.IdentityType_IDENTITY_TYPE_AGENT:
		return identity.IdentityTypeAgent, nil
	case identityv1.IdentityType_IDENTITY_TYPE_RUNNER:
		return identity.IdentityTypeRunner, nil
	case identityv1.IdentityType_IDENTITY_TYPE_CHANNEL:
		return identity.IdentityTypeChannel, nil
	case identityv1.IdentityType_IDENTITY_TYPE_USER:
		return identity.IdentityTypeUser, nil
	case identityv1.IdentityType_IDENTITY_TYPE_UNSPECIFIED:
		return "", fmt.Errorf("identity type unspecified")
	default:
		return "", fmt.Errorf("identity type unsupported: %s", identityType.String())
	}
}
