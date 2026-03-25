//go:build e2e

package e2e

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
)

const (
	defaultProviderResponse = `{"ok":true}`
	defaultStreamResponse   = "data: hello\n\n"
)

type CapturedRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

type FakeProviderServer struct {
	mu       sync.Mutex
	server   *httptest.Server
	handler  http.HandlerFunc
	requests []CapturedRequest
}

func NewFakeProviderServer() *FakeProviderServer {
	server := &FakeProviderServer{}
	server.handler = server.defaultHandler
	server.server = httptest.NewServer(http.HandlerFunc(server.serve))
	return server
}

func (f *FakeProviderServer) URL() string {
	return f.server.URL
}

func (f *FakeProviderServer) Close() {
	f.server.Close()
}

func (f *FakeProviderServer) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = nil
	f.handler = f.defaultHandler
}

func (f *FakeProviderServer) SetHandler(handler http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if handler == nil {
		f.handler = f.defaultHandler
		return
	}
	f.handler = handler
}

func (f *FakeProviderServer) SetStreamHandler() {
	f.SetHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(defaultStreamResponse)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(defaultStreamResponse))
	})
}

func (f *FakeProviderServer) Requests() []CapturedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	requests := make([]CapturedRequest, len(f.requests))
	copy(requests, f.requests)
	return requests
}

func (f *FakeProviderServer) LastRequest() (CapturedRequest, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return CapturedRequest{}, false
	}
	return f.requests[len(f.requests)-1], true
}

func (f *FakeProviderServer) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))

	recorded := CapturedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Header: r.Header.Clone(),
		Body:   body,
	}

	f.mu.Lock()
	f.requests = append(f.requests, recorded)
	handler := f.handler
	f.mu.Unlock()

	handler(w, r)
}

func (f *FakeProviderServer) defaultHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(defaultProviderResponse))
}
