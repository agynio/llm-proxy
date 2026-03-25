//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	identityv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/identity/v1"
	"github.com/agynio/llm-proxy/internal/ziticonn"
	"github.com/google/uuid"
)

func TestZitiAuthResolveAndProxy(t *testing.T) {
	setupTest(t)

	modelID := uuid.New()
	registerModel(modelID.String(), "remote-model", "org-1")
	fakeZitiServer.RegisterIdentity("ziti-1", "user-1", identityv1.IdentityType_IDENTITY_TYPE_USER)
	allowIdentity("user-1", "org-1")

	server := newZitiServer("ziti-1")
	defer server.Close()

	body := `{"model":"` + modelID.String() + `"}`
	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
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
}

func TestZitiAuthUnknownIdentity(t *testing.T) {
	setupTest(t)

	modelID := uuid.New()
	body := `{"model":"` + modelID.String() + `"}`

	server := newZitiServer("ziti-missing")
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", strings.NewReader(body))
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

func newZitiServer(identity string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := ziticonn.WithSourceIdentity(r.Context(), identity)
		proxyHandler.ServeHTTP(w, r.WithContext(ctx))
	}))
}
