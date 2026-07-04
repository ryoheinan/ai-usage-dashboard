package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/ryoheinan/ai-usage-dashboard/internal/pricing"
	"github.com/ryoheinan/ai-usage-dashboard/internal/store"
)

const (
	sourceCodex      = "codex"
	sourceClaudeCode = "claude-code"
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
	mux.HandleFunc("POST /v1/metrics", h.handleMetrics)
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

func (h *Handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxBody))
	if err != nil {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	var req colmetricspb.ExportMetricsServiceRequest
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "json") {
		err = protojson.Unmarshal(body, &req)
	} else {
		err = proto.Unmarshal(body, &req)
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("decode otlp metrics: %v", err), http.StatusBadRequest)
		return
	}

	events := h.normalizeMetrics(&req)
	if err := h.store.InsertEvents(r.Context(), events); err != nil {
		http.Error(w, fmt.Sprintf("store metric events: %v", err), http.StatusInternalServerError)
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
	source := detectSource(flat, name)
	if source == sourceClaudeCode && strings.HasPrefix(name, "api_") {
		name = "claude_code." + name
	}
	model := firstString(flat, "payload.model", "model", "codex.model", "gen_ai.request.model", "gen_ai.response.model")
	conversationID := firstString(flat, "conversation.id", "conversation_id", "codex.conversation_id", "thread.id", "thread_id", "payload.conversation_id", "payload.thread_id")
	kind := firstString(flat, "event.kind", "kind", "sse.event", "sse_event", "payload.kind")
	success, hasSuccess := firstBool(flat, "success", "codex.success", "http.status_code", "http.response.status_code", "status_code", "payload.success", "payload.status_code", "payload.http.status_code")
	duration := firstInt(flat, "duration_ms", "codex.duration_ms", "durationMilliseconds", "payload.duration_ms", "payload.durationMilliseconds")
	input := firstInt64(flat,
		"input_tokens",
		"input_token_count",
		"inputTokens",
		"usage.input_tokens",
		"usage.input_token_count",
		"usage.inputTokens",
		"codex.usage.input_tokens",
		"codex.usage.input_token_count",
		"codex.usage.inputTokens",
		"gen_ai.usage.input_tokens",
		"gen_ai.usage.input_token_count",
		"gen_ai.usage.inputTokens",
		"payload.usage.input_tokens",
		"payload.usage.input_token_count",
		"payload.usage.inputTokens",
		"payload.info.last_token_usage.input_tokens",
		"payload.info.last_token_usage.input_token_count",
		"payload.info.last_token_usage.inputTokens",
		"payload.info.total_token_usage.input_tokens",
		"payload.info.total_token_usage.input_token_count",
		"payload.info.total_token_usage.inputTokens",
	)
	cached := firstInt64(flat,
		"cached_input_tokens",
		"cache_read_tokens",
		"cached_token_count",
		"cachedInputTokens",
		"usage.cached_input_tokens",
		"usage.cache_read_tokens",
		"usage.cached_token_count",
		"usage.cachedInputTokens",
		"codex.usage.cached_input_tokens",
		"codex.usage.cached_token_count",
		"codex.usage.cachedInputTokens",
		"payload.usage.cached_input_tokens",
		"payload.usage.cached_token_count",
		"payload.usage.cachedInputTokens",
		"payload.info.last_token_usage.cached_input_tokens",
		"payload.info.last_token_usage.cached_token_count",
		"payload.info.last_token_usage.cachedInputTokens",
		"payload.info.total_token_usage.cached_input_tokens",
		"payload.info.total_token_usage.cached_token_count",
		"payload.info.total_token_usage.cachedInputTokens",
	)
	cacheCreation := firstInt64(flat,
		"cache_creation_tokens",
		"cache_creation_input_tokens",
		"usage.cache_creation_tokens",
		"usage.cache_creation_input_tokens",
		"payload.usage.cache_creation_tokens",
		"payload.usage.cache_creation_input_tokens",
	)
	output := firstInt64(flat,
		"output_tokens",
		"output_token_count",
		"outputTokens",
		"usage.output_tokens",
		"usage.output_token_count",
		"usage.outputTokens",
		"codex.usage.output_tokens",
		"codex.usage.output_token_count",
		"codex.usage.outputTokens",
		"gen_ai.usage.output_tokens",
		"gen_ai.usage.output_token_count",
		"gen_ai.usage.outputTokens",
		"payload.usage.output_tokens",
		"payload.usage.output_token_count",
		"payload.usage.outputTokens",
		"payload.info.last_token_usage.output_tokens",
		"payload.info.last_token_usage.output_token_count",
		"payload.info.last_token_usage.outputTokens",
		"payload.info.total_token_usage.output_tokens",
		"payload.info.total_token_usage.output_token_count",
		"payload.info.total_token_usage.outputTokens",
	)
	reasoning := firstInt64(flat,
		"reasoning_output_tokens",
		"reasoning_token_count",
		"reasoningOutputTokens",
		"usage.reasoning_output_tokens",
		"usage.reasoning_token_count",
		"usage.reasoningOutputTokens",
		"codex.usage.reasoning_output_tokens",
		"codex.usage.reasoning_token_count",
		"codex.usage.reasoningOutputTokens",
		"payload.usage.reasoning_output_tokens",
		"payload.usage.reasoning_token_count",
		"payload.usage.reasoningOutputTokens",
		"payload.info.last_token_usage.reasoning_output_tokens",
		"payload.info.last_token_usage.reasoning_token_count",
		"payload.info.last_token_usage.reasoningOutputTokens",
		"payload.info.total_token_usage.reasoning_output_tokens",
		"payload.info.total_token_usage.reasoning_token_count",
		"payload.info.total_token_usage.reasoningOutputTokens",
	)
	total := firstInt64(flat,
		"total_tokens",
		"tool_token_count",
		"totalTokens",
		"usage.total_tokens",
		"usage.tool_token_count",
		"usage.totalTokens",
		"codex.usage.total_tokens",
		"codex.usage.tool_token_count",
		"codex.usage.totalTokens",
		"gen_ai.usage.total_tokens",
		"gen_ai.usage.tool_token_count",
		"gen_ai.usage.totalTokens",
		"payload.usage.total_tokens",
		"payload.usage.tool_token_count",
		"payload.usage.totalTokens",
		"payload.info.last_token_usage.total_tokens",
		"payload.info.last_token_usage.tool_token_count",
		"payload.info.last_token_usage.totalTokens",
		"payload.info.total_token_usage.total_tokens",
		"payload.info.total_token_usage.tool_token_count",
		"payload.info.total_token_usage.totalTokens",
	)
	if total == 0 {
		total = input + output
		if source == sourceClaudeCode {
			total += cached + cacheCreation
		}
	}
	logDiagnosticFields(name, kind, flat, input, cached, output, reasoning, total)

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
		Source:                source,
		ConversationID:        conversationID,
		Model:                 model,
		Name:                  name,
		Kind:                  kind,
		Success:               successPtr,
		DurationMS:            durationPtr,
		InputTokens:           input,
		CachedInputTokens:     cached,
		CacheCreationTokens:   cacheCreation,
		OutputTokens:          output,
		ReasoningOutputTokens: reasoning,
		TotalTokens:           total,
		EstimatedCostUSD:      estimatedCost(flat, h.prices, model, input, cached, output),
		DroppedContentFields:  countContentFields(flat),
	}
}

type metricKey struct {
	model          string
	conversationID string
	timestamp      int64
}

type metricEvent struct {
	event store.Event
}

func (h *Handler) normalizeMetrics(req *colmetricspb.ExportMetricsServiceRequest) []store.Event {
	eventsByKey := make(map[metricKey]*metricEvent)
	for _, resourceMetrics := range req.ResourceMetrics {
		resourceAttrs := attrs(resourceMetrics.GetResource().GetAttributes())
		for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
			for _, metric := range scopeMetrics.Metrics {
				if metric.GetName() != "claude_code.token.usage" && metric.GetName() != "claude_code.cost.usage" {
					continue
				}
				for _, point := range metricPoints(metric) {
					pointAttrs := attrs(point.GetAttributes())
					flat := flatten(mergeAttrs(resourceAttrs, pointAttrs))
					source := detectSource(flat, metric.GetName())
					if source != sourceClaudeCode {
						source = sourceClaudeCode
					}
					model := firstString(flat, "model", "gen_ai.request.model")
					ts := metricTimestamp(point)
					key := metricKey{
						model:          model,
						conversationID: firstString(flat, "session.id", "conversation.id", "conversation_id"),
						timestamp:      ts.UnixNano(),
					}
					item := eventsByKey[key]
					if item == nil {
						item = &metricEvent{event: store.Event{
							Timestamp:      ts,
							Source:         source,
							ConversationID: key.conversationID,
							Model:          model,
							Name:           "claude_code.usage",
							Kind:           "metric",
						}}
						eventsByKey[key] = item
					}
					value := metricValue(point)
					switch metric.GetName() {
					case "claude_code.cost.usage":
						item.event.EstimatedCostUSD += value
					case "claude_code.token.usage":
						switch firstString(flat, "type") {
						case "input":
							item.event.InputTokens += int64(value)
						case "output":
							item.event.OutputTokens += int64(value)
						case "cacheRead":
							item.event.CachedInputTokens += int64(value)
						case "cacheCreation":
							item.event.CacheCreationTokens += int64(value)
						}
					}
				}
			}
		}
	}

	events := make([]store.Event, 0, len(eventsByKey))
	for _, item := range eventsByKey {
		item.event.TotalTokens = item.event.InputTokens + item.event.CachedInputTokens + item.event.CacheCreationTokens + item.event.OutputTokens
		events = append(events, item.event)
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].Timestamp.Equal(events[j].Timestamp) {
			return events[i].Model < events[j].Model
		}
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	return events
}

func mergeAttrs(first, second map[string]any) map[string]any {
	merged := make(map[string]any, len(first)+len(second))
	for key, value := range first {
		merged[key] = value
	}
	for key, value := range second {
		merged[key] = value
	}
	return merged
}

func metricPoints(metric *metricspb.Metric) []*metricspb.NumberDataPoint {
	if sum := metric.GetSum(); sum != nil {
		return sum.GetDataPoints()
	}
	if gauge := metric.GetGauge(); gauge != nil {
		return gauge.GetDataPoints()
	}
	return nil
}

func metricTimestamp(point *metricspb.NumberDataPoint) time.Time {
	if point.GetTimeUnixNano() > 0 {
		return time.Unix(0, int64(point.GetTimeUnixNano())).UTC()
	}
	return time.Now().UTC()
}

func metricValue(point *metricspb.NumberDataPoint) float64 {
	switch value := point.GetValue().(type) {
	case *metricspb.NumberDataPoint_AsDouble:
		return value.AsDouble
	case *metricspb.NumberDataPoint_AsInt:
		return float64(value.AsInt)
	default:
		return 0
	}
}

func detectSource(values map[string]any, name string) string {
	for _, key := range []string{"source", "ai.source", "tool.source"} {
		if value, ok := values[key].(string); ok {
			switch strings.ToLower(value) {
			case sourceCodex:
				return sourceCodex
			case sourceClaudeCode, "claude", "claude_code":
				return sourceClaudeCode
			}
		}
	}
	service := strings.ToLower(firstString(values, "service.name", "service_name", "telemetry.sdk.name"))
	switch {
	case strings.Contains(service, "claude"):
		return sourceClaudeCode
	case strings.Contains(service, "codex"):
		return sourceCodex
	case strings.HasPrefix(name, "claude_code."):
		return sourceClaudeCode
	case strings.HasPrefix(name, "codex."):
		return sourceCodex
	default:
		return sourceCodex
	}
}

func estimatedCost(values map[string]any, prices pricing.Catalog, model string, input, cached, output int64) float64 {
	if cost := firstFloat64(values, "cost_usd", "estimated_cost_usd", "usage.cost_usd", "payload.cost_usd", "payload.usage.cost_usd"); cost > 0 {
		return cost
	}
	return prices.EstimateUSD(model, input, cached, output)
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
		case []any:
			for index, child := range typed {
				childKey := strconv.Itoa(index)
				if prefix != "" {
					childKey = prefix + "." + childKey
				}
				walk(childKey, child)
			}
		case string:
			if decoded, ok := decodeJSONObjectOrArray(typed); ok {
				walk(prefix, decoded)
				return
			}
			out[prefix] = value
		default:
			out[prefix] = value
		}
	}
	for key, value := range values {
		walk(key, value)
	}
	return out
}

func decodeJSONObjectOrArray(raw string) (any, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false
	}
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return nil, false
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, false
	}
	switch decoded.(type) {
	case map[string]any, []any:
		return decoded, true
	default:
		return nil, false
	}
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

func firstFloat64(values map[string]any, keys ...string) float64 {
	for _, key := range keys {
		switch value := values[key].(type) {
		case int64:
			return float64(value)
		case int:
			return float64(value)
		case float64:
			return value
		case string:
			if parsed, err := strconv.ParseFloat(value, 64); err == nil {
				return parsed
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

func logDiagnosticFields(name, kind string, values map[string]any, input, cached, output, reasoning, total int64) {
	if os.Getenv("CUA_DEBUG_OTEL_KEYS") == "" {
		return
	}
	if !strings.Contains(name, "sse") && kind != "response.completed" {
		return
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		normalized := strings.ToLower(key)
		if strings.Contains(normalized, "token") ||
			strings.Contains(normalized, "usage") ||
			strings.Contains(normalized, "model") ||
			strings.Contains(normalized, "event") ||
			strings.Contains(normalized, "type") ||
			strings.Contains(normalized, "kind") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	slog.Info("otel diagnostic fields",
		"name", name,
		"kind", kind,
		"input_tokens", input,
		"cached_input_tokens", cached,
		"output_tokens", output,
		"reasoning_output_tokens", reasoning,
		"total_tokens", total,
		"keys", strings.Join(keys, ","),
	)
}
