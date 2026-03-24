//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	sdk "github.com/openziti/sdk-golang"
	"github.com/openziti/sdk-golang/ziti"
	"github.com/stretchr/testify/require"
)

const requestTimeout = 30 * time.Second

var proxyURL = envOrDefault("LLM_PROXY_URL", "http://llm-proxy:8080")

type apiTokenCredentials struct {
	token      string
	identityID string
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func newClient() *http.Client {
	return &http.Client{}
}

func newAuthenticatedClient(token string) *http.Client {
	client := newClient()
	client.Transport = bearerTransport{token: token, base: client.Transport}
	return client
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return base.RoundTrip(clone)
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func apiTokenConfig() apiTokenCredentials {
	token := strings.TrimSpace(os.Getenv("E2E_API_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("API_TOKEN"))
	}

	identityID := strings.TrimSpace(os.Getenv("E2E_API_TOKEN_IDENTITY_ID"))
	if identityID == "" {
		identityID = strings.TrimSpace(os.Getenv("API_TOKEN_IDENTITY_ID"))
	}

	return apiTokenCredentials{token: token, identityID: identityID}
}

func modelID() string {
	value := strings.TrimSpace(os.Getenv("E2E_MODEL_ID"))
	if value == "" {
		value = strings.TrimSpace(os.Getenv("MODEL_ID"))
	}
	return value
}

func unauthorizedModelID() string {
	return strings.TrimSpace(os.Getenv("E2E_UNAUTHORIZED_MODEL_ID"))
}

func requireAPIToken(t *testing.T) string {
	t.Helper()
	token := apiTokenConfig().token
	if token == "" {
		t.Skip("api token not configured")
	}
	return token
}

func requireModelID(t *testing.T) string {
	t.Helper()
	value := modelID()
	if value == "" {
		t.Skip("model id not configured")
	}
	return value
}

func requireUnauthorizedModelID(t *testing.T) string {
	t.Helper()
	value := unauthorizedModelID()
	if value == "" {
		t.Skip("unauthorized model id not configured")
	}
	return value
}

func zitiIdentityFile() string {
	if value := strings.TrimSpace(os.Getenv("ZITI_E2E_IDENTITY_FILE")); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("ZITI_IDENTITY_FILE"))
}

func zitiProxyURL() string {
	return envOrDefault("ZITI_LLM_PROXY_URL", "http://llm-proxy")
}

func newZitiClient(t *testing.T) *http.Client {
	t.Helper()
	identityFile := zitiIdentityFile()
	if identityFile == "" {
		t.Skip("ziti identity file not configured")
	}
	if _, err := os.Stat(identityFile); err != nil {
		t.Skipf("ziti identity file unavailable: %v", err)
	}

	zitiContext, err := ziti.NewContextFromFile(identityFile)
	if err != nil {
		t.Skipf("ziti context unavailable: %v", err)
	}
	t.Cleanup(func() {
		zitiContext.Close()
	})

	client := sdk.NewHttpClient(zitiContext, nil)
	return client
}

func doPost(t *testing.T, client *http.Client, baseURL string, body []byte) *http.Response {
	t.Helper()
	return doRequest(t, client, http.MethodPost, responsesURL(baseURL), body, "application/json")
}

func doRequest(t *testing.T, client *http.Client, method, url string, body []byte, contentType string) *http.Response {
	t.Helper()
	ensureResolvableHost(t, url)
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	require.NoError(t, err)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

func ensureResolvableHost(t *testing.T, rawURL string) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Skipf("invalid proxy url: %v", err)
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		t.Skip("proxy url missing hostname")
	}

	if _, err := net.LookupHost(hostname); err != nil {
		t.Skipf("proxy host unavailable: %v", err)
	}
}

func responsesURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/v1/responses"
}

func requestBody(t *testing.T, model string, stream bool) []byte {
	t.Helper()
	payload := map[string]any{
		"model": model,
		"input": "Say hello from e2e",
	}
	if stream {
		payload["stream"] = true
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	return body
}

func readSSEEvents(t *testing.T, body io.Reader) []string {
	t.Helper()
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	events := make([]string, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "event:") {
			event := strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			if event != "" {
				events = append(events, event)
			}
		}
	}
	require.NoError(t, scanner.Err())
	return events
}
