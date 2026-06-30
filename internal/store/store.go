package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	db *sql.DB
}

type Event struct {
	Timestamp             time.Time
	ConversationID        string
	Model                 string
	Name                  string
	Kind                  string
	Success               *bool
	DurationMS            *int64
	InputTokens           int64
	CachedInputTokens     int64
	OutputTokens          int64
	ReasoningOutputTokens int64
	TotalTokens           int64
	EstimatedCostUSD      float64
	DroppedContentFields  int
}

type Summary struct {
	Events                int64   `json:"events"`
	Requests              int64   `json:"requests"`
	Failures              int64   `json:"failures"`
	AvgDurationMS         float64 `json:"avgDurationMs"`
	InputTokens           int64   `json:"inputTokens"`
	CachedInputTokens     int64   `json:"cachedInputTokens"`
	OutputTokens          int64   `json:"outputTokens"`
	ReasoningOutputTokens int64   `json:"reasoningOutputTokens"`
	TotalTokens           int64   `json:"totalTokens"`
	EstimatedCostUSD      float64 `json:"estimatedCostUsd"`
}

type SeriesPoint struct {
	Bucket                string  `json:"bucket"`
	Events                int64   `json:"events"`
	Requests              int64   `json:"requests"`
	Failures              int64   `json:"failures"`
	InputTokens           int64   `json:"inputTokens"`
	CachedInputTokens     int64   `json:"cachedInputTokens"`
	OutputTokens          int64   `json:"outputTokens"`
	ReasoningOutputTokens int64   `json:"reasoningOutputTokens"`
	TotalTokens           int64   `json:"totalTokens"`
	EstimatedCostUSD      float64 `json:"estimatedCostUsd"`
}

type BreakdownRow struct {
	Model            string  `json:"model"`
	Events           int64   `json:"events"`
	TotalTokens      int64   `json:"totalTokens"`
	EstimatedCostUSD float64 `json:"estimatedCostUsd"`
}

type Health struct {
	LastEventAt          *time.Time `json:"lastEventAt"`
	AcceptedEvents       int64      `json:"acceptedEvents"`
	DroppedContentFields int64      `json:"droppedContentFields"`
}

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	wrapped := &DB{db: db}
	if err := wrapped.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return wrapped, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS telemetry_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	ts TEXT NOT NULL,
	conversation_id TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL,
	kind TEXT NOT NULL DEFAULT '',
	success INTEGER,
	duration_ms INTEGER,
	input_tokens INTEGER NOT NULL DEFAULT 0,
	cached_input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_output_tokens INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	estimated_cost_usd REAL NOT NULL DEFAULT 0,
	dropped_content_fields INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_telemetry_events_ts ON telemetry_events(ts);
CREATE INDEX IF NOT EXISTS idx_telemetry_events_model ON telemetry_events(model);
CREATE INDEX IF NOT EXISTS idx_telemetry_events_name ON telemetry_events(name);
`)
	if err != nil {
		return fmt.Errorf("migrate sqlite: %w", err)
	}
	return nil
}

func (d *DB) InsertEvents(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO telemetry_events (
	ts, conversation_id, model, name, kind, success, duration_ms,
	input_tokens, cached_input_tokens, output_tokens, reasoning_output_tokens, total_tokens,
	estimated_cost_usd, dropped_content_fields
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, event := range events {
		var success any
		if event.Success != nil {
			if *event.Success {
				success = 1
			} else {
				success = 0
			}
		}
		var duration any
		if event.DurationMS != nil {
			duration = *event.DurationMS
		}
		_, err := stmt.ExecContext(ctx,
			event.Timestamp.UTC().Format(time.RFC3339Nano),
			event.ConversationID,
			event.Model,
			event.Name,
			event.Kind,
			success,
			duration,
			event.InputTokens,
			event.CachedInputTokens,
			event.OutputTokens,
			event.ReasoningOutputTokens,
			event.TotalTokens,
			event.EstimatedCostUSD,
			event.DroppedContentFields,
		)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) Summary(ctx context.Context, since time.Time) (Summary, error) {
	var out Summary
	err := d.db.QueryRowContext(ctx, `
SELECT
	COUNT(*),
	COALESCE(SUM(CASE WHEN name = 'codex.api_request' THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END), 0),
	COALESCE(AVG(duration_ms), 0),
	COALESCE(SUM(input_tokens), 0),
	COALESCE(SUM(cached_input_tokens), 0),
	COALESCE(SUM(output_tokens), 0),
	COALESCE(SUM(reasoning_output_tokens), 0),
	COALESCE(SUM(total_tokens), 0),
	COALESCE(SUM(estimated_cost_usd), 0)
FROM telemetry_events
WHERE ts >= ?`, since.UTC().Format(time.RFC3339Nano)).Scan(
		&out.Events, &out.Requests, &out.Failures, &out.AvgDurationMS,
		&out.InputTokens, &out.CachedInputTokens, &out.OutputTokens,
		&out.ReasoningOutputTokens, &out.TotalTokens, &out.EstimatedCostUSD,
	)
	return out, err
}

func (d *DB) Series(ctx context.Context, since time.Time) ([]SeriesPoint, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT
	strftime('%Y-%m-%d', ts),
	COUNT(*),
	COALESCE(SUM(CASE WHEN name = 'codex.api_request' THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(input_tokens), 0),
	COALESCE(SUM(cached_input_tokens), 0),
	COALESCE(SUM(output_tokens), 0),
	COALESCE(SUM(reasoning_output_tokens), 0),
	COALESCE(SUM(total_tokens), 0),
	COALESCE(SUM(estimated_cost_usd), 0)
FROM telemetry_events
WHERE ts >= ?
GROUP BY 1
ORDER BY 1`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SeriesPoint
	for rows.Next() {
		var point SeriesPoint
		if err := rows.Scan(
			&point.Bucket, &point.Events, &point.Requests, &point.Failures,
			&point.InputTokens, &point.CachedInputTokens, &point.OutputTokens,
			&point.ReasoningOutputTokens, &point.TotalTokens, &point.EstimatedCostUSD,
		); err != nil {
			return nil, err
		}
		out = append(out, point)
	}
	return out, rows.Err()
}

func (d *DB) ModelBreakdown(ctx context.Context, since time.Time) ([]BreakdownRow, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT COALESCE(NULLIF(model, ''), 'unknown'), COUNT(*), COALESCE(SUM(total_tokens), 0), COALESCE(SUM(estimated_cost_usd), 0)
FROM telemetry_events
WHERE ts >= ?
GROUP BY 1
ORDER BY 4 DESC, 3 DESC`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BreakdownRow
	for rows.Next() {
		var row BreakdownRow
		if err := rows.Scan(&row.Model, &row.Events, &row.TotalTokens, &row.EstimatedCostUSD); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (d *DB) IngestionHealth(ctx context.Context) (Health, error) {
	var out Health
	var last sql.NullString
	err := d.db.QueryRowContext(ctx, `
SELECT MAX(ts), COUNT(*), COALESCE(SUM(dropped_content_fields), 0)
FROM telemetry_events`).Scan(&last, &out.AcceptedEvents, &out.DroppedContentFields)
	if err != nil {
		return out, err
	}
	if last.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, last.String)
		if err != nil {
			return out, err
		}
		out.LastEventAt = &parsed
	}
	return out, nil
}

func (d *DB) Count(ctx context.Context) (int64, error) {
	var count int64
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM telemetry_events`).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return count, err
}
