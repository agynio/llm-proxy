//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

func TestConnectivity(t *testing.T) {
	client := newAuthenticatedClient(testAPIToken)
	req, err := http.NewRequest(http.MethodGet, responsesURL(), nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	resp := doRequest(t, client, req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, resp.StatusCode)
	}
}
