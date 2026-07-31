package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentsv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/agents/v1"
	authorizationv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/authorization/v1"
	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
	"github.com/agynio/llm-proxy/internal/identity"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	testSandboxID      = "sandbox-1"
	testSandboxOwnerID = "owner-1"
	testSandboxOrgID   = "org-1"
)

type fakeSandboxResolver struct {
	sandbox *agentsv1.Sandbox
	err     error
	calls   int
	lastID  string
}

func (f *fakeSandboxResolver) GetSandbox(_ context.Context, req *agentsv1.GetSandboxRequest, _ ...grpc.CallOption) (*agentsv1.GetSandboxResponse, error) {
	f.calls++
	f.lastID = req.GetId()
	if f.err != nil {
		return nil, f.err
	}
	if f.sandbox == nil {
		return nil, status.Error(codes.NotFound, "sandbox not found")
	}
	return &agentsv1.GetSandboxResponse{Sandbox: f.sandbox}, nil
}

func runningSandboxResolver() *fakeSandboxResolver {
	return &fakeSandboxResolver{sandbox: &agentsv1.Sandbox{
		Meta:           &agentsv1.EntityMeta{Id: testSandboxID},
		OrganizationId: testSandboxOrgID,
		OwnerId:        testSandboxOwnerID,
		Status:         agentsv1.SandboxStatus_SANDBOX_STATUS_RUNNING,
	}}
}

func sandboxIdentity() identity.ResolvedIdentity {
	return identity.ResolvedIdentity{
		IdentityID:   testSandboxID,
		IdentityType: identity.IdentityTypeSandbox,
		WorkloadID:   "workload-1",
		ZitiID:       "ziti-sandbox-1",
	}
}

func sandboxProviderResponse(endpoint string) *llmv1.ResolveModelResponse {
	return &llmv1.ResolveModelResponse{
		Endpoint:       endpoint,
		Token:          "provider-token",
		RemoteName:     "remote-model",
		OrganizationId: testSandboxOrgID,
		Protocol:       llmv1.Protocol_PROTOCOL_RESPONSES,
		AuthMethod:     llmv1.AuthMethod_AUTH_METHOD_BEARER,
	}
}

func newProviderStub(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"usage":{"input_tokens":10,"output_tokens":5}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func sandboxRequest(t *testing.T, modelID string) *http.Request {
	t.Helper()
	body := `{"model":"` + modelID + `","stream":false}`
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/responses", strings.NewReader(body))
	return req.WithContext(identity.WithIdentity(req.Context(), sandboxIdentity()))
}

// A sandbox holds no model grant: can_use follows organization membership and a
// sandbox is deliberately not a member. The check has to resolve through its
// record to the owner who started it, or model calls could never work from
// inside a sandbox.
func TestHandlerAuthorizesSandboxThroughItsOwner(t *testing.T) {
	modelID := uuid.New()
	provider := newProviderStub(t)
	llmClient := &fakeLLMClient{resp: sandboxProviderResponse(provider.URL)}
	authzClient := &fakeAuthzClient{resp: &authorizationv1.CheckResponse{Allowed: true}}
	sandboxes := runningSandboxResolver()

	handler := NewHandler(llmClient, authzClient, &fakeMeteringClient{}, sandboxes, provider.Client())
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, sandboxRequest(t, modelID.String()))

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, resp.Code, resp.Body.String())
	}
	if sandboxes.calls != 1 || sandboxes.lastID != testSandboxID {
		t.Fatalf("expected one lookup of %s, got %d for %s", testSandboxID, sandboxes.calls, sandboxes.lastID)
	}
	tuple := authzClient.lastReq.GetTupleKey()
	if tuple.GetUser() != "identity:"+testSandboxOwnerID {
		t.Fatalf("expected the owner as principal, got %q", tuple.GetUser())
	}
	if tuple.GetRelation() != "can_use" {
		t.Fatalf("unexpected authz relation %q", tuple.GetRelation())
	}
	if tuple.GetObject() != "model:"+modelID.String() {
		t.Fatalf("unexpected authz object %q", tuple.GetObject())
	}
}

// The owner may be a member of several organizations while the model decides
// which one the call is billed to. A sandbox spends only against the
// organization it runs in.
func TestHandlerRefusesSandboxCallingAnotherOrganizationsModel(t *testing.T) {
	modelID := uuid.New()
	provider := newProviderStub(t)
	response := sandboxProviderResponse(provider.URL)
	response.OrganizationId = "org-2"
	llmClient := &fakeLLMClient{resp: response}
	authzClient := &fakeAuthzClient{resp: &authorizationv1.CheckResponse{Allowed: true}}

	handler := NewHandler(llmClient, authzClient, &fakeMeteringClient{}, runningSandboxResolver(), provider.Client())
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, sandboxRequest(t, modelID.String()))

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, resp.Code)
	}
	if authzClient.lastReq != nil {
		t.Fatalf("expected no authorization check for a cross-organization call")
	}
}

func TestHandlerRefusesTerminatedSandbox(t *testing.T) {
	modelID := uuid.New()
	provider := newProviderStub(t)
	sandboxes := runningSandboxResolver()
	sandboxes.sandbox.Status = agentsv1.SandboxStatus_SANDBOX_STATUS_TERMINATED

	handler := NewHandler(&fakeLLMClient{resp: sandboxProviderResponse(provider.URL)}, &fakeAuthzClient{resp: &authorizationv1.CheckResponse{Allowed: true}}, &fakeMeteringClient{}, sandboxes, provider.Client())
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, sandboxRequest(t, modelID.String()))

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, resp.Code)
	}
}

func TestHandlerRefusesSandboxWithoutRecord(t *testing.T) {
	modelID := uuid.New()
	provider := newProviderStub(t)
	sandboxes := &fakeSandboxResolver{err: status.Error(codes.NotFound, "sandbox not found")}

	handler := NewHandler(&fakeLLMClient{resp: sandboxProviderResponse(provider.URL)}, &fakeAuthzClient{resp: &authorizationv1.CheckResponse{Allowed: true}}, &fakeMeteringClient{}, sandboxes, provider.Client())
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, sandboxRequest(t, modelID.String()))

	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, resp.Code)
	}
}

func TestHandlerDoesNotResolveSandboxForOtherIdentityTypes(t *testing.T) {
	modelID := uuid.New()
	provider := newProviderStub(t)
	sandboxes := runningSandboxResolver()

	handler := NewHandler(&fakeLLMClient{resp: sandboxProviderResponse(provider.URL)}, &fakeAuthzClient{resp: &authorizationv1.CheckResponse{Allowed: true}}, &fakeMeteringClient{}, sandboxes, provider.Client())
	body := `{"model":"` + modelID.String() + `","stream":false}`
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/responses", strings.NewReader(body))
	req = req.WithContext(identity.WithIdentity(req.Context(), identity.ResolvedIdentity{
		IdentityID:   "agent-1",
		IdentityType: identity.IdentityTypeAgent,
		WorkloadID:   "workload-1",
	}))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.Code)
	}
	if sandboxes.calls != 0 {
		t.Fatalf("expected no sandbox lookup, got %d", sandboxes.calls)
	}
}

// Usage is the organization's to pay for, but the organization alone does not
// say who ran it up.
func TestSandboxUsageIsMeteredToTheOrgAndLabelledWithSandboxAndOwner(t *testing.T) {
	records := buildUsageRecords(meteringMetadata{
		callID:    "call-1",
		orgID:     testSandboxOrgID,
		modelID:   "model-1",
		modelName: "remote-model",
		identity:  sandboxIdentity(),
		sandbox: sandboxPrincipal{
			sandboxID:      testSandboxID,
			ownerID:        testSandboxOwnerID,
			organizationID: testSandboxOrgID,
		},
	}, &usageCounts{inputTokens: 10, outputTokens: 5}, meteringStatusSuccess)

	if len(records) == 0 {
		t.Fatalf("expected usage records")
	}
	for _, record := range records {
		if record.GetOrgId() != testSandboxOrgID {
			t.Fatalf("expected usage metered to %s, got %s", testSandboxOrgID, record.GetOrgId())
		}
		labels := record.GetLabels()
		if labels[meteringLabelSandboxID] != testSandboxID {
			t.Fatalf("expected sandbox label, got %v", labels)
		}
		if labels[meteringLabelSandboxOwnerID] != testSandboxOwnerID {
			t.Fatalf("expected sandbox owner label, got %v", labels)
		}
		if labels["identity_type"] != string(identity.IdentityTypeSandbox) {
			t.Fatalf("expected sandbox identity type, got %v", labels)
		}
	}
}

func TestAgentUsageCarriesNoSandboxLabels(t *testing.T) {
	records := buildUsageRecords(meteringMetadata{
		callID:    "call-1",
		orgID:     testSandboxOrgID,
		modelID:   "model-1",
		modelName: "remote-model",
		identity: identity.ResolvedIdentity{
			IdentityID:   "agent-1",
			IdentityType: identity.IdentityTypeAgent,
		},
	}, &usageCounts{inputTokens: 10, outputTokens: 5}, meteringStatusSuccess)

	for _, record := range records {
		labels := record.GetLabels()
		if _, ok := labels[meteringLabelSandboxID]; ok {
			t.Fatalf("unexpected sandbox label on agent usage: %v", labels)
		}
		if _, ok := labels[meteringLabelSandboxOwnerID]; ok {
			t.Fatalf("unexpected sandbox owner label on agent usage: %v", labels)
		}
	}
}

func TestResolveSandboxPrincipalRequiresOwnerAndOrganization(t *testing.T) {
	sandboxes := runningSandboxResolver()
	sandboxes.sandbox.OwnerId = ""
	handler := &Handler{sandboxResolver: sandboxes}

	_, err := handler.resolveSandboxPrincipal(context.Background(), sandboxIdentity())
	if !errors.Is(err, ErrSandboxUnusable) {
		t.Fatalf("expected an unusable sandbox, got %v", err)
	}
}
