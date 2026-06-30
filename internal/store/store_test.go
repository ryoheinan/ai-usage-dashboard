package store

import (
	"context"
	"testing"
	"time"
)

func TestStoreSummaryAndHealth(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	success := true
	duration := int64(125)
	err = db.InsertEvents(context.Background(), []Event{{
		Timestamp:            time.Now().UTC(),
		Model:                "gpt-test",
		Name:                 "codex.api_request",
		Success:              &success,
		DurationMS:           &duration,
		InputTokens:          100,
		CachedInputTokens:    20,
		OutputTokens:         50,
		TotalTokens:          150,
		EstimatedCostUSD:     0.001,
		DroppedContentFields: 2,
	}})
	if err != nil {
		t.Fatal(err)
	}

	summary, err := db.Summary(context.Background(), time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Events != 1 || summary.Requests != 1 || summary.TotalTokens != 150 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	health, err := db.IngestionHealth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if health.AcceptedEvents != 1 || health.DroppedContentFields != 2 || health.LastEventAt == nil {
		t.Fatalf("unexpected health: %+v", health)
	}
}
