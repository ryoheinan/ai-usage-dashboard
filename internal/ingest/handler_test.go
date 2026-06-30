package ingest

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"

	"github.com/ryohei/codex-usage-analytics/internal/pricing"
	"github.com/ryohei/codex-usage-analytics/internal/store"
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
