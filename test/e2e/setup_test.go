//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
	organizationsv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/organizations/v1"
	usersv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/users/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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
	testAPIToken            string
	testModelID             string
	testUnauthorizedModelID string
)

func setupFixtures(ctx context.Context) (func(), error) {
	usersAddr := envOrDefault("USERS_ADDR", defaultUsersAddr)
	orgAddr := envOrDefault("ORGANIZATIONS_ADDR", defaultOrganizationsAddr)
	llmAddr := envOrDefault("LLM_ADDR", defaultLLMAddr)

	usersConn, err := grpc.NewClient(usersAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect users service: %w", err)
	}
	defer usersConn.Close()
	usersClient := usersv1.NewUsersServiceClient(usersConn)

	identityID, err := resolveOrCreateUser(ctx, usersClient, "e2e-llm-proxy-test", "E2E LLM Proxy", "e2e@test.local")
	if err != nil {
		return nil, err
	}

	apiToken, apiTokenID, err := createAPIToken(ctx, usersClient, identityID, apiTokenName)
	if err != nil {
		return nil, err
	}
	testAPIToken = apiToken

	unauthorizedIdentityID, err := resolveOrCreateUser(ctx, usersClient, "e2e-llm-proxy-unauthorized", "E2E LLM Proxy Unauthorized", "e2e-unauthorized@test.local")
	if err != nil {
		return nil, err
	}

	orgConn, err := grpc.NewClient(orgAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect organizations service: %w", err)
	}
	defer orgConn.Close()
	orgClient := organizationsv1.NewOrganizationsServiceClient(orgConn)

	orgID, err := createOrganization(ctx, orgClient, identityID, fmt.Sprintf("e2e-llm-proxy-org-%s", uuid.NewString()))
	if err != nil {
		return nil, err
	}

	unauthorizedOrgID, err := createOrganization(ctx, orgClient, unauthorizedIdentityID, fmt.Sprintf("e2e-llm-proxy-org-unauthorized-%s", uuid.NewString()))
	if err != nil {
		return nil, err
	}

	llmConn, err := grpc.NewClient(llmAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("connect llm service: %w", err)
	}
	defer llmConn.Close()
	llmClient := llmv1.NewLLMServiceClient(llmConn)

	modelID, providerID, err := createModel(ctx, llmClient, orgID, "e2e-simple-hello")
	if err != nil {
		return nil, err
	}
	testModelID = modelID

	unauthorizedModelID, unauthorizedProviderID, err := createModel(ctx, llmClient, unauthorizedOrgID, "e2e-simple-hello-unauthorized")
	if err != nil {
		return nil, err
	}
	testUnauthorizedModelID = unauthorizedModelID

	cleanup := func() {
		cleanupCtx := context.Background()
		cleanupLLM(cleanupCtx, llmAddr, []string{testModelID, testUnauthorizedModelID}, []string{providerID, unauthorizedProviderID})
		cleanupOrganizations(cleanupCtx, orgAddr, []orgCleanupSpec{
			{organizationID: orgID, identityID: identityID},
			{organizationID: unauthorizedOrgID, identityID: unauthorizedIdentityID},
		})
		cleanupAPIToken(cleanupCtx, usersAddr, identityID, apiTokenID)
	}

	return cleanup, nil
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

func createAPIToken(ctx context.Context, client usersv1.UsersServiceClient, identityID string, name string) (string, string, error) {
	callCtx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()

	callCtx = withIdentity(callCtx, identityID)
	resp, err := client.CreateAPIToken(callCtx, &usersv1.CreateAPITokenRequest{Name: name})
	if err != nil {
		return "", "", fmt.Errorf("create api token: %w", err)
	}
	if resp == nil {
		return "", "", fmt.Errorf("create api token: missing response")
	}
	if resp.GetToken() == nil {
		return "", "", fmt.Errorf("create api token: missing token metadata")
	}
	tokenID := strings.TrimSpace(resp.GetToken().GetId())
	if tokenID == "" {
		return "", "", fmt.Errorf("create api token: token id missing")
	}
	token := strings.TrimSpace(resp.GetPlaintextToken())
	if token == "" {
		return "", "", fmt.Errorf("create api token: plaintext token missing")
	}
	return token, tokenID, nil
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

func createModel(ctx context.Context, client llmv1.LLMServiceClient, orgID string, name string) (string, string, error) {
	callCtx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()

	providerResp, err := client.CreateLLMProvider(callCtx, &llmv1.CreateLLMProviderRequest{
		Endpoint:       llmProviderEndpoint,
		Token:          "not-needed",
		AuthMethod:     llmv1.AuthMethod_AUTH_METHOD_BEARER,
		OrganizationId: orgID,
	})
	if err != nil {
		return "", "", fmt.Errorf("create llm provider: %w", err)
	}
	if providerResp == nil || providerResp.GetProvider() == nil || providerResp.GetProvider().GetMeta() == nil {
		return "", "", fmt.Errorf("create llm provider: missing provider metadata")
	}
	providerID := strings.TrimSpace(providerResp.GetProvider().GetMeta().GetId())
	if providerID == "" {
		return "", "", fmt.Errorf("create llm provider: id missing")
	}

	modelResp, err := client.CreateModel(callCtx, &llmv1.CreateModelRequest{
		Name:           name,
		LlmProviderId:  providerID,
		RemoteName:     "simple-hello",
		OrganizationId: orgID,
	})
	if err != nil {
		return "", "", fmt.Errorf("create model %s: %w", name, err)
	}
	if modelResp == nil || modelResp.GetModel() == nil || modelResp.GetModel().GetMeta() == nil {
		return "", "", fmt.Errorf("create model %s: missing model metadata", name)
	}
	modelID := strings.TrimSpace(modelResp.GetModel().GetMeta().GetId())
	if modelID == "" {
		return "", "", fmt.Errorf("create model %s: id missing", name)
	}
	return modelID, providerID, nil
}

func withIdentity(ctx context.Context, identityID string) context.Context {
	md := metadata.New(map[string]string{"x-identity-id": identityID})
	return metadata.NewOutgoingContext(ctx, md)
}

type orgCleanupSpec struct {
	organizationID string
	identityID     string
}

func cleanupAPIToken(ctx context.Context, addr string, identityID string, tokenID string) {
	if tokenID == "" {
		return
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logCleanupError("connect users service", err)
		return
	}
	defer conn.Close()
	client := usersv1.NewUsersServiceClient(conn)

	callCtx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	callCtx = withIdentity(callCtx, identityID)
	_, err = client.RevokeAPIToken(callCtx, &usersv1.RevokeAPITokenRequest{TokenId: tokenID})
	logCleanupError("revoke api token", err)
}

func cleanupOrganizations(ctx context.Context, addr string, specs []orgCleanupSpec) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logCleanupError("connect organizations service", err)
		return
	}
	defer conn.Close()
	client := organizationsv1.NewOrganizationsServiceClient(conn)

	for _, spec := range specs {
		if spec.organizationID == "" {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, setupTimeout)
		callCtx = withIdentity(callCtx, spec.identityID)
		_, err := client.DeleteOrganization(callCtx, &organizationsv1.DeleteOrganizationRequest{Id: spec.organizationID})
		cancel()
		logCleanupError("delete organization", err)
	}
}

func cleanupLLM(ctx context.Context, addr string, modelIDs []string, providerIDs []string) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logCleanupError("connect llm service", err)
		return
	}
	defer conn.Close()
	client := llmv1.NewLLMServiceClient(conn)

	for _, modelID := range modelIDs {
		if modelID == "" {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, setupTimeout)
		_, err := client.DeleteModel(callCtx, &llmv1.DeleteModelRequest{Id: modelID})
		cancel()
		logCleanupError("delete model", err)
	}

	for _, providerID := range providerIDs {
		if providerID == "" {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, setupTimeout)
		_, err := client.DeleteLLMProvider(callCtx, &llmv1.DeleteLLMProviderRequest{Id: providerID})
		cancel()
		logCleanupError("delete llm provider", err)
	}
}

func logCleanupError(action string, err error) {
	if err == nil {
		return
	}
	code := status.Code(err)
	if code == codes.Unimplemented || code == codes.NotFound {
		return
	}
	fmt.Fprintf(os.Stderr, "e2e cleanup %s: %v\n", action, err)
}
