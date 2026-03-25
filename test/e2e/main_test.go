//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	defaultProxyURL = "http://llm-proxy:8080"
	proxyTimeout    = 45 * time.Second
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := setupFixtures(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "e2e setup failed: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func proxyURL() string {
	return strings.TrimRight(envOrDefault("LLM_PROXY_URL", defaultProxyURL), "/")
}

func responsesURL() string {
	return proxyURL() + "/v1/responses"
}

func newClient() *http.Client {
	return &http.Client{Timeout: proxyTimeout}
}

func newAuthenticatedClient(token string) *http.Client {
	return &http.Client{
		Timeout: proxyTimeout,
		Transport: bearerTransport{
			token: token,
			base:  http.DefaultTransport,
		},
	}
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = clone.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

func doPost(t *testing.T, client *http.Client, url string, body []byte) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	return doRequest(t, client, req)
}

func doRequest(t *testing.T, client *http.Client, req *http.Request) *http.Response {
	t.Helper()

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	return resp
}

func requestBody(t *testing.T, modelID string, input string, stream bool) []byte {
	t.Helper()

	payload := map[string]any{
		"model": modelID,
		"input": input,
	}
	if stream {
		payload["stream"] = true
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	return body
}

func readSSEEvents(t *testing.T, body io.Reader) []string {
	t.Helper()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var events []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		events = append(events, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read stream: %v", err)
	}

	return events
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
