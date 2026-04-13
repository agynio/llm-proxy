package proxy

import (
	"testing"

	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
	meteringv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/metering/v1"
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

func TestUsageFromInfoCachedTokensTopLevel(t *testing.T) {
	details := &struct {
		CachedTokens int64 `json:"cached_tokens"`
	}{CachedTokens: 4}
	info := &usageInfo{InputTokens: 2, OutputTokens: 3, CachedTokens: 7, InputTokensDetails: details}

	usage := usageFromInfo(info)
	if usage.inputTokens != 2 {
		t.Fatalf("expected input tokens 2, got %d", usage.inputTokens)
	}
	if usage.outputTokens != 3 {
		t.Fatalf("expected output tokens 3, got %d", usage.outputTokens)
	}
	if usage.cachedTokens != 7 {
		t.Fatalf("expected cached tokens 7, got %d", usage.cachedTokens)
	}
}

func TestUsageFromInfoCachedTokensFallback(t *testing.T) {
	details := &struct {
		CachedTokens int64 `json:"cached_tokens"`
	}{CachedTokens: 6}
	info := &usageInfo{InputTokens: 5, OutputTokens: 8, CachedTokens: 0, InputTokensDetails: details}

	usage := usageFromInfo(info)
	if usage.cachedTokens != 6 {
		t.Fatalf("expected cached tokens 6, got %d", usage.cachedTokens)
	}
}

func TestBuildUsageRecordsSuccessWithCached(t *testing.T) {
	meta := meteringMetadata{callID: "call-1", orgID: "org-1", modelID: "model-1", modelName: "model", threadID: "thread"}
	usage := usageCounts{inputTokens: 10, cachedTokens: 2, outputTokens: 4}

	records := buildUsageRecords(meta, &usage, meteringStatusSuccess)
	if len(records) != 4 {
		t.Fatalf("expected 4 records, got %d", len(records))
	}
	counts := recordKinds(records)
	if counts[meteringKindInput] != 1 {
		t.Fatalf("expected 1 input record, got %d", counts[meteringKindInput])
	}
	if counts[meteringKindCached] != 1 {
		t.Fatalf("expected 1 cached record, got %d", counts[meteringKindCached])
	}
	if counts[meteringKindOutput] != 1 {
		t.Fatalf("expected 1 output record, got %d", counts[meteringKindOutput])
	}
	if counts[meteringKindRequest] != 1 {
		t.Fatalf("expected 1 request record, got %d", counts[meteringKindRequest])
	}
}

func TestBuildUsageRecordsSuccessWithoutCached(t *testing.T) {
	meta := meteringMetadata{callID: "call-1", orgID: "org-1", modelID: "model-1", modelName: "model", threadID: "thread"}
	usage := usageCounts{inputTokens: 10, cachedTokens: 0, outputTokens: 4}

	records := buildUsageRecords(meta, &usage, meteringStatusSuccess)
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	counts := recordKinds(records)
	if counts[meteringKindCached] != 0 {
		t.Fatalf("expected 0 cached records, got %d", counts[meteringKindCached])
	}
	if counts[meteringKindRequest] != 1 {
		t.Fatalf("expected 1 request record, got %d", counts[meteringKindRequest])
	}
}

func TestBuildUsageRecordsFailure(t *testing.T) {
	meta := meteringMetadata{callID: "call-1", orgID: "org-1", modelID: "model-1", modelName: "model", threadID: "thread"}
	usage := usageCounts{inputTokens: 10, cachedTokens: 2, outputTokens: 4}

	records := buildUsageRecords(meta, &usage, meteringStatusFailed)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	record := records[0]
	if record.Unit != meteringv1.Unit_UNIT_COUNT {
		t.Fatalf("expected count unit, got %v", record.Unit)
	}
	if record.Labels["kind"] != meteringKindRequest {
		t.Fatalf("expected request kind, got %q", record.Labels["kind"])
	}
	if record.Labels["status"] != meteringStatusFailed {
		t.Fatalf("expected failed status, got %q", record.Labels["status"])
	}
}

func TestBuildUsageRecordsGuard(t *testing.T) {
	usage := usageCounts{inputTokens: 1, cachedTokens: 0, outputTokens: 1}

	missingCall := meteringMetadata{orgID: "org-1"}
	if records := buildUsageRecords(missingCall, &usage, meteringStatusSuccess); records != nil {
		t.Fatalf("expected nil records for missing call ID")
	}

	missingOrg := meteringMetadata{callID: "call-1"}
	if records := buildUsageRecords(missingOrg, &usage, meteringStatusSuccess); records != nil {
		t.Fatalf("expected nil records for missing org ID")
	}
}

func recordKinds(records []*meteringv1.UsageRecord) map[string]int {
	counts := make(map[string]int)
	for _, record := range records {
		counts[record.Labels["kind"]]++
	}
	return counts
}
