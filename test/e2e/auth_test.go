//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

func TestAuthMissingToken(t *testing.T) {
	client := newClient()
	resp := doPost(t, client, responsesURL(), requestBody(t, testModelID, "hi", false))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
}

func TestAuthInvalidToken(t *testing.T) {
	client := newAuthenticatedClient("agyn_invalid")
	resp := doPost(t, client, responsesURL(), requestBody(t, testModelID, "hi", false))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
}

func TestAuthNonAgynToken(t *testing.T) {
	client := newAuthenticatedClient("not-agyn-token")
	resp := doPost(t, client, responsesURL(), requestBody(t, testModelID, "hi", false))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, resp.StatusCode)
	}
}

func TestAuthValidToken(t *testing.T) {
	client := newAuthenticatedClient(testAPIToken)
	resp := doPost(t, client, responsesURL(), requestBody(t, testModelID, "hi", false))
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("expected non-401 status, got %d", resp.StatusCode)
	}
}
