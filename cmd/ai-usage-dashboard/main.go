package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ryoheinan/ai-usage-dashboard/internal/ingest"
	"github.com/ryoheinan/ai-usage-dashboard/internal/pricing"
	"github.com/ryoheinan/ai-usage-dashboard/internal/store"
	"github.com/ryoheinan/ai-usage-dashboard/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		addr   = flag.String("addr", envOrDefault("CUA_ADDR", ":4318"), "HTTP listen address")
		dbPath = flag.String("db", envOrDefault("CUA_DB", "data/ai-usage-dashboard.sqlite"), "SQLite database path")
	)
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	staticFS, err := web.Files()
	if err != nil {
		return fmt.Errorf("load embedded assets: %w", err)
	}

	mux := http.NewServeMux()
	handler := ingest.NewHandler(db, pricing.DefaultCatalog())
	handler.Register(mux)
	registerAPI(mux, db)
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	server := &http.Server{
		Addr:              *addr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", *addr, "db", *dbPath)
		errs <- server.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds())
	})
}
