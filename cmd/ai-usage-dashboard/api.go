package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/ryoheinan/ai-usage-dashboard/internal/store"
)

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
