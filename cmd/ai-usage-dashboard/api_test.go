package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ryoheinan/ai-usage-dashboard/internal/store"
)

func TestAPIEmptyCollectionsEncodeAsArrays(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mux := http.NewServeMux()
	registerAPI(mux, db)

	for _, path := range []string{"/api/breakdown/models", "/api/breakdown/sources"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("%s status = %d body = %s", path, res.Code, res.Body.String())
		}
		var decoded []map[string]any
		if err := json.Unmarshal(res.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("%s returned non-array JSON: %v body = %s", path, err, res.Body.String())
		}
		if len(decoded) != 0 {
			t.Fatalf("%s returned %d rows, want 0", path, len(decoded))
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/series", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("/api/series status = %d body = %s", res.Code, res.Body.String())
	}
	var series []map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &series); err != nil {
		t.Fatalf("/api/series returned non-array JSON: %v body = %s", err, res.Body.String())
	}
	if len(series) != 7 {
		t.Fatalf("/api/series returned %d rows, want 7", len(series))
	}
	for _, row := range series {
		if row["totalTokens"] != float64(0) {
			t.Fatalf("/api/series row has tokens: %#v", row)
		}
	}
}

func TestAPISeriesDaysParamControlsFilledWindow(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mux := http.NewServeMux()
	registerAPI(mux, db)

	req := httptest.NewRequest(http.MethodGet, "/api/series?days=3", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("/api/series status = %d body = %s", res.Code, res.Body.String())
	}
	var series []map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &series); err != nil {
		t.Fatalf("/api/series returned non-array JSON: %v body = %s", err, res.Body.String())
	}
	if len(series) != 3 {
		t.Fatalf("/api/series returned %d rows, want 3", len(series))
	}
}

func TestAPIAllRangeIncludesOlderEvents(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	success := true
	old := time.Now().UTC().AddDate(0, 0, -45)
	if err := db.InsertEvents(context.Background(), []store.Event{{
		Timestamp:        old,
		Model:            "gpt-test",
		Name:             "codex.api_request",
		Success:          &success,
		InputTokens:      100,
		OutputTokens:     50,
		TotalTokens:      150,
		EstimatedCostUSD: 0.001,
	}}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	registerAPI(mux, db)

	req := httptest.NewRequest(http.MethodGet, "/api/summary?range=all", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("/api/summary status = %d body = %s", res.Code, res.Body.String())
	}
	var summary store.Summary
	if err := json.Unmarshal(res.Body.Bytes(), &summary); err != nil {
		t.Fatalf("/api/summary returned invalid JSON: %v body = %s", err, res.Body.String())
	}
	if summary.TotalTokens != 150 || summary.Requests != 1 {
		t.Fatalf("/api/summary?range=all = %+v, want old event included", summary)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/breakdown/models?range=all", nil)
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("/api/breakdown/models status = %d body = %s", res.Code, res.Body.String())
	}
	var models []store.BreakdownRow
	if err := json.Unmarshal(res.Body.Bytes(), &models); err != nil {
		t.Fatalf("/api/breakdown/models returned invalid JSON: %v body = %s", err, res.Body.String())
	}
	if len(models) != 1 || models[0].TotalTokens != 150 {
		t.Fatalf("/api/breakdown/models?range=all = %+v, want old event included", models)
	}
}

func TestAPISourceParamFiltersSummary(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now().UTC()
	success := true
	if err := db.InsertEvents(context.Background(), []store.Event{
		{
			Timestamp:   now,
			Source:      "codex",
			Model:       "gpt-test",
			Name:        "codex.api_request",
			Success:     &success,
			TotalTokens: 100,
		},
		{
			Timestamp:   now,
			Source:      "claude-code",
			Model:       "claude-test",
			Name:        "claude_code.api_request",
			Success:     &success,
			TotalTokens: 200,
		},
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	registerAPI(mux, db)

	req := httptest.NewRequest(http.MethodGet, "/api/summary?range=all&source=claude-code", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("/api/summary status = %d body = %s", res.Code, res.Body.String())
	}
	var summary store.Summary
	if err := json.Unmarshal(res.Body.Bytes(), &summary); err != nil {
		t.Fatalf("/api/summary returned invalid JSON: %v body = %s", err, res.Body.String())
	}
	if summary.Requests != 1 || summary.TotalTokens != 200 {
		t.Fatalf("filtered summary = %+v, want Claude row only", summary)
	}
}
