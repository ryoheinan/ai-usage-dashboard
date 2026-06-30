package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/ryoheinan/ai-usage-analytics/internal/store"
)

func registerAPI(mux *http.ServeMux, db *store.DB) {
	mux.HandleFunc("GET /api/summary", func(w http.ResponseWriter, r *http.Request) {
		days := intParam(r, "days", 30)
		summary, err := db.Summary(r.Context(), time.Now().UTC().AddDate(0, 0, -days))
		writeJSON(w, summary, err)
	})
	mux.HandleFunc("GET /api/series", func(w http.ResponseWriter, r *http.Request) {
		days := intParam(r, "days", 30)
		series, err := db.Series(r.Context(), time.Now().UTC().AddDate(0, 0, -days))
		writeJSON(w, series, err)
	})
	mux.HandleFunc("GET /api/breakdown/models", func(w http.ResponseWriter, r *http.Request) {
		days := intParam(r, "days", 30)
		rows, err := db.ModelBreakdown(r.Context(), time.Now().UTC().AddDate(0, 0, -days))
		writeJSON(w, rows, err)
	})
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		health, err := db.IngestionHealth(r.Context())
		writeJSON(w, health, err)
	})
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

func writeJSON(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}
