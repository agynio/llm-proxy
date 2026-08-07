package native

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"

	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
)

// connResponseWriter writes an HTTP/1.1 response straight onto the terminated
// TLS connection. Streaming is the common case in native mode -- the agent CLI
// sets stream:true on its first request -- so writes flush as they arrive
// rather than buffering the body.
type connResponseWriter struct {
	conn        net.Conn
	header      http.Header
	statusCode  int
	wroteHeader bool
	sentHeader  bool
	closeAfter  bool
}

func newConnResponseWriter(conn net.Conn) *connResponseWriter {
	return &connResponseWriter{conn: conn, header: http.Header{}}
}

func (w *connResponseWriter) Header() http.Header { return w.header }

func (w *connResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.statusCode = statusCode
	w.wroteHeader = true
}

func (w *connResponseWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if err := w.writeHeader(); err != nil {
		return 0, err
	}
	return w.conn.Write(body)
}

// Flush is a no-op beyond what Write already does -- bytes go straight to the
// connection -- but the SSE relay type-asserts for http.Flusher.
func (w *connResponseWriter) Flush() {}

func (w *connResponseWriter) finish() error {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.writeHeader()
}

func (w *connResponseWriter) writeHeader() error {
	if w.sentHeader {
		return nil
	}
	w.sentHeader = true

	statusText := http.StatusText(w.statusCode)
	if statusText == "" {
		statusText = "status code " + strconv.Itoa(w.statusCode)
	}
	// Close-delimited: the upstream body length is not known ahead of a
	// streamed response, and the caller reads to EOF either way.
	w.closeAfter = true
	head := fmt.Sprintf("HTTP/1.1 %d %s\r\n", w.statusCode, statusText)
	for name, values := range w.header {
		for _, value := range values {
			head += name + ": " + value + "\r\n"
		}
	}
	head += "Connection: close\r\n\r\n"
	_, err := w.conn.Write([]byte(head))
	return err
}

// writeVendorError refuses a request in the vendor's own error format, so the
// agent CLI renders it rather than showing a transport failure. The message
// identifies the platform as the source.
func writeVendorError(conn net.Conn, vendor llmv1.Vendor, status int, message string) {
	w := newConnResponseWriter(conn)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(vendorErrorBody(vendor, message)); err != nil {
		return
	}
	_ = w.finish()
}

func vendorErrorBody(vendor llmv1.Vendor, message string) []byte {
	prefixed := "agyn: " + message
	var payload any
	switch vendor {
	case llmv1.Vendor_VENDOR_CLAUDE:
		payload = map[string]any{
			"type":  "error",
			"error": map[string]any{"type": "permission_error", "message": prefixed},
		}
	default:
		payload = map[string]any{
			"error": map[string]any{"type": "permission_error", "message": prefixed},
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return []byte(`{"error":{"type":"permission_error","message":"agyn: request refused"}}`)
	}
	return body
}
