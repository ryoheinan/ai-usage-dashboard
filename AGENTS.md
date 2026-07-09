# AI Usage Dashboard: Agent Guide

This is a single Go module that receives opt-in OpenTelemetry from Codex CLI and Claude Code, stores derived usage metadata in SQLite, and serves a static dashboard.

## Boundaries

- Do not store or expose prompt/response text, command or tool output, raw OTel payloads, or local tool state.
- Costs are estimates, not billing data.
- Keep the project lightweight; do not add a frontend toolchain or additional Go modules without a clear need.

## Code Changes

- `cmd/ai-usage-dashboard` owns application startup and HTTP API wiring.
- `internal/ingest` normalizes telemetry, `internal/store` owns SQLite schema and queries, and `internal/web` owns dashboard assets.
- Carry new telemetry dimensions through ingest, storage, API filtering, and UI where applicable.
- Preserve existing SQLite data with forward-compatible migrations or a documented migration path.
- Do not modify user telemetry configuration outside this repository; provide opt-in documentation only.

## Testing

- Add or update focused tests for behavior changes, especially telemetry parsing, storage migrations/queries, and API responses.
- Use fixtures containing derived metadata only; never add sensitive telemetry content to tests.
- Do not add tests that only assert static catalogue contents (for example, that a particular model was added) or other implementation details without behavioral value.
- Run focused tests while iterating, then run the full suite before handoff.
