package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestAPIExportJSONHonorsFiltersAndDownloadHeaders(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "/api/export.json?range=all&source=claude-code", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("/api/export.json status = %d body = %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := res.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment;") || !strings.Contains(got, ".json") {
		t.Fatalf("Content-Disposition = %q, want JSON attachment", got)
	}
	var bundle exportBundle
	if err := json.Unmarshal(res.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("export JSON invalid: %v body = %s", err, res.Body.String())
	}
	if bundle.SchemaVersion != exportSchemaVersion || bundle.App != exportAppName {
		t.Fatalf("export bundle metadata = %+v", bundle)
	}
	if len(bundle.Events) != 1 || bundle.Events[0].Source != "claude-code" || bundle.Events[0].TotalTokens != 200 {
		t.Fatalf("exported events = %+v, want filtered Claude row", bundle.Events)
	}
}

func TestAPIExportCSVHonorsFiltersAndDownloadHeaders(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "/api/export.csv?range=all&source=codex", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("/api/export.csv status = %d body = %s", res.Code, res.Body.String())
	}
	if got := res.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/csv; charset=utf-8", got)
	}
	if got := res.Header().Get("Content-Disposition"); !strings.Contains(got, "attachment;") || !strings.Contains(got, ".csv") {
		t.Fatalf("Content-Disposition = %q, want CSV attachment", got)
	}
	records, err := csv.NewReader(strings.NewReader(res.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("export CSV invalid: %v body = %s", err, res.Body.String())
	}
	if len(records) != 2 {
		t.Fatalf("CSV rows = %d, want header plus one row: %#v", len(records), records)
	}
	if records[0][0] != "timestamp" || records[0][14] != "estimated_cost_usd" {
		t.Fatalf("CSV header = %#v", records[0])
	}
	if records[1][1] != "codex" || records[1][13] != "100" {
		t.Fatalf("CSV row = %#v, want filtered Codex row", records[1])
	}
}

func TestAPIImportMergeAndReplace(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mux := http.NewServeMux()
	registerAPI(mux, db)

	success := true
	event := store.PortableEvent{
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
		Source:      "codex",
		Model:       "gpt-test",
		Name:        "codex.api_request",
		Success:     &success,
		TotalTokens: 100,
	}
	req := multipartImportRequest(t, "merge", exportBundle{
		SchemaVersion: exportSchemaVersion,
		ExportedAt:    time.Now().UTC(),
		App:           exportAppName,
		Events:        []store.PortableEvent{event},
	})
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("merge import status = %d body = %s", res.Code, res.Body.String())
	}
	var result store.ImportResult
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatalf("merge result invalid: %v body = %s", err, res.Body.String())
	}
	if result.Inserted != 1 || result.Skipped != 0 || result.Replaced != 0 {
		t.Fatalf("merge result = %+v, want one insert", result)
	}

	req = multipartImportRequest(t, "merge", exportBundle{
		SchemaVersion: exportSchemaVersion,
		ExportedAt:    time.Now().UTC(),
		App:           exportAppName,
		Events:        []store.PortableEvent{event},
	})
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("second merge import status = %d body = %s", res.Code, res.Body.String())
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatalf("second merge result invalid: %v body = %s", err, res.Body.String())
	}
	if result.Inserted != 0 || result.Skipped != 1 {
		t.Fatalf("second merge result = %+v, want duplicate skip", result)
	}

	replacement := store.PortableEvent{
		Timestamp:   time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano),
		Source:      "claude-code",
		Model:       "claude-test",
		Name:        "claude_code.api_request",
		Success:     &success,
		TotalTokens: 250,
	}
	req = multipartImportRequest(t, "replace", exportBundle{
		SchemaVersion: exportSchemaVersion,
		ExportedAt:    time.Now().UTC(),
		App:           exportAppName,
		Events:        []store.PortableEvent{replacement},
	})
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("replace import status = %d body = %s", res.Code, res.Body.String())
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatalf("replace result invalid: %v body = %s", err, res.Body.String())
	}
	if result.Inserted != 1 || result.Replaced != 1 || result.Skipped != 0 {
		t.Fatalf("replace result = %+v, want one old row replaced", result)
	}
	events, err := db.ExportEvents(context.Background(), time.Now().UTC().Add(-time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Source != "claude-code" || events[0].TotalTokens != 250 {
		t.Fatalf("events after replace = %+v, want replacement only", events)
	}
}

func TestAPIImportRejectsInvalidRequests(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mux := http.NewServeMux()
	registerAPI(mux, db)

	valid := exportBundle{
		SchemaVersion: exportSchemaVersion,
		ExportedAt:    time.Now().UTC(),
		App:           exportAppName,
		Events: []store.PortableEvent{{
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
			Source:    "codex",
			Name:      "codex.api_request",
		}},
	}

	t.Run("invalid mode", func(t *testing.T) {
		req := multipartImportRequest(t, "append", valid)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("invalid mode status = %d body = %s", res.Code, res.Body.String())
		}
	})

	t.Run("invalid schema", func(t *testing.T) {
		invalid := valid
		invalid.SchemaVersion = 999
		req := multipartImportRequest(t, "merge", invalid)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("invalid schema status = %d body = %s", res.Code, res.Body.String())
		}
	})

	t.Run("missing file", func(t *testing.T) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("mode", "merge"); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/import", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("missing file status = %d body = %s", res.Code, res.Body.String())
		}
	})

	t.Run("invalid event", func(t *testing.T) {
		invalid := valid
		invalid.Events[0].Source = "other"
		req := multipartImportRequest(t, "merge", invalid)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("invalid event status = %d body = %s", res.Code, res.Body.String())
		}
	})
}

func multipartImportRequest(t *testing.T, mode string, bundle exportBundle) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("mode", mode); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("file", "export.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(file).Encode(bundle); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/import", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}
