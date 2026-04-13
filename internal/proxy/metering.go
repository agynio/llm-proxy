package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	llmv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/llm/v1"
	meteringv1 "github.com/agynio/llm-proxy/.gen/go/agynio/api/metering/v1"
	"github.com/agynio/llm-proxy/internal/identity"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	meteringProducer         = "llm-proxy"
	meteringMicroUnits int64 = 1_000_000

	meteringStatusSuccess = "success"
	meteringStatusFailed  = "failed"

	meteringKindInput   = "input"
	meteringKindCached  = "cached"
	meteringKindOutput  = "output"
	meteringKindRequest = "request"
)

type MeteringRecorder interface {
	Record(ctx context.Context, req *meteringv1.RecordRequest, opts ...grpc.CallOption) (*meteringv1.RecordResponse, error)
}

type meteringMetadata struct {
	callID    string
	orgID     string
	modelID   string
	modelName string
	threadID  string
	identity  identity.ResolvedIdentity
}

type usageCounts struct {
	inputTokens  int64
	cachedTokens int64
	outputTokens int64
}

type usageInfo struct {
	InputTokens        int64 `json:"input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	CachedTokens       int64 `json:"cached_tokens"`
	InputTokensDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

type responseUsagePayload struct {
	Usage *usageInfo `json:"usage"`
}

type openAICompletedPayload struct {
	Response *responseUsagePayload `json:"response"`
}

type anthropicDeltaPayload struct {
	Usage *usageInfo `json:"usage"`
}

func usageFromInfo(info *usageInfo) usageCounts {
	cachedTokens := info.CachedTokens
	if cachedTokens == 0 && info.InputTokensDetails != nil {
		cachedTokens = info.InputTokensDetails.CachedTokens
	}
	return usageCounts{
		inputTokens:  info.InputTokens,
		cachedTokens: cachedTokens,
		outputTokens: info.OutputTokens,
	}
}

func parseUsageFromPayload(body []byte) (usageCounts, error) {
	if len(body) == 0 {
		return usageCounts{}, fmt.Errorf("usage payload is empty")
	}

	var payload responseUsagePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return usageCounts{}, fmt.Errorf("parse usage payload: %w", err)
	}
	if payload.Usage == nil {
		return usageCounts{}, fmt.Errorf("usage payload missing usage")
	}
	return usageFromInfo(payload.Usage), nil
}

func parseUsageFromEvent(protocol llmv1.Protocol, eventType string, data string) (usageCounts, bool, error) {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return usageCounts{}, false, nil
	}

	switch protocol {
	case llmv1.Protocol_PROTOCOL_RESPONSES:
		if eventType != "response.completed" {
			return usageCounts{}, false, nil
		}
		var payload openAICompletedPayload
		if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
			return usageCounts{}, true, fmt.Errorf("parse response.completed: %w", err)
		}
		if payload.Response == nil || payload.Response.Usage == nil {
			return usageCounts{}, true, fmt.Errorf("response.completed missing usage")
		}
		return usageFromInfo(payload.Response.Usage), true, nil
	case llmv1.Protocol_PROTOCOL_ANTHROPIC_MESSAGES:
		if eventType != "message_delta" {
			return usageCounts{}, false, nil
		}
		var payload anthropicDeltaPayload
		if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
			return usageCounts{}, true, fmt.Errorf("parse message_delta: %w", err)
		}
		if payload.Usage == nil {
			return usageCounts{}, true, fmt.Errorf("message_delta missing usage")
		}
		return usageFromInfo(payload.Usage), true, nil
	default:
		return usageCounts{}, false, nil
	}
}

func (h *Handler) recordMetering(meta meteringMetadata, usage *usageCounts, status string) {
	if h.meteringClient == nil {
		return
	}

	records := buildUsageRecords(meta, usage, status)
	if len(records) == 0 {
		return
	}

	go func(records []*meteringv1.UsageRecord) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err := h.meteringClient.Record(ctx, &meteringv1.RecordRequest{Records: records}); err != nil {
			log.Printf("proxy: metering record failed: %v", err)
		}
	}(records)
}

func buildUsageRecords(meta meteringMetadata, usage *usageCounts, status string) []*meteringv1.UsageRecord {
	if meta.callID == "" || meta.orgID == "" {
		return nil
	}

	timestamp := timestamppb.New(time.Now().UTC())
	baseLabels := map[string]string{
		"resource_id":   meta.modelID,
		"resource":      meta.modelName,
		"identity_id":   meta.identity.IdentityID,
		"identity_type": string(meta.identity.IdentityType),
		"thread_id":     meta.threadID,
	}

	records := make([]*meteringv1.UsageRecord, 0, 4)

	if status == meteringStatusSuccess && usage != nil {
		records = append(records, newUsageRecord(meta, timestamp, meteringv1.Unit_UNIT_TOKENS, usage.inputTokens*meteringMicroUnits,
			withLabels(baseLabels, map[string]string{"kind": meteringKindInput}), "input"))
		if usage.cachedTokens > 0 {
			records = append(records, newUsageRecord(meta, timestamp, meteringv1.Unit_UNIT_TOKENS, usage.cachedTokens*meteringMicroUnits,
				withLabels(baseLabels, map[string]string{"kind": meteringKindCached}), "cached"))
		}
		records = append(records, newUsageRecord(meta, timestamp, meteringv1.Unit_UNIT_TOKENS, usage.outputTokens*meteringMicroUnits,
			withLabels(baseLabels, map[string]string{"kind": meteringKindOutput}), "output"))
	}

	requestLabels := withLabels(baseLabels, map[string]string{"kind": meteringKindRequest, "status": status})
	records = append(records, newUsageRecord(meta, timestamp, meteringv1.Unit_UNIT_COUNT, meteringMicroUnits, requestLabels, "request"))

	return records
}

func newUsageRecord(meta meteringMetadata, timestamp *timestamppb.Timestamp, unit meteringv1.Unit, value int64, labels map[string]string, suffix string) *meteringv1.UsageRecord {
	return &meteringv1.UsageRecord{
		OrgId:          meta.orgID,
		IdempotencyKey: fmt.Sprintf("%s-%s", meta.callID, suffix),
		Producer:       meteringProducer,
		Timestamp:      timestamp,
		Labels:         labels,
		Unit:           unit,
		Value:          value,
	}
}

func withLabels(base map[string]string, extra map[string]string) map[string]string {
	labels := make(map[string]string, len(base)+len(extra))
	for key, value := range base {
		labels[key] = value
	}
	for key, value := range extra {
		labels[key] = value
	}
	return labels
}
