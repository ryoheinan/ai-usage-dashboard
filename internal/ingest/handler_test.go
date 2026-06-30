package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"

	"github.com/ryoheinan/ai-usage-analytics/internal/pricing"
	"github.com/ryoheinan/ai-usage-analytics/internal/store"
)

type memoryStore struct {
	events []store.Event
}

func (m *memoryStore) InsertEvents(_ context.Context, events []store.Event) error {
	m.events = append(m.events, events...)
	return nil
}

func TestHandleLogsStoresMetadataOnly(t *testing.T) {
	mem := &memoryStore{}
	handler := NewHandler(mem, pricing.Catalog{
		"gpt-test": {InputPerMTok: 1, CachedInputPerMTok: 0.1, OutputPerMTok: 5},
	})
	mux := http.NewServeMux()
	handler.Register(mux)

	payload := sampleRequest()
	body, err := proto.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", res.Code, res.Body.String())
	}
	if len(mem.events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(mem.events))
	}
	event := mem.events[0]
	if event.Name != "codex.sse_event" {
		t.Fatalf("Name = %q", event.Name)
	}
	if event.Model != "gpt-test" {
		t.Fatalf("Model = %q", event.Model)
	}
	if event.InputTokens != 1000 || event.CachedInputTokens != 250 || event.OutputTokens != 80 || event.TotalTokens != 1080 {
		t.Fatalf("unexpected tokens: %+v", event)
	}
	if event.DroppedContentFields == 0 {
		t.Fatalf("DroppedContentFields = 0, want content field detection")
	}
	if event.EstimatedCostUSD <= 0 {
		t.Fatalf("EstimatedCostUSD = %v, want positive", event.EstimatedCostUSD)
	}
}

func TestHandleLogsReadsNestedCodexTokenUsage(t *testing.T) {
	mem := &memoryStore{}
	handler := NewHandler(mem, pricing.Catalog{
		"gpt-test": {InputPerMTok: 1, CachedInputPerMTok: 0.1, OutputPerMTok: 5},
	})
	mux := http.NewServeMux()
	handler.Register(mux)

	body, err := proto.Marshal(nestedCodexUsageRequest())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", res.Code, res.Body.String())
	}
	if len(mem.events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(mem.events))
	}
	event := mem.events[0]
	if event.Name != "codex.sse_event" {
		t.Fatalf("Name = %q", event.Name)
	}
	if event.InputTokens != 1200 || event.CachedInputTokens != 300 || event.OutputTokens != 150 || event.ReasoningOutputTokens != 40 || event.TotalTokens != 1350 {
		t.Fatalf("unexpected nested usage: %+v", event)
	}
}

func TestHandleLogsReadsJSONStringBodyAndStringNumbers(t *testing.T) {
	mem := &memoryStore{}
	handler := NewHandler(mem, pricing.Catalog{
		"gpt-test": {InputPerMTok: 1, CachedInputPerMTok: 0.1, OutputPerMTok: 5},
	})
	mux := http.NewServeMux()
	handler.Register(mux)

	body, err := proto.Marshal(jsonStringBodyRequest())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", res.Code, res.Body.String())
	}
	if len(mem.events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(mem.events))
	}
	event := mem.events[0]
	if event.Name != "codex.sse_event" || event.Kind != "response.completed" {
		t.Fatalf("unexpected event identity: %+v", event)
	}
	if event.Success == nil || !*event.Success {
		t.Fatalf("Success = %v, want true", event.Success)
	}
	if event.InputTokens != 2000 || event.CachedInputTokens != 500 || event.OutputTokens != 300 || event.TotalTokens != 2300 {
		t.Fatalf("unexpected string body usage: %+v", event)
	}
}

func TestHandleLogsExpandsNestedJSONStringAttributes(t *testing.T) {
	mem := &memoryStore{}
	handler := NewHandler(mem, pricing.Catalog{
		"gpt-test": {InputPerMTok: 1, CachedInputPerMTok: 0.1, OutputPerMTok: 5},
	})
	mux := http.NewServeMux()
	handler.Register(mux)

	body, err := proto.Marshal(jsonStringAttributeRequest())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", res.Code, res.Body.String())
	}
	if len(mem.events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(mem.events))
	}
	event := mem.events[0]
	if event.Name != "codex.sse_event" || event.Kind != "response.completed" {
		t.Fatalf("unexpected event identity: %+v", event)
	}
	if event.Model != "gpt-test" {
		t.Fatalf("Model = %q", event.Model)
	}
	if event.InputTokens != 3000 || event.CachedInputTokens != 750 || event.OutputTokens != 450 || event.ReasoningOutputTokens != 25 || event.TotalTokens != 3450 {
		t.Fatalf("unexpected JSON attribute usage: %+v", event)
	}
}

func TestHandleLogsReadsCodexTokenCountFields(t *testing.T) {
	mem := &memoryStore{}
	handler := NewHandler(mem, pricing.Catalog{
		"gpt-test": {InputPerMTok: 1, CachedInputPerMTok: 0.1, OutputPerMTok: 5},
	})
	mux := http.NewServeMux()
	handler.Register(mux)

	body, err := proto.Marshal(tokenCountFieldRequest())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", res.Code, res.Body.String())
	}
	if len(mem.events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(mem.events))
	}
	event := mem.events[0]
	if event.InputTokens != 4000 || event.CachedInputTokens != 1000 || event.OutputTokens != 600 || event.ReasoningOutputTokens != 50 || event.TotalTokens != 4600 {
		t.Fatalf("unexpected token count fields: %+v", event)
	}
	if event.EstimatedCostUSD <= 0 {
		t.Fatalf("EstimatedCostUSD = %v, want positive", event.EstimatedCostUSD)
	}
}

func sampleRequest() *collogspb.ExportLogsServiceRequest {
	return &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{{
			Resource: &resourcepb.Resource{
				Attributes: []*commonpb.KeyValue{
					kv("service.name", "codex"),
					kv("codex.conversation_id", "conv_123"),
					kv("model", "gpt-test"),
				},
			},
			ScopeLogs: []*logspb.ScopeLogs{{
				LogRecords: []*logspb.LogRecord{{
					TimeUnixNano: uint64(time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC).UnixNano()),
					Attributes: []*commonpb.KeyValue{
						kv("event.name", "codex.sse_event"),
						kv("kind", "response.completed"),
						kvi("input_tokens", 1000),
						kvi("cached_input_tokens", 250),
						kvi("output_tokens", 80),
						kvi("total_tokens", 1080),
						kv("prompt", "do not store this"),
					},
				}},
			}},
		}},
	}
}

func jsonStringBodyRequest() *collogspb.ExportLogsServiceRequest {
	body := map[string]any{
		"type": "codex.sse_event",
		"payload": map[string]any{
			"kind":        "response.completed",
			"status_code": "200",
			"usage": map[string]any{
				"input_tokens":        "2000",
				"cached_input_tokens": "500",
				"output_tokens":       "300",
				"total_tokens":        "2300",
			},
		},
	}
	encoded, _ := json.Marshal(body)
	return &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{{
			Resource: &resourcepb.Resource{
				Attributes: []*commonpb.KeyValue{
					kv("model", "gpt-test"),
				},
			},
			ScopeLogs: []*logspb.ScopeLogs{{
				LogRecords: []*logspb.LogRecord{{
					TimeUnixNano: uint64(time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC).UnixNano()),
					Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{
						StringValue: string(encoded),
					}},
				}},
			}},
		}},
	}
}

func jsonStringAttributeRequest() *collogspb.ExportLogsServiceRequest {
	payload := map[string]any{
		"kind":  "response.completed",
		"model": "gpt-test",
		"usage": map[string]any{
			"input_tokens":            3000,
			"cached_input_tokens":     750,
			"output_tokens":           450,
			"reasoning_output_tokens": 25,
			"total_tokens":            3450,
		},
	}
	encodedPayload, _ := json.Marshal(payload)
	return &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{{
			Resource: &resourcepb.Resource{
				Attributes: []*commonpb.KeyValue{
					kv("model", "gpt-fallback"),
				},
			},
			ScopeLogs: []*logspb.ScopeLogs{{
				LogRecords: []*logspb.LogRecord{{
					TimeUnixNano: uint64(time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC).UnixNano()),
					Attributes: []*commonpb.KeyValue{
						kv("event.name", "codex.sse_event"),
						kv("payload", string(encodedPayload)),
					},
				}},
			}},
		}},
	}
}

func tokenCountFieldRequest() *collogspb.ExportLogsServiceRequest {
	return &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{{
			Resource: &resourcepb.Resource{
				Attributes: []*commonpb.KeyValue{
					kv("model", "gpt-test"),
				},
			},
			ScopeLogs: []*logspb.ScopeLogs{{
				LogRecords: []*logspb.LogRecord{{
					TimeUnixNano: uint64(time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC).UnixNano()),
					Attributes: []*commonpb.KeyValue{
						kv("event.name", "codex.sse_event"),
						kv("event.kind", "response.completed"),
						kvi("input_token_count", 4000),
						kvi("cached_token_count", 1000),
						kvi("output_token_count", 600),
						kvi("reasoning_token_count", 50),
						kvi("tool_token_count", 4600),
					},
				}},
			}},
		}},
	}
}

func nestedCodexUsageRequest() *collogspb.ExportLogsServiceRequest {
	return &collogspb.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{{
			Resource: &resourcepb.Resource{
				Attributes: []*commonpb.KeyValue{
					kv("model", "gpt-test"),
				},
			},
			ScopeLogs: []*logspb.ScopeLogs{{
				LogRecords: []*logspb.LogRecord{{
					TimeUnixNano: uint64(time.Date(2026, 6, 30, 1, 2, 3, 0, time.UTC).UnixNano()),
					Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_KvlistValue{KvlistValue: &commonpb.KeyValueList{
						Values: []*commonpb.KeyValue{
							kv("type", "codex.sse_event"),
							{
								Key: "payload",
								Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_KvlistValue{KvlistValue: &commonpb.KeyValueList{
									Values: []*commonpb.KeyValue{
										kv("kind", "response.completed"),
										{
											Key: "info",
											Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_KvlistValue{KvlistValue: &commonpb.KeyValueList{
												Values: []*commonpb.KeyValue{
													{
														Key: "total_token_usage",
														Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_KvlistValue{KvlistValue: &commonpb.KeyValueList{
															Values: []*commonpb.KeyValue{
																kvi("input_tokens", 1200),
																kvi("cached_input_tokens", 300),
																kvi("output_tokens", 150),
																kvi("reasoning_output_tokens", 40),
																kvi("total_tokens", 1350),
															},
														}}},
													},
												},
											}}},
										},
									},
								}}},
							},
						},
					}}},
				}},
			}},
		}},
	}
}

func kv(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key: key,
		Value: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: value},
		},
	}
}

func kvi(key string, value int64) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key: key,
		Value: &commonpb.AnyValue{
			Value: &commonpb.AnyValue_IntValue{IntValue: value},
		},
	}
}
