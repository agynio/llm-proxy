package proxy

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Forwarding the caller's Accept-Encoding makes net/http treat compression as
// caller-managed and stop decoding, so a gzipped provider response reaches the
// SSE reader as raw deflate bytes. That is what produced
// "invalid character '\x1f'" and a stream the client reported as disconnected.
func TestProviderRequestDropsAcceptEncoding(t *testing.T) {
	if !shouldStripProviderRequestHeader("Accept-Encoding", nil) {
		t.Fatal("Accept-Encoding must not be forwarded to the provider")
	}
	dst := http.Header{}
	src := http.Header{"Accept-Encoding": {"gzip"}, "Accept": {"text/event-stream"}}
	copyProviderRequestHeaders(dst, src)
	if got := dst.Get("Accept-Encoding"); got != "" {
		t.Errorf("Accept-Encoding forwarded as %q", got)
	}
	if got := dst.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", got)
	}
}

// End to end through a real Transport: a gzipped provider response must arrive
// decoded, which only happens when we leave Accept-Encoding to net/http.
func TestProviderResponseArrivesDecoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/event-stream")
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte("data: {\"ok\":true}\n\n"))
		_ = gz.Close()
	}))
	defer srv.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	copyProviderRequestHeaders(req.Header, http.Header{"Accept-Encoding": {"gzip"}})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(body) > 0 && body[0] == 0x1f {
		t.Fatal("body is still gzipped; Accept-Encoding leaked to the provider")
	}
	if string(body) != "data: {\"ok\":true}\n\n" {
		t.Errorf("body = %q", string(body))
	}
}
