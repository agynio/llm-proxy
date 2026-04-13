package proxy

import "testing"

func TestParseUsageFromPayloadOpenAIUsage(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":12,"output_tokens":34,"cached_tokens":5}}`)

	usage, err := parseUsageFromPayload(body)
	if err != nil {
		t.Fatalf("parse usage payload: %v", err)
	}
	if usage.inputTokens != 12 {
		t.Fatalf("expected input tokens 12, got %d", usage.inputTokens)
	}
	if usage.outputTokens != 34 {
		t.Fatalf("expected output tokens 34, got %d", usage.outputTokens)
	}
	if usage.cachedTokens != 5 {
		t.Fatalf("expected cached tokens 5, got %d", usage.cachedTokens)
	}
}

func TestParseUsageFromPayloadCachedTokensZero(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":7,"output_tokens":11,"cached_tokens":0}}`)

	usage, err := parseUsageFromPayload(body)
	if err != nil {
		t.Fatalf("parse usage payload: %v", err)
	}
	if usage.cachedTokens != 0 {
		t.Fatalf("expected cached tokens 0, got %d", usage.cachedTokens)
	}
}
