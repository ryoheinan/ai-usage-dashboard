package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryoheinan/ai-usage-analytics/internal/store"
)

func TestAPIEmptyCollectionsEncodeAsArrays(t *testing.T) {
	db, err := store.Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mux := http.NewServeMux()
	registerAPI(mux, db)

	for _, path := range []string{"/api/series", "/api/breakdown/models"} {
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
}
