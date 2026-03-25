//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

func TestValidationMissingModel(t *testing.T) {
	client := newAuthenticatedClient(testAPIToken)
	resp := doPost(t, client, responsesURL(), []byte(`{"input":"hi"}`))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}

func TestValidationInvalidModel(t *testing.T) {
	client := newAuthenticatedClient(testAPIToken)
	resp := doPost(t, client, responsesURL(), []byte(`{"model":"not-a-uuid","input":"hi"}`))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}

func TestValidationInvalidJSON(t *testing.T) {
	client := newAuthenticatedClient(testAPIToken)
	resp := doPost(t, client, responsesURL(), []byte("{"))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}

func TestValidationInvalidStream(t *testing.T) {
	client := newAuthenticatedClient(testAPIToken)
	body := []byte(`{"model":"` + testModelID + `","input":"hi","stream":"nope"}`)
	resp := doPost(t, client, responsesURL(), body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}
