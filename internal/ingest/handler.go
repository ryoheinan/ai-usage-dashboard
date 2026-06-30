package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/ryoheinan/ai-usage-analytics/internal/pricing"
	"github.com/ryoheinan/ai-usage-analytics/internal/store"
)

type Handler struct {
	store   eventStore
	prices  pricing.Catalog
	maxBody int64
}

type eventStore interface {
	InsertEvents(ctx context.Context, events []store.Event) error
}

func NewHandler(db interface {
	InsertEvents(ctx context.Context, events []store.Event) error
}, prices pricing.Catalog) *Handler {
	return &Handler{store: db, prices: prices, maxBody: 8 << 20}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/logs", h.handleLogs)
}

func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxBody))
	if err != nil {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	var req collogspb.ExportLogsServiceRequest
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "json") {
		err = protojson.Unmarshal(body, &req)
	} else {
		err = proto.Unmarshal(body, &req)
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("decode otlp logs: %v", err), http.StatusBadRequest)
		return
	}

	events := h.normalize(&req)
	if err := h.store.InsertEvents(r.Context(), events); err != nil {
		http.Error(w, fmt.Sprintf("store events: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"accepted": len(events)})
}

func (h *Handler) normalize(req *collogspb.ExportLogsServiceRequest) []store.Event {
	var events []store.Event
	for _, resourceLogs := range req.ResourceLogs {
		resourceAttrs := attrs(resourceLogs.GetResource().GetAttributes())
		for _, scopeLogs := range resourceLogs.ScopeLogs {
			for _, record := range scopeLogs.LogRecords {
				event := h.normalizeRecord(resourceAttrs, record)
				if event.Name == "" {
					continue
				}
				events = append(events, event)
			}
		}
	}
	return events
}

func (h *Handler) normalizeRecord(resourceAttrs map[string]any, record *logspb.LogRecord) store.Event {
	recordAttrs := attrs(record.GetAttributes())
	merged := make(map[string]any, len(resourceAttrs)+len(recordAttrs)+8)
	for key, value := range resourceAttrs {
		merged[key] = value
	}
	for key, value := range recordAttrs {
		merged[key] = value
	}
	if bodyMap := anyMap(record.GetBody()); len(bodyMap) > 0 {
		for key, value := range bodyMap {
			if _, exists := merged[key]; !exists {
				merged[key] = value
			}
		}
	}
	flat := flatten(merged)

	name := firstString(flat, "event.name", "name", "codex.event_name", "type", "payload.type")
	model := firstString(flat, "model", "codex.model", "gen_ai.request.model", "gen_ai.response.model", "payload.model")
	conversationID := firstString(flat, "conversation.id", "conversation_id", "codex.conversation_id", "thread.id", "thread_id", "payload.conversation_id", "payload.thread_id")
	kind := firstString(flat, "event.kind", "kind", "sse.event", "sse_event", "payload.kind")
	success, hasSuccess := firstBool(flat, "success", "codex.success", "http.status_code", "http.response.status_code", "status_code", "payload.success", "payload.status_code", "payload.http.status_code")
	duration := firstInt(flat, "duration_ms", "codex.duration_ms", "durationMilliseconds", "payload.duration_ms", "payload.durationMilliseconds")
	input := firstInt64(flat,
		"input_tokens",
		"usage.input_tokens",
		"codex.usage.input_tokens",
		"gen_ai.usage.input_tokens",
		"payload.usage.input_tokens",
		"payload.info.last_token_usage.input_tokens",
		"payload.info.total_token_usage.input_tokens",
	)
	cached := firstInt64(flat,
		"cached_input_tokens",
		"usage.cached_input_tokens",
		"codex.usage.cached_input_tokens",
		"payload.usage.cached_input_tokens",
		"payload.info.last_token_usage.cached_input_tokens",
		"payload.info.total_token_usage.cached_input_tokens",
	)
	output := firstInt64(flat,
		"output_tokens",
		"usage.output_tokens",
		"codex.usage.output_tokens",
		"gen_ai.usage.output_tokens",
		"payload.usage.output_tokens",
		"payload.info.last_token_usage.output_tokens",
		"payload.info.total_token_usage.output_tokens",
	)
	reasoning := firstInt64(flat,
		"reasoning_output_tokens",
		"usage.reasoning_output_tokens",
		"codex.usage.reasoning_output_tokens",
		"payload.usage.reasoning_output_tokens",
		"payload.info.last_token_usage.reasoning_output_tokens",
		"payload.info.total_token_usage.reasoning_output_tokens",
	)
	total := firstInt64(flat,
		"total_tokens",
		"usage.total_tokens",
		"codex.usage.total_tokens",
		"gen_ai.usage.total_tokens",
		"payload.usage.total_tokens",
		"payload.info.last_token_usage.total_tokens",
		"payload.info.total_token_usage.total_tokens",
	)
	if total == 0 {
		total = input + output
	}

	var successPtr *bool
	if hasSuccess {
		successPtr = &success
	}
	var durationPtr *int64
	if duration > 0 {
		durationPtr = &duration
	}

	return store.Event{
		Timestamp:             timestamp(record),
		ConversationID:        conversationID,
		Model:                 model,
		Name:                  name,
		Kind:                  kind,
		Success:               successPtr,
		DurationMS:            durationPtr,
		InputTokens:           input,
		CachedInputTokens:     cached,
		OutputTokens:          output,
		ReasoningOutputTokens: reasoning,
		TotalTokens:           total,
		EstimatedCostUSD:      h.prices.EstimateUSD(model, input, cached, output),
		DroppedContentFields:  countContentFields(flat),
	}
}

func flatten(values map[string]any) map[string]any {
	out := make(map[string]any)
	var walk func(prefix string, value any)
	walk = func(prefix string, value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				childKey := key
				if prefix != "" {
					childKey = prefix + "." + key
				}
				walk(childKey, child)
			}
		default:
			out[prefix] = value
		}
	}
	for key, value := range values {
		walk(key, value)
	}
	return out
}

func attrs(kvs []*commonpb.KeyValue) map[string]any {
	out := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		out[kv.GetKey()] = value(kv.GetValue())
	}
	return out
}

func anyMap(v *commonpb.AnyValue) map[string]any {
	switch value := value(v).(type) {
	case map[string]any:
		return value
	case string:
		var decoded map[string]any
		if err := json.Unmarshal([]byte(value), &decoded); err == nil {
			return decoded
		}
	}
	return nil
}

func value(v *commonpb.AnyValue) any {
	switch typed := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return typed.StringValue
	case *commonpb.AnyValue_BoolValue:
		return typed.BoolValue
	case *commonpb.AnyValue_IntValue:
		return typed.IntValue
	case *commonpb.AnyValue_DoubleValue:
		return typed.DoubleValue
	case *commonpb.AnyValue_ArrayValue:
		out := make([]any, 0, len(typed.ArrayValue.Values))
		for _, item := range typed.ArrayValue.Values {
			out = append(out, value(item))
		}
		return out
	case *commonpb.AnyValue_KvlistValue:
		out := make(map[string]any, len(typed.KvlistValue.Values))
		for _, item := range typed.KvlistValue.Values {
			out[item.GetKey()] = value(item.GetValue())
		}
		return out
	case *commonpb.AnyValue_BytesValue:
		return nil
	default:
		return nil
	}
}

func timestamp(record *logspb.LogRecord) time.Time {
	if record.TimeUnixNano > 0 {
		return time.Unix(0, int64(record.TimeUnixNano)).UTC()
	}
	if record.ObservedTimeUnixNano > 0 {
		return time.Unix(0, int64(record.ObservedTimeUnixNano)).UTC()
	}
	return time.Now().UTC()
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := values[key].(type) {
		case string:
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func firstBool(values map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		switch value := values[key].(type) {
		case bool:
			return value, true
		case int64:
			if strings.Contains(key, "status_code") {
				return value >= 200 && value < 400, true
			}
		case float64:
			if strings.Contains(key, "status_code") {
				return value >= 200 && value < 400, true
			}
		case string:
			if parsed, err := strconv.ParseBool(value); err == nil {
				return parsed, true
			}
			if strings.Contains(key, "status_code") {
				if parsed, err := strconv.ParseFloat(value, 64); err == nil {
					return parsed >= 200 && parsed < 400, true
				}
			}
		}
	}
	return false, false
}

func firstInt(values map[string]any, keys ...string) int64 {
	return firstInt64(values, keys...)
}

func firstInt64(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch value := values[key].(type) {
		case int64:
			return value
		case int:
			return int64(value)
		case float64:
			return int64(value)
		case string:
			if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
				return parsed
			}
			if parsed, err := strconv.ParseFloat(value, 64); err == nil {
				return int64(parsed)
			}
		}
	}
	return 0
}

func countContentFields(values map[string]any) int {
	count := 0
	for key := range values {
		normalized := strings.ToLower(key)
		if strings.Contains(normalized, "prompt") ||
			strings.Contains(normalized, "content") ||
			strings.Contains(normalized, "message") ||
			strings.Contains(normalized, "snippet") ||
			strings.Contains(normalized, "output") && !strings.Contains(normalized, "tokens") {
			count++
		}
	}
	return count
}
