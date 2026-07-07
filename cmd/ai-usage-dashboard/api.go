package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ryoheinan/ai-usage-dashboard/internal/store"
)

const (
	exportAppName       = "ai-usage-dashboard"
	exportSchemaVersion = 1
	maxImportBytes      = 10 << 20
)

type exportBundle struct {
	SchemaVersion int                   `json:"schemaVersion"`
	ExportedAt    time.Time             `json:"exportedAt"`
	App           string                `json:"app"`
	Events        []store.PortableEvent `json:"events"`
}

func registerAPI(mux *http.ServeMux, db *store.DB) {
	mux.HandleFunc("GET /api/summary", func(w http.ResponseWriter, r *http.Request) {
		source := sourceParam(r)
		since, err := sinceParam(r, db, 30, source)
		if err != nil {
			writeJSON(w, nil, err)
			return
		}
		summary, err := db.SummaryBySource(r.Context(), since, source)
		writeJSON(w, summary, err)
	})
	mux.HandleFunc("GET /api/series", func(w http.ResponseWriter, r *http.Request) {
		source := sourceParam(r)
		since, err := seriesSinceParam(r, db, 7, source)
		if err != nil {
			writeJSON(w, nil, err)
			return
		}
		series, err := db.SeriesBySource(r.Context(), since, source)
		writeJSON(w, series, err)
	})
	mux.HandleFunc("GET /api/breakdown/models", func(w http.ResponseWriter, r *http.Request) {
		source := sourceParam(r)
		since, err := sinceParam(r, db, 30, source)
		if err != nil {
			writeJSON(w, nil, err)
			return
		}
		rows, err := db.ModelBreakdownBySource(r.Context(), since, source)
		writeJSON(w, rows, err)
	})
	mux.HandleFunc("GET /api/breakdown/sources", func(w http.ResponseWriter, r *http.Request) {
		source := sourceParam(r)
		since, err := sinceParam(r, db, 30, source)
		if err != nil {
			writeJSON(w, nil, err)
			return
		}
		rows, err := db.SourceBreakdownBySource(r.Context(), since, source)
		writeJSON(w, rows, err)
	})
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		health, err := db.IngestionHealth(r.Context())
		writeJSON(w, health, err)
	})
	mux.HandleFunc("GET /api/export.json", func(w http.ResponseWriter, r *http.Request) {
		source := sourceParam(r)
		since, err := sinceParam(r, db, 30, source)
		if err != nil {
			writeJSON(w, nil, err)
			return
		}
		events, err := db.ExportEvents(r.Context(), since, source)
		if err != nil {
			writeJSON(w, nil, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, exportFilename("json")))
		_ = json.NewEncoder(w).Encode(exportBundle{
			SchemaVersion: exportSchemaVersion,
			ExportedAt:    time.Now().UTC(),
			App:           exportAppName,
			Events:        events,
		})
	})
	mux.HandleFunc("GET /api/export.csv", func(w http.ResponseWriter, r *http.Request) {
		source := sourceParam(r)
		since, err := sinceParam(r, db, 30, source)
		if err != nil {
			writeJSON(w, nil, err)
			return
		}
		events, err := db.ExportEvents(r.Context(), since, source)
		if err != nil {
			writeJSON(w, nil, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, exportFilename("csv")))
		if err := writeEventsCSV(w, events); err != nil {
			writeJSON(w, nil, err)
			return
		}
	})
	mux.HandleFunc("POST /api/import", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
		if err := r.ParseMultipartForm(maxImportBytes); err != nil {
			writeError(w, http.StatusBadRequest, "invalid multipart import")
			return
		}
		mode := store.ImportMode(r.FormValue("mode"))
		if mode != store.ImportModeMerge && mode != store.ImportModeReplace {
			writeError(w, http.StatusBadRequest, "mode must be merge or replace")
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "import file is required")
			return
		}
		defer file.Close()

		var bundle exportBundle
		decoder := json.NewDecoder(file)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&bundle); err != nil {
			writeError(w, http.StatusBadRequest, "invalid import JSON")
			return
		}
		if bundle.SchemaVersion != exportSchemaVersion {
			writeError(w, http.StatusBadRequest, "unsupported import schemaVersion")
			return
		}
		if bundle.App != exportAppName {
			writeError(w, http.StatusBadRequest, "unsupported import app")
			return
		}
		result, err := db.ImportEvents(r.Context(), bundle.Events, mode)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, result, nil)
	})
}

func sinceParam(r *http.Request, db *store.DB, fallbackDays int, source string) (time.Time, error) {
	if r.URL.Query().Get("range") == "all" {
		first, err := db.FirstEventAtBySource(r.Context(), source)
		if err != nil {
			return time.Time{}, err
		}
		if first == nil {
			return startOfUTCDay(time.Now().UTC()), nil
		}
		return *first, nil
	}
	days := intParam(r, "days", fallbackDays)
	return time.Now().UTC().AddDate(0, 0, -days), nil
}

func seriesSinceParam(r *http.Request, db *store.DB, fallbackDays int, source string) (time.Time, error) {
	if r.URL.Query().Get("range") == "all" {
		return sinceParam(r, db, fallbackDays, source)
	}
	days := intParam(r, "days", fallbackDays)
	return startOfUTCDay(time.Now().UTC()).AddDate(0, 0, -(days - 1)), nil
}

func sourceParam(r *http.Request) string {
	return store.NormalizeSource(r.URL.Query().Get("source"))
}

func intParam(r *http.Request, key string, fallback int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 366 {
		return fallback
	}
	return value
}

func startOfUTCDay(t time.Time) time.Time {
	year, month, day := t.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func writeJSON(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func exportFilename(ext string) string {
	return fmt.Sprintf("ai-usage-dashboard-%s.%s", time.Now().UTC().Format("20060102-150405"), ext)
}

func writeEventsCSV(w http.ResponseWriter, events []store.PortableEvent) error {
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{
		"timestamp",
		"source",
		"conversation_id",
		"model",
		"name",
		"kind",
		"success",
		"duration_ms",
		"input_tokens",
		"cached_input_tokens",
		"cache_creation_tokens",
		"output_tokens",
		"reasoning_output_tokens",
		"total_tokens",
		"estimated_cost_usd",
		"dropped_content_fields",
	}); err != nil {
		return err
	}
	for _, event := range events {
		if err := writer.Write([]string{
			event.Timestamp,
			event.Source,
			event.ConversationID,
			event.Model,
			event.Name,
			event.Kind,
			formatBoolPtr(event.Success),
			formatIntPtr(event.DurationMS),
			strconv.FormatInt(event.InputTokens, 10),
			strconv.FormatInt(event.CachedInputTokens, 10),
			strconv.FormatInt(event.CacheCreationTokens, 10),
			strconv.FormatInt(event.OutputTokens, 10),
			strconv.FormatInt(event.ReasoningOutputTokens, 10),
			strconv.FormatInt(event.TotalTokens, 10),
			strconv.FormatFloat(event.EstimatedCostUSD, 'f', -1, 64),
			strconv.Itoa(event.DroppedContentFields),
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func formatBoolPtr(value *bool) string {
	if value == nil {
		return ""
	}
	return strconv.FormatBool(*value)
}

func formatIntPtr(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}
