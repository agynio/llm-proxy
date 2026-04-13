package proxy

import (
	"testing"

	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
)

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

func TestParseUsageFromEventOpenAICompleted(t *testing.T) {
	data := `{"response":{"usage":{"input_tokens":3,"output_tokens":9,"cached_tokens":2}}}`

	usage, matched, err := parseUsageFromEvent(llmv1.Protocol_PROTOCOL_RESPONSES, "response.completed", data)
	if err != nil {
		t.Fatalf("parse event usage: %v", err)
	}
	if !matched {
		t.Fatalf("expected matched event")
	}
	if usage.inputTokens != 3 {
		t.Fatalf("expected input tokens 3, got %d", usage.inputTokens)
	}
	if usage.outputTokens != 9 {
		t.Fatalf("expected output tokens 9, got %d", usage.outputTokens)
	}
	if usage.cachedTokens != 2 {
		t.Fatalf("expected cached tokens 2, got %d", usage.cachedTokens)
	}
}

func TestParseUsageFromEventOpenAINonTerminal(t *testing.T) {
	usage, matched, err := parseUsageFromEvent(llmv1.Protocol_PROTOCOL_RESPONSES, "response.created", `{"response":{}}`)
	if err != nil {
		t.Fatalf("parse event usage: %v", err)
	}
	if matched {
		t.Fatalf("expected unmatched event")
	}
	if usage != (usageCounts{}) {
		t.Fatalf("expected empty usage counts")
	}
}

func TestParseUsageFromEventAnthropicDelta(t *testing.T) {
	data := `{"usage":{"input_tokens":4,"output_tokens":5,"cached_tokens":1}}`

	usage, matched, err := parseUsageFromEvent(llmv1.Protocol_PROTOCOL_ANTHROPIC_MESSAGES, "message_delta", data)
	if err != nil {
		t.Fatalf("parse event usage: %v", err)
	}
	if !matched {
		t.Fatalf("expected matched event")
	}
	if usage.inputTokens != 4 {
		t.Fatalf("expected input tokens 4, got %d", usage.inputTokens)
	}
	if usage.outputTokens != 5 {
		t.Fatalf("expected output tokens 5, got %d", usage.outputTokens)
	}
	if usage.cachedTokens != 1 {
		t.Fatalf("expected cached tokens 1, got %d", usage.cachedTokens)
	}
}

func TestParseUsageFromEventAnthropicNonTerminal(t *testing.T) {
	usage, matched, err := parseUsageFromEvent(llmv1.Protocol_PROTOCOL_ANTHROPIC_MESSAGES, "message_start", `{"usage":{}}`)
	if err != nil {
		t.Fatalf("parse event usage: %v", err)
	}
	if matched {
		t.Fatalf("expected unmatched event")
	}
	if usage != (usageCounts{}) {
		t.Fatalf("expected empty usage counts")
	}
}

func TestParseUsageFromEventUnknownProtocol(t *testing.T) {
	usage, matched, err := parseUsageFromEvent(llmv1.Protocol_PROTOCOL_UNSPECIFIED, "response.completed", `{"response":{}}`)
	if err != nil {
		t.Fatalf("parse event usage: %v", err)
	}
	if matched {
		t.Fatalf("expected unmatched event")
	}
	if usage != (usageCounts{}) {
		t.Fatalf("expected empty usage counts")
	}
}
