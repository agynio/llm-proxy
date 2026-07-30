package proxy

import (
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
)

const sseBody = "event: response.completed\n" +
	"data: {\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":3}}}\n\n"

// The relay reads whatever resp.Body gives it. When Accept-Encoding leaks to the
// provider, net/http stops decoding and that is gzip — the SSE parser then sees
// compressed bytes, usage never parses, and the client gets a body it cannot
// read as a stream. This pins the decoded case as the contract.
func TestStreamToClientRelaysDecodedSSE(t *testing.T) {
	rec := httptest.NewRecorder()
	usage, err := streamToClient(context.Background(), rec, strings.NewReader(sseBody), llmv1.Protocol_PROTOCOL_RESPONSES)
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	if usage == nil {
		t.Fatal("usage not parsed from a decoded stream")
	}
	if !strings.Contains(rec.Body.String(), "response.completed") {
		t.Errorf("client body missing the event: %q", rec.Body.String())
	}
}

// Same relay, gzipped input: nothing usable reaches the client and usage is
// lost. This is the failure the Accept-Encoding strip prevents.
func TestStreamToClientCannotRelayGzip(t *testing.T) {
	var gzipped strings.Builder
	gz := gzip.NewWriter(&gzipped)
	_, _ = gz.Write([]byte(sseBody))
	_ = gz.Close()

	rec := httptest.NewRecorder()
	usage, _ := streamToClient(context.Background(), rec, strings.NewReader(gzipped.String()), llmv1.Protocol_PROTOCOL_RESPONSES)
	if usage != nil {
		t.Fatal("usage parsed from gzip; test no longer reproduces the failure")
	}
	if strings.Contains(rec.Body.String(), "response.completed") {
		t.Fatal("gzip relayed as readable SSE; test no longer reproduces the failure")
	}
}

// The end the fix acts on: with the strip in place a gzip-encoding provider is
// decoded by the Transport, so the relay gets exactly the decoded case above.
func TestProviderRequestYieldsDecodedStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/event-stream")
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte(sseBody))
		_ = gz.Close()
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	copyProviderRequestHeaders(req.Header, http.Header{"Accept-Encoding": {"gzip"}})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	rec := httptest.NewRecorder()
	usage, err := streamToClient(context.Background(), rec, resp.Body, llmv1.Protocol_PROTOCOL_RESPONSES)
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	if usage == nil {
		t.Fatal("usage lost: provider response reached the relay still gzipped")
	}
}
