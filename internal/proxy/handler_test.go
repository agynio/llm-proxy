package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authorizationv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/authorization/v1"
	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
	"github.com/agynio/llm-proxy/internal/identity"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

type fakeLLMClient struct {
	resp    *llmv1.ResolveModelResponse
	err     error
	lastReq *llmv1.ResolveModelRequest
}

func (f *fakeLLMClient) ResolveModel(_ context.Context, req *llmv1.ResolveModelRequest, _ ...grpc.CallOption) (*llmv1.ResolveModelResponse, error) {
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

type fakeAuthzClient struct {
	resp    *authorizationv1.CheckResponse
	err     error
	lastReq *authorizationv1.CheckRequest
}

func (f *fakeAuthzClient) Check(_ context.Context, req *authorizationv1.CheckRequest, _ ...grpc.CallOption) (*authorizationv1.CheckResponse, error) {
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func TestHandlerRejectsMissingIdentity(t *testing.T) {
	handler := NewHandler(&fakeLLMClient{}, &fakeAuthzClient{}, http.DefaultClient)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"`+uuid.NewString()+`"}`))
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, resp.Code)
	}
}

func TestHandlerForwardNonStream(t *testing.T) {
	modelID := uuid.New()
	providerCalled := false
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalled = true
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer provider-token" {
			t.Fatalf("unexpected auth header %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected content type %q", r.Header.Get("Content-Type"))
		}
		if got := r.Header.Get("Accept"); got != "" {
			t.Fatalf("unexpected accept header %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read provider body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal provider body: %v", err)
		}
		if payload["model"] != "remote-model" {
			t.Fatalf("expected model remote-model, got %v", payload["model"])
		}
		if payload["stream"] != false {
			t.Fatalf("expected stream false, got %v", payload["stream"])
		}
		w.Header().Set("X-Provider", "ok")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer provider.Close()

	llmClient := &fakeLLMClient{resp: &llmv1.ResolveModelResponse{
		Endpoint:       provider.URL,
		Token:          "provider-token",
		RemoteName:     "remote-model",
		OrganizationId: "org-1",
	}}
	authzClient := &fakeAuthzClient{resp: &authorizationv1.CheckResponse{Allowed: true}}

	handler := NewHandler(llmClient, authzClient, provider.Client())

	body := `{"model":"` + modelID.String() + `","stream":false}`
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/responses", strings.NewReader(body))
	ctx := identity.WithIdentity(req.Context(), identity.ResolvedIdentity{IdentityID: "user-1", IdentityType: identity.IdentityTypeUser})
	req = req.WithContext(ctx)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if !providerCalled {
		t.Fatalf("expected provider to be called")
	}
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.Code)
	}
	if strings.TrimSpace(resp.Body.String()) != `{"ok":true}` {
		t.Fatalf("unexpected response body: %s", resp.Body.String())
	}
	if resp.Header().Get("X-Provider") != "ok" {
		t.Fatalf("expected provider header")
	}
	if llmClient.lastReq.GetModelId() != modelID.String() {
		t.Fatalf("expected model id %s, got %s", modelID.String(), llmClient.lastReq.GetModelId())
	}
	if authzClient.lastReq.GetTupleKey().GetUser() != "identity:user-1" {
		t.Fatalf("unexpected authz user %q", authzClient.lastReq.GetTupleKey().GetUser())
	}
	if authzClient.lastReq.GetTupleKey().GetObject() != "organization:org-1" {
		t.Fatalf("unexpected authz object %q", authzClient.lastReq.GetTupleKey().GetObject())
	}
}

func TestHandlerForwardStream(t *testing.T) {
	modelID := uuid.New()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("expected event-stream accept header")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read provider body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal provider body: %v", err)
		}
		if payload["stream"] != true {
			t.Fatalf("expected stream true, got %v", payload["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "123")
		w.Header().Set("X-Provider", "stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: hello\n\n"))
	}))
	defer provider.Close()

	llmClient := &fakeLLMClient{resp: &llmv1.ResolveModelResponse{
		Endpoint:       provider.URL,
		Token:          "provider-token",
		RemoteName:     "remote-model",
		OrganizationId: "org-1",
	}}
	authzClient := &fakeAuthzClient{resp: &authorizationv1.CheckResponse{Allowed: true}}
	handler := NewHandler(llmClient, authzClient, provider.Client())

	body := `{"model":"` + modelID.String() + `","stream":true}`
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/responses", strings.NewReader(body))
	ctx := identity.WithIdentity(req.Context(), identity.ResolvedIdentity{IdentityID: "user-1", IdentityType: identity.IdentityTypeUser})
	req = req.WithContext(ctx)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.Code)
	}
	if resp.Header().Get("Content-Length") != "" {
		t.Fatalf("expected content-length to be omitted")
	}
	if resp.Header().Get("X-Provider") != "stream" {
		t.Fatalf("expected provider header")
	}
	if strings.TrimSpace(resp.Body.String()) != "data: hello" {
		t.Fatalf("unexpected response body: %s", resp.Body.String())
	}
}

func TestHandlerForbidden(t *testing.T) {
	modelID := uuid.New()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer provider.Close()

	llmClient := &fakeLLMClient{resp: &llmv1.ResolveModelResponse{
		Endpoint:       provider.URL,
		Token:          "provider-token",
		RemoteName:     "remote-model",
		OrganizationId: "org-1",
	}}
	authzClient := &fakeAuthzClient{resp: &authorizationv1.CheckResponse{Allowed: false}}
	handler := NewHandler(llmClient, authzClient, provider.Client())

	body := `{"model":"` + modelID.String() + `"}`
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/responses", strings.NewReader(body))
	ctx := identity.WithIdentity(req.Context(), identity.ResolvedIdentity{IdentityID: "user-1", IdentityType: identity.IdentityTypeUser})
	req = req.WithContext(ctx)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, resp.Code)
	}
}

func TestHandlerInvalidBody(t *testing.T) {
	handler := NewHandler(&fakeLLMClient{}, &fakeAuthzClient{}, http.DefaultClient)

	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/responses", strings.NewReader("{"))
	ctx := identity.WithIdentity(req.Context(), identity.ResolvedIdentity{IdentityID: "user-1", IdentityType: identity.IdentityTypeUser})
	req = req.WithContext(ctx)
	resp := httptest.NewRecorder()

	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.Code)
	}
}
