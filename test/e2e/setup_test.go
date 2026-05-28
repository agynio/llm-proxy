//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

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
	defaultOrganizationsAddr = "organizations:50051"
	defaultGatewayBaseURL    = "http://gateway-gateway.platform.svc.cluster.local:8080"
	setupTimeout             = 30 * time.Second
	apiTokenName             = "e2e-llm-proxy"
	llmProviderEndpoint      = "https://testllm.dev/v1/org/agynio/suite/agn/responses"
	llmGatewayServicePath    = "agynio.api.gateway.v1.LLMGateway"
	metadataIdentityIDKey    = "x-identity-id"
	metadataIdentityTypeKey  = "x-identity-type"
	identityTypeUser         = "user"
)

var (
	testAPIToken            string
	testModelID             string
	testUnauthorizedModelID string
)

func setupFixtures(ctx context.Context) (func(), error) {
	usersAddr := envOrDefault("USERS_ADDR", defaultUsersAddr)
	orgAddr := envOrDefault("ORGANIZATIONS_ADDR", defaultOrganizationsAddr)
	gatewayBaseURL, err := parseGatewayBaseURL(envOrDefault("AGYN_BASE_URL", defaultGatewayBaseURL))
	if err != nil {
		return nil, err
	}

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

	modelID, providerID, err := createModel(ctx, gatewayBaseURL, apiToken, orgID, "e2e-simple-hello")
	if err != nil {
		return nil, err
	}
	testModelID = modelID

	unauthorizedAPIToken, unauthorizedAPITokenID, err := createAPIToken(ctx, usersClient, unauthorizedIdentityID, apiTokenName+"-unauthorized")
	if err != nil {
		return nil, err
	}
	unauthorizedModelID, unauthorizedProviderID, err := createModel(ctx, gatewayBaseURL, unauthorizedAPIToken, unauthorizedOrgID, "e2e-simple-hello-unauthorized")
	if err != nil {
		return nil, err
	}
	testUnauthorizedModelID = unauthorizedModelID

	cleanup := func() {
		cleanupCtx := context.Background()
		cleanupLLM(cleanupCtx, gatewayBaseURL, []llmCleanupSpec{
			{modelID: testModelID, providerID: providerID, apiToken: apiToken},
			{modelID: testUnauthorizedModelID, providerID: unauthorizedProviderID, apiToken: unauthorizedAPIToken},
		})
		cleanupOrganizations(cleanupCtx, orgAddr, []orgCleanupSpec{
			{organizationID: orgID, identityID: identityID},
			{organizationID: unauthorizedOrgID, identityID: unauthorizedIdentityID},
		})
		cleanupAPIToken(cleanupCtx, usersAddr, unauthorizedIdentityID, unauthorizedAPITokenID)
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

func createModel(ctx context.Context, gatewayBaseURL string, apiToken string, orgID string, name string) (string, string, error) {
	callCtx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()

	providerResp, err := postGatewayConnect[gatewayCreateLLMProviderResponse](callCtx, gatewayBaseURL, apiToken, "CreateLLMProvider", map[string]string{
		"endpoint":       llmProviderEndpoint,
		"token":          "not-needed",
		"authMethod":     "AUTH_METHOD_BEARER",
		"organizationId": orgID,
		"protocol":       "PROTOCOL_RESPONSES",
	})
	if err != nil {
		return "", "", fmt.Errorf("create llm provider: %w", err)
	}
	providerID := strings.TrimSpace(providerResp.Provider.Meta.ID)
	if providerID == "" {
		return "", "", fmt.Errorf("create llm provider: id missing")
	}

	modelResp, err := postGatewayConnect[gatewayCreateModelResponse](callCtx, gatewayBaseURL, apiToken, "CreateModel", map[string]string{
		"name":           name,
		"llmProviderId":  providerID,
		"remoteName":     "simple-hello",
		"organizationId": orgID,
	})
	if err != nil {
		return "", "", fmt.Errorf("create model %s: %w", name, err)
	}
	modelID := strings.TrimSpace(modelResp.Model.Meta.ID)
	if modelID == "" {
		return "", "", fmt.Errorf("create model %s: id missing", name)
	}
	return modelID, providerID, nil
}

func withIdentity(ctx context.Context, identityID string) context.Context {
	md := metadata.New(map[string]string{
		metadataIdentityIDKey:   identityID,
		metadataIdentityTypeKey: identityTypeUser,
	})
	return metadata.NewOutgoingContext(ctx, md)
}

type orgCleanupSpec struct {
	organizationID string
	identityID     string
}

type llmCleanupSpec struct {
	modelID    string
	providerID string
	apiToken   string
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

func cleanupLLM(ctx context.Context, gatewayBaseURL string, specs []llmCleanupSpec) {
	for _, spec := range specs {
		if spec.modelID == "" {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, setupTimeout)
		postGatewayConnectBestEffort(callCtx, gatewayBaseURL, spec.apiToken, "DeleteModel", map[string]string{"id": spec.modelID})
		cancel()
	}

	for _, spec := range specs {
		if spec.providerID == "" {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, setupTimeout)
		postGatewayConnectBestEffort(callCtx, gatewayBaseURL, spec.apiToken, "DeleteLLMProvider", map[string]string{"id": spec.providerID})
		cancel()
	}
}

func postGatewayConnect[T any](ctx context.Context, gatewayBaseURL string, apiToken string, method string, payload any) (T, error) {
	var response T
	body, statusCode, err := postGatewayConnectRaw(ctx, gatewayBaseURL, apiToken, method, payload)
	if err != nil {
		return response, err
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return response, fmt.Errorf("gateway %s failed with status %d: %s", method, statusCode, body)
	}
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		return response, fmt.Errorf("decode gateway %s response: %w", method, err)
	}
	return response, nil
}

func postGatewayConnectBestEffort(ctx context.Context, gatewayBaseURL string, apiToken string, method string, payload any) {
	_, _, _ = postGatewayConnectRaw(ctx, gatewayBaseURL, apiToken, method, payload)
}

func postGatewayConnectRaw(ctx context.Context, gatewayBaseURL string, apiToken string, method string, payload any) (string, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", 0, fmt.Errorf("marshal gateway %s request: %w", method, err)
	}

	endpoint, err := gatewayLLMConnectEndpoint(gatewayBaseURL, method)
	if err != nil {
		return "", 0, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("build gateway %s request: %w", method, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	request.Header.Set("Authorization", "Bearer "+apiToken)

	response, err := gatewayHTTPClient(gatewayBaseURL).Do(request)
	if err != nil {
		return "", 0, fmt.Errorf("post gateway %s: %w", method, err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return "", 0, fmt.Errorf("read gateway %s response: %w", method, err)
	}
	return strings.TrimSpace(string(responseBody)), response.StatusCode, nil
}

func gatewayLLMConnectEndpoint(gatewayBaseURL string, method string) (string, error) {
	return url.JoinPath(gatewayBaseURL, llmGatewayServicePath, method)
}

func gatewayHTTPClient(gatewayBaseURL string) *http.Client {
	transport := http.DefaultTransport
	parsed, err := url.Parse(gatewayBaseURL)
	if err == nil && parsed.Scheme == "https" {
		baseTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			panic("unexpected default transport type")
		}
		cloned := baseTransport.Clone()
		cloned.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		transport = cloned
	}
	return &http.Client{Timeout: setupTimeout, Transport: transport}
}

func parseGatewayBaseURL(value string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(value), "/")
	if baseURL == "" {
		return "", fmt.Errorf("AGYN_BASE_URL empty")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse AGYN_BASE_URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("AGYN_BASE_URL must start with http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("AGYN_BASE_URL missing host")
	}
	return baseURL, nil
}

type gatewayCreateLLMProviderResponse struct {
	Provider gatewayLLMEntity `json:"provider"`
}

type gatewayCreateModelResponse struct {
	Model gatewayLLMEntity `json:"model"`
}

type gatewayLLMEntity struct {
	Meta gatewayEntityMeta `json:"meta"`
}

type gatewayEntityMeta struct {
	ID string `json:"id"`
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
