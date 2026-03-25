//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
	organizationsv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/organizations/v1"
	usersv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/users/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const (
	defaultUsersAddr         = "users:50051"
	defaultOrganizationsAddr = "tenants:50051"
	defaultLLMAddr           = "llm:50051"
	setupTimeout             = 30 * time.Second
	apiTokenName             = "e2e-llm-proxy"
	llmProviderEndpoint      = "https://testllm.dev/v1/org/agynio/suite/agn"
)

var (
	testAPIToken               string
	testModelID                string
	testUnauthorizedModelID    string
	testIdentityID             string
	testOrganizationID         string
	testUnauthorizedOrgID      string
	testUnauthorizedIdentityID string
)

func setupFixtures(ctx context.Context) error {
	usersAddr := envOrDefault("USERS_ADDR", defaultUsersAddr)
	orgAddr := envOrDefault("ORGANIZATIONS_ADDR", defaultOrganizationsAddr)
	llmAddr := envOrDefault("LLM_ADDR", defaultLLMAddr)

	usersConn, err := grpc.NewClient(usersAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connect users service: %w", err)
	}
	defer usersConn.Close()
	usersClient := usersv1.NewUsersServiceClient(usersConn)

	identityID, err := resolveOrCreateUser(ctx, usersClient, "e2e-llm-proxy-test", "E2E LLM Proxy", "e2e@test.local")
	if err != nil {
		return err
	}
	testIdentityID = identityID

	apiToken, err := createAPIToken(ctx, usersClient, identityID, apiTokenName)
	if err != nil {
		return err
	}
	testAPIToken = apiToken

	unauthorizedIdentityID, err := resolveOrCreateUser(ctx, usersClient, "e2e-llm-proxy-unauthorized", "E2E LLM Proxy Unauthorized", "e2e-unauthorized@test.local")
	if err != nil {
		return err
	}
	testUnauthorizedIdentityID = unauthorizedIdentityID

	orgConn, err := grpc.NewClient(orgAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connect organizations service: %w", err)
	}
	defer orgConn.Close()
	orgClient := organizationsv1.NewOrganizationsServiceClient(orgConn)

	orgID, err := createOrganization(ctx, orgClient, identityID, fmt.Sprintf("e2e-llm-proxy-org-%s", uuid.NewString()))
	if err != nil {
		return err
	}
	testOrganizationID = orgID

	unauthorizedOrgID, err := createOrganization(ctx, orgClient, unauthorizedIdentityID, fmt.Sprintf("e2e-llm-proxy-org-unauthorized-%s", uuid.NewString()))
	if err != nil {
		return err
	}
	testUnauthorizedOrgID = unauthorizedOrgID

	llmConn, err := grpc.NewClient(llmAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connect llm service: %w", err)
	}
	defer llmConn.Close()
	llmClient := llmv1.NewLLMServiceClient(llmConn)

	modelID, err := createModel(ctx, llmClient, orgID, "e2e-simple-hello")
	if err != nil {
		return err
	}
	testModelID = modelID

	unauthorizedModelID, err := createModel(ctx, llmClient, unauthorizedOrgID, "e2e-simple-hello-unauthorized")
	if err != nil {
		return err
	}
	testUnauthorizedModelID = unauthorizedModelID

	return nil
}

func resolveOrCreateUser(ctx context.Context, client usersv1.UsersServiceClient, subject string, name string, email string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()

	resp, err := client.ResolveOrCreateUser(callCtx, &usersv1.ResolveOrCreateUserRequest{
		OidcSubject: subject,
		Name:        name,
		Email:       email,
	})
	if err != nil {
		return "", fmt.Errorf("resolve user %s: %w", subject, err)
	}
	if resp == nil || resp.GetUser() == nil || resp.GetUser().GetMeta() == nil {
		return "", fmt.Errorf("resolve user %s: missing user metadata", subject)
	}

	identityID := strings.TrimSpace(resp.GetUser().GetMeta().GetId())
	if identityID == "" {
		return "", fmt.Errorf("resolve user %s: identity id missing", subject)
	}
	return identityID, nil
}

func createAPIToken(ctx context.Context, client usersv1.UsersServiceClient, identityID string, name string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()

	callCtx = withIdentity(callCtx, identityID)
	resp, err := client.CreateAPIToken(callCtx, &usersv1.CreateAPITokenRequest{Name: name})
	if err != nil {
		return "", fmt.Errorf("create api token: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("create api token: missing response")
	}
	token := strings.TrimSpace(resp.GetPlaintextToken())
	if token == "" {
		return "", fmt.Errorf("create api token: plaintext token missing")
	}
	return token, nil
}

func createOrganization(ctx context.Context, client organizationsv1.OrganizationsServiceClient, identityID string, name string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()

	callCtx = withIdentity(callCtx, identityID)
	resp, err := client.CreateOrganization(callCtx, &organizationsv1.CreateOrganizationRequest{Name: name})
	if err != nil {
		return "", fmt.Errorf("create organization %s: %w", name, err)
	}
	if resp == nil || resp.GetOrganization() == nil {
		return "", fmt.Errorf("create organization %s: missing organization", name)
	}
	orgID := strings.TrimSpace(resp.GetOrganization().GetId())
	if orgID == "" {
		return "", fmt.Errorf("create organization %s: id missing", name)
	}
	return orgID, nil
}

func createModel(ctx context.Context, client llmv1.LLMServiceClient, orgID string, name string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()

	providerResp, err := client.CreateLLMProvider(callCtx, &llmv1.CreateLLMProviderRequest{
		Endpoint:       llmProviderEndpoint,
		Token:          "not-needed",
		AuthMethod:     llmv1.AuthMethod_AUTH_METHOD_BEARER,
		OrganizationId: orgID,
	})
	if err != nil {
		return "", fmt.Errorf("create llm provider: %w", err)
	}
	if providerResp == nil || providerResp.GetProvider() == nil || providerResp.GetProvider().GetMeta() == nil {
		return "", fmt.Errorf("create llm provider: missing provider metadata")
	}
	providerID := strings.TrimSpace(providerResp.GetProvider().GetMeta().GetId())
	if providerID == "" {
		return "", fmt.Errorf("create llm provider: id missing")
	}

	modelResp, err := client.CreateModel(callCtx, &llmv1.CreateModelRequest{
		Name:           name,
		LlmProviderId:  providerID,
		RemoteName:     "simple-hello",
		OrganizationId: orgID,
	})
	if err != nil {
		return "", fmt.Errorf("create model %s: %w", name, err)
	}
	if modelResp == nil || modelResp.GetModel() == nil || modelResp.GetModel().GetMeta() == nil {
		return "", fmt.Errorf("create model %s: missing model metadata", name)
	}
	modelID := strings.TrimSpace(modelResp.GetModel().GetMeta().GetId())
	if modelID == "" {
		return "", fmt.Errorf("create model %s: id missing", name)
	}
	return modelID, nil
}

func withIdentity(ctx context.Context, identityID string) context.Context {
	md := metadata.New(map[string]string{"x-identity-id": identityID})
	return metadata.NewOutgoingContext(ctx, md)
}
