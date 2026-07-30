package proxy

import "testing"

// A streamed response.completed event carries usage inside a "response"
// envelope, and the non-streaming body for the same API uses the same shape.
// The non-streaming parser only read the top level, so identical usage was
// billed when streamed and dropped when not.
func TestParseUsageFromPayloadAcceptsResponseEnvelope(t *testing.T) {
	usage, err := parseUsageFromPayload([]byte(`{"response":{"usage":{"input_tokens":11,"output_tokens":4}}}`))
	if err != nil {
		t.Fatalf("enveloped usage not parsed: %v", err)
	}
	if usage.inputTokens != 11 || usage.outputTokens != 4 {
		t.Errorf("usage = %+v, want 11 in / 4 out", usage)
	}
}

func TestParseUsageFromPayloadAcceptsTopLevel(t *testing.T) {
	usage, err := parseUsageFromPayload([]byte(`{"usage":{"input_tokens":3,"output_tokens":9}}`))
	if err != nil {
		t.Fatalf("top-level usage not parsed: %v", err)
	}
	if usage.inputTokens != 3 || usage.outputTokens != 9 {
		t.Errorf("usage = %+v, want 3 in / 9 out", usage)
	}
}

// A body with usage nowhere still errors, and the error names the keys that
// were present so the next occurrence is diagnosable from the log alone.
func TestParseUsageFromPayloadNamesKeysWhenAbsent(t *testing.T) {
	_, err := parseUsageFromPayload([]byte(`{"id":"resp_1","status":"completed"}`))
	if err == nil {
		t.Fatal("expected an error when usage is absent")
	}
	if got := err.Error(); got != "usage payload missing usage (top-level keys: id,status)" {
		t.Errorf("error = %q", got)
	}
}
