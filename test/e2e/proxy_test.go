//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestProxyForwardNonStream(t *testing.T) {
	setupTest(t)

	modelID := uuid.New()
	registerModel(modelID.String(), "remote-model", "org-1")

	token := "agyn_token"
	fakeUsersServer.RegisterToken(token, "user-1")
	allowIdentity("user-1", "org-1")

	body := `{"model":"` + modelID.String() + `","stream":false}`
	req, err := http.NewRequest(http.MethodPost, proxyBaseURL+"/v1/responses", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if strings.TrimSpace(string(respBody)) != defaultProviderResponse {
		t.Fatalf("unexpected response body: %s", respBody)
	}

	lastReq, ok := fakeProvider.LastRequest()
	if !ok {
		t.Fatalf("expected provider to be called")
	}
	if lastReq.Path != "/v1/responses" {
		t.Fatalf("expected provider path /v1/responses, got %s", lastReq.Path)
	}
	if got := lastReq.Header.Get("Authorization"); got != "Bearer "+providerToken {
		t.Fatalf("unexpected provider auth header: %q", got)
	}
	if got := lastReq.Header.Get("Accept"); got != "" {
		t.Fatalf("unexpected provider accept header: %q", got)
	}

	var payload map[string]any
	if err := json.Unmarshal(lastReq.Body, &payload); err != nil {
		t.Fatalf("unmarshal provider body: %v", err)
	}
	if payload["model"] != "remote-model" {
		t.Fatalf("expected model remote-model, got %v", payload["model"])
	}
	if payload["stream"] != false {
		t.Fatalf("expected stream false, got %v", payload["stream"])
	}
}

func TestProxyForwardStream(t *testing.T) {
	setupTest(t)

	modelID := uuid.New()
	registerModel(modelID.String(), "remote-model", "org-1")
	fakeProvider.SetStreamHandler()

	token := "agyn_stream"
	fakeUsersServer.RegisterToken(token, "user-1")
	allowIdentity("user-1", "org-1")

	body := `{"model":"` + modelID.String() + `","stream":true}`
	req, err := http.NewRequest(http.MethodPost, proxyBaseURL+"/v1/responses", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if resp.Header.Get("Content-Length") != "" {
		t.Fatalf("expected content-length to be stripped")
	}
	if strings.TrimSpace(string(respBody)) != strings.TrimSpace(defaultStreamResponse) {
		t.Fatalf("unexpected response body: %s", respBody)
	}

	lastReq, ok := fakeProvider.LastRequest()
	if !ok {
		t.Fatalf("expected provider to be called")
	}
	if got := lastReq.Header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("unexpected provider accept header: %q", got)
	}

	var payload map[string]any
	if err := json.Unmarshal(lastReq.Body, &payload); err != nil {
		t.Fatalf("unmarshal provider body: %v", err)
	}
	if payload["model"] != "remote-model" {
		t.Fatalf("expected model remote-model, got %v", payload["model"])
	}
	if payload["stream"] != true {
		t.Fatalf("expected stream true, got %v", payload["stream"])
	}
}

func TestProxyUnauthorizedNoToken(t *testing.T) {
	setupTest(t)

	modelID := uuid.New()
	body := `{"model":"` + modelID.String() + `"}`
	req, err := http.NewRequest(http.MethodPost, proxyBaseURL+"/v1/responses", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
	if len(fakeProvider.Requests()) != 0 {
		t.Fatalf("expected provider not to be called")
	}
}

func TestProxyUnauthorizedBadToken(t *testing.T) {
	setupTest(t)

	modelID := uuid.New()
	body := `{"model":"` + modelID.String() + `"}`
	req, err := http.NewRequest(http.MethodPost, proxyBaseURL+"/v1/responses", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer invalid")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
	if len(fakeProvider.Requests()) != 0 {
		t.Fatalf("expected provider not to be called")
	}
}

func TestProxyUnauthorizedInvalidAPIToken(t *testing.T) {
	setupTest(t)

	modelID := uuid.New()
	body := `{"model":"` + modelID.String() + `"}`
	req, err := http.NewRequest(http.MethodPost, proxyBaseURL+"/v1/responses", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer agyn_missing")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
	if len(fakeProvider.Requests()) != 0 {
		t.Fatalf("expected provider not to be called")
	}
}

func TestProxyForbidden(t *testing.T) {
	setupTest(t)

	modelID := uuid.New()
	registerModel(modelID.String(), "remote-model", "org-1")

	token := "agyn_forbidden"
	fakeUsersServer.RegisterToken(token, "user-1")

	body := `{"model":"` + modelID.String() + `"}`
	req, err := http.NewRequest(http.MethodPost, proxyBaseURL+"/v1/responses", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, resp.StatusCode)
	}
	if len(fakeProvider.Requests()) != 0 {
		t.Fatalf("expected provider not to be called")
	}
}

func TestProxyModelNotFound(t *testing.T) {
	setupTest(t)

	modelID := uuid.New()
	missingModel := modelID.String()

	token := "agyn_missing_model"
	fakeUsersServer.RegisterToken(token, "user-1")
	allowIdentity("user-1", "org-1")
	fakeAuthzServer.SetDefaultAllow(true)

	body := `{"model":"` + missingModel + `"}`
	req, err := http.NewRequest(http.MethodPost, proxyBaseURL+"/v1/responses", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, resp.StatusCode)
	}
	if len(fakeProvider.Requests()) != 0 {
		t.Fatalf("expected provider not to be called")
	}
}

func TestProxyInvalidBody(t *testing.T) {
	setupTest(t)

	token := "agyn_bad_body"
	fakeUsersServer.RegisterToken(token, "user-1")
	allowIdentity("user-1", "org-1")

	req, err := http.NewRequest(http.MethodPost, proxyBaseURL+"/v1/responses", strings.NewReader("{"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}

func TestProxyMissingModel(t *testing.T) {
	setupTest(t)

	token := "agyn_missing_model"
	fakeUsersServer.RegisterToken(token, "user-1")
	allowIdentity("user-1", "org-1")

	body := `{"stream":false}`
	req, err := http.NewRequest(http.MethodPost, proxyBaseURL+"/v1/responses", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}

func TestProxyModelNotUUID(t *testing.T) {
	setupTest(t)

	token := "agyn_bad_uuid"
	fakeUsersServer.RegisterToken(token, "user-1")
	allowIdentity("user-1", "org-1")

	body := `{"model":"not-a-uuid"}`
	req, err := http.NewRequest(http.MethodPost, proxyBaseURL+"/v1/responses", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}

func TestProxyWrongMethod(t *testing.T) {
	setupTest(t)

	token := "agyn_wrong_method"
	fakeUsersServer.RegisterToken(token, "user-1")

	req, err := http.NewRequest(http.MethodGet, proxyBaseURL+"/v1/responses", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); allow != http.MethodPost {
		t.Fatalf("expected Allow header %q, got %q", http.MethodPost, allow)
	}
}

func TestProxyWrongPath(t *testing.T) {
	setupTest(t)

	token := "agyn_wrong_path"
	fakeUsersServer.RegisterToken(token, "user-1")

	req, err := http.NewRequest(http.MethodPost, proxyBaseURL+"/v1/unknown", strings.NewReader(`{"model":"`+uuid.NewString()+`"}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, resp.StatusCode)
	}
}

func TestProxyProviderError(t *testing.T) {
	setupTest(t)

	modelID := uuid.New()
	registerModel(modelID.String(), "remote-model", "org-1")
	fakeProvider.SetHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"provider"}`))
	})

	token := "agyn_provider_error"
	fakeUsersServer.RegisterToken(token, "user-1")
	allowIdentity("user-1", "org-1")

	body := `{"model":"` + modelID.String() + `"}`
	req, err := http.NewRequest(http.MethodPost, proxyBaseURL+"/v1/responses", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, resp.StatusCode)
	}
	if strings.TrimSpace(string(respBody)) != `{"error":"provider"}` {
		t.Fatalf("unexpected response body: %s", respBody)
	}
}
