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

func TestStoreSeriesFillsEmptyDays(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now().UTC()
	success := true
	err = db.InsertEvents(context.Background(), []Event{{
		Timestamp:    now,
		Model:        "gpt-test",
		Name:         "codex.api_request",
		Success:      &success,
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
	}})
	if err != nil {
		t.Fatal(err)
	}

	series, err := db.Series(context.Background(), now.AddDate(0, 0, -6))
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 7 {
		t.Fatalf("len(Series()) = %d, want 7: %#v", len(series), series)
	}
	if series[0].Bucket != now.AddDate(0, 0, -6).Format("2006-01-02") {
		t.Fatalf("first bucket = %q, want six days ago", series[0].Bucket)
	}
	last := series[len(series)-1]
	if last.Bucket != now.Format("2006-01-02") || last.TotalTokens != 150 || last.Requests != 1 {
		t.Fatalf("last bucket = %+v, want today's totals", last)
	}
	for _, point := range series[:len(series)-1] {
		if point.TotalTokens != 0 || point.Events != 0 {
			t.Fatalf("empty day has data: %+v", point)
		}
	}
}

func TestStoreReturnsEmptySlices(t *testing.T) {
	db, err := Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	series, err := db.Series(context.Background(), time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if series == nil || len(series) != 1 || series[0].TotalTokens != 0 {
		t.Fatalf("Series() = %#v, want one zero-filled point", series)
	}

	models, err := db.ModelBreakdown(context.Background(), time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if models == nil || len(models) != 0 {
		t.Fatalf("ModelBreakdown() = %#v, want empty non-nil slice", models)
	}
}
