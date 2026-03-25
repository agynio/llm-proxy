//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestProxyUnauthorizedModel(t *testing.T) {
	client := newAuthenticatedClient(testAPIToken)
	resp := doPost(t, client, responsesURL(), requestBody(t, testUnauthorizedModelID, "hi", false))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, resp.StatusCode)
	}
}

func TestProxyNonStreamResponse(t *testing.T) {
	client := newAuthenticatedClient(testAPIToken)
	resp := doPost(t, client, responsesURL(), requestBody(t, testModelID, "hi", false))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if !strings.Contains(string(body), "Hi! How are you?") {
		t.Fatalf("unexpected response body: %s", string(body))
	}
}

func TestProxyStreamResponse(t *testing.T) {
	client := newAuthenticatedClient(testAPIToken)
	resp := doPost(t, client, responsesURL(), requestBody(t, testModelID, "hi", true))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("expected text/event-stream content type, got %q", resp.Header.Get("Content-Type"))
	}
	events := readSSEEvents(t, resp.Body)
	if len(events) == 0 {
		t.Fatalf("expected streamed events")
	}
}
