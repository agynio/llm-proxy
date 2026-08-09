package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
	meteringv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/metering/v1"
	"github.com/agynio/llm-proxy/internal/identity"
	"github.com/agynio/llm-proxy/internal/native"
	"google.golang.org/grpc"
)

type capturingMeteringClient struct {
	records chan []*meteringv1.UsageRecord
}

func newCapturingMeteringClient() *capturingMeteringClient {
	return &capturingMeteringClient{records: make(chan []*meteringv1.UsageRecord, 8)}
}

func (c *capturingMeteringClient) Record(_ context.Context, req *meteringv1.RecordRequest, _ ...grpc.CallOption) (*meteringv1.RecordResponse, error) {
	c.records <- req.GetRecords()
	return &meteringv1.RecordResponse{}, nil
}

func labelsOf(records []*meteringv1.UsageRecord, kind string) map[string]string {
	for _, record := range records {
		if record.GetLabels()["kind"] == kind {
			return record.GetLabels()
		}
	}
	return nil
}

func nativeBinding(upstream string) native.Binding {
	return native.Binding{
		SubscriptionID:   "sub-1",
		Token:            "sk-ant-oat01-real",
		UpstreamEndpoint: upstream,
		Protocol:         llmv1.Protocol_PROTOCOL_ANTHROPIC_MESSAGES,
		OrganizationID:   "11111111-1111-1111-1111-111111111111",
		Vendor:           llmv1.Vendor_VENDOR_ANTHROPIC,
		Identity:         identity.ResolvedIdentity{IdentityID: "id-1", IdentityType: identity.IdentityTypeSandbox},
	}
}

func nativeRequest(t *testing.T, target, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer agyn-placeholder-not-a-credential")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "claude-code-20250219,oauth-2025-04-20")
	return req
}

// The CLI addresses /v1/messages?beta=true. Dropping the query changes the
// request the vendor receives.
func TestNativeForwarderPreservesPathAndQuery(t *testing.T) {
	var gotURI string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		w.Write([]byte(`{"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer upstream.Close()

	forwarder := NewNativeForwarder(upstream.Client(), newCapturingMeteringClient())
	forwarder.Forward(httptest.NewRecorder(),
		nativeRequest(t, "/v1/messages?beta=true", `{"model":"claude-sonnet-5"}`),
		nativeBinding(upstream.URL))

	if gotURI != "/v1/messages?beta=true" {
		t.Fatalf("expected the path and query preserved, got %q", gotURI)
	}
}

// The placeholder is replaced and everything else passes through: anthropic-beta
// carries oauth-2025-04-20, without which the subscription token does not
// authenticate.
func TestNativeForwarderSwapsCredentialAndForwardsTheRest(t *testing.T) {
	var got http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Write([]byte(`{"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer upstream.Close()

	forwarder := NewNativeForwarder(upstream.Client(), newCapturingMeteringClient())
	req := nativeRequest(t, "/v1/messages", `{"model":"claude-sonnet-5"}`)
	req.Header.Set("x-api-key", "should-not-survive")
	req.Header.Set("x-agyn-thread-id", "22222222-2222-2222-2222-222222222222")
	forwarder.Forward(httptest.NewRecorder(), req, nativeBinding(upstream.URL))

	if got.Get("Authorization") != "Bearer sk-ant-oat01-real" {
		t.Fatalf("expected the subscription token injected, got %q", got.Get("Authorization"))
	}
	if got.Get("X-Api-Key") != "" {
		t.Fatalf("expected x-api-key stripped, got %q", got.Get("X-Api-Key"))
	}
	if got.Get("anthropic-beta") != "claude-code-20250219,oauth-2025-04-20" {
		t.Fatalf("expected anthropic-beta forwarded verbatim, got %q", got.Get("anthropic-beta"))
	}
	if got.Get("anthropic-version") != "2023-06-01" {
		t.Fatalf("expected anthropic-version forwarded, got %q", got.Get("anthropic-version"))
	}
	// Platform-internal headers are consumed here, not part of any vendor API.
	if got.Get("x-agyn-thread-id") != "" {
		t.Fatalf("expected x-agyn-* stripped, got %q", got.Get("x-agyn-thread-id"))
	}
}

// The body is the vendor's; the model field in particular is never rewritten.
func TestNativeForwarderForwardsBodyByteForByte(t *testing.T) {
	body := `{"model":"claude-sonnet-5","max_tokens":7,"messages":[{"role":"user","content":"hi"}]}`
	var got []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = readAllBody(r)
		w.Write([]byte(`{"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer upstream.Close()

	forwarder := NewNativeForwarder(upstream.Client(), newCapturingMeteringClient())
	forwarder.Forward(httptest.NewRecorder(), nativeRequest(t, "/v1/messages", body), nativeBinding(upstream.URL))

	if string(got) != body {
		t.Fatalf("expected the body unchanged, got %s", got)
	}
}

func TestNativeForwarderRefusesModelOutsideAllowlist(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("expected no upstream call")
	}))
	defer upstream.Close()

	binding := nativeBinding(upstream.URL)
	binding.AllowedModels = []string{"claude-opus-4-6"}

	recorder := httptest.NewRecorder()
	forwarder := NewNativeForwarder(upstream.Client(), newCapturingMeteringClient())
	forwarder.Forward(recorder, nativeRequest(t, "/v1/messages", `{"model":"claude-sonnet-5"}`), binding)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}
	// Refused in the vendor's own error format so the CLI renders it, naming
	// the platform as the source.
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected a vendor-shaped error body, got %s", recorder.Body.String())
	}
	if !strings.HasPrefix(payload.Error.Message, "agyn: ") {
		t.Fatalf("expected the platform named as the source, got %q", payload.Error.Message)
	}
}

func TestNativeForwarderAllowsModelInAllowlist(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer upstream.Close()

	binding := nativeBinding(upstream.URL)
	binding.AllowedModels = []string{"claude-sonnet-5"}

	forwarder := NewNativeForwarder(upstream.Client(), newCapturingMeteringClient())
	forwarder.Forward(httptest.NewRecorder(), nativeRequest(t, "/v1/messages", `{"model":"claude-sonnet-5"}`), binding)

	if !called {
		t.Fatal("expected the request forwarded")
	}
}

// Native tokens must be distinguishable from API tokens at the aggregation
// layer, which is what resource=subscription is for.
func TestNativeForwarderMetersAgainstTheSubscription(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"usage":{"input_tokens":11,"output_tokens":3}}`))
	}))
	defer upstream.Close()

	metering := newCapturingMeteringClient()
	forwarder := NewNativeForwarder(upstream.Client(), metering)
	forwarder.Forward(httptest.NewRecorder(), nativeRequest(t, "/v1/messages", `{"model":"claude-sonnet-5"}`), nativeBinding(upstream.URL))

	records := <-metering.records
	labels := labelsOf(records, meteringKindInput)
	if labels == nil {
		t.Fatalf("expected an input token record, got %d records", len(records))
	}
	if labels["resource"] != meteringResourceSubscription {
		t.Fatalf("expected resource=subscription, got %q", labels["resource"])
	}
	if labels["resource_id"] != "sub-1" {
		t.Fatalf("expected the subscription id, got %q", labels["resource_id"])
	}
	if labels["vendor"] != "anthropic" {
		t.Fatalf("expected vendor=claude, got %q", labels["vendor"])
	}
	if labels["model_name"] != "claude-sonnet-5" {
		t.Fatalf("expected the vendor model name, got %q", labels["model_name"])
	}
}

// The platform path records a resource *type* too; it previously recorded the
// remote model name there, which left the two modes indistinguishable.
func TestPlatformModeMetersResourceAsAType(t *testing.T) {
	records := buildUsageRecords(meteringMetadata{
		callID:    "call-1",
		orgID:     "org-1",
		modelID:   "model-1",
		modelName: "claude-sonnet-4-6",
	}, &usageCounts{inputTokens: 2, outputTokens: 1}, meteringStatusSuccess)

	labels := labelsOf(records, meteringKindInput)
	if labels["resource"] != meteringResourceModel {
		t.Fatalf("expected resource=model, got %q", labels["resource"])
	}
	if _, ok := labels["vendor"]; ok {
		t.Fatal("expected no vendor label in platform mode")
	}
}

// Vendor errors pass through unchanged so the CLI's own handling works.
func TestNativeForwarderPassesVendorErrorsThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("retry-after", "42")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error"}}`))
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	forwarder := NewNativeForwarder(upstream.Client(), newCapturingMeteringClient())
	forwarder.Forward(recorder, nativeRequest(t, "/v1/messages", `{"model":"claude-sonnet-5"}`), nativeBinding(upstream.URL))

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the vendor status preserved, got %d", recorder.Code)
	}
	if recorder.Header().Get("retry-after") != "42" {
		t.Fatalf("expected the vendor headers preserved, got %v", recorder.Header())
	}
	if !strings.Contains(recorder.Body.String(), "rate_limit_error") {
		t.Fatalf("expected the vendor body preserved, got %s", recorder.Body.String())
	}
}

func TestModelAllowed(t *testing.T) {
	// Empty means no restriction, which is the default.
	if !modelAllowed("claude-sonnet-5", nil) {
		t.Fatal("expected an empty allowlist to permit everything")
	}
	// A body the platform cannot parse yields no model name; refusing it would
	// reject on a guess about a format the platform does not own.
	if !modelAllowed("", []string{"claude-opus-4-6"}) {
		t.Fatal("expected an unknown model name to pass")
	}
	if modelAllowed("claude-sonnet-5", []string{"claude-opus-4-6"}) {
		t.Fatal("expected a model outside the allowlist to be refused")
	}
	if !modelAllowed("claude-sonnet-5", []string{" Claude-Sonnet-5 "}) {
		t.Fatal("expected matching to ignore case and surrounding space")
	}
}

func readAllBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}

// The caller's whole path is appended to the upstream, so an upstream that
// carries a path of its own forwards it twice -- which reaches the vendor as a
// 404 and looks like the endpoint does not exist.
func TestNativeForwardPreservesTheCallersPathExactly(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-5.5","usage":{}}`))
	}))
	defer upstream.Close()

	binding := nativeBinding(upstream.URL)
	binding.Vendor = llmv1.Vendor_VENDOR_OPENAI
	binding.Protocol = llmv1.Protocol_PROTOCOL_RESPONSES

	forwarder := NewNativeForwarder(upstream.Client(), newCapturingMeteringClient())

	recorder := httptest.NewRecorder()
	forwarder.Forward(recorder, nativeRequest(t, "/backend-api/codex/responses", `{"model":"gpt-5.5"}`), binding)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if gotPath != "/backend-api/codex/responses" {
		t.Fatalf("upstream path = %q, want the caller's own", gotPath)
	}
}
