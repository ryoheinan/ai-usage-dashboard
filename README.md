# Codex Usage Analytics

A local dashboard for Codex CLI usage when Codex is authenticated with an API key.

The app receives opt-in OpenTelemetry logs from Codex CLI, stores analytics metadata in SQLite, and shows token usage plus estimated cost in a small web UI.

## Privacy Model

- Input source is OpenTelemetry only.
- The app does not read Codex internal files, local session JSONL, SQLite logs, or other private Codex state.
- Prompt text, response text, command output, tool output snippets, and raw OTel payloads are not stored.
- Cost is estimated from local token telemetry and the app pricing table. It is not OpenAI billing data.

## Run With Docker

```bash
docker compose up --build
```

Then open `http://localhost:4318`.

## Configure Codex CLI

Add an OTel exporter to your Codex config:

```toml
[otel]
environment = "dev"
log_user_prompt = false
exporter = { otlp-http = {
  endpoint = "http://localhost:4318/v1/logs",
  protocol = "binary"
}}
```

The tool only provides this snippet. It does not modify `~/.codex/config.toml`.

## Local Development

```bash
go run ./cmd/codex-usage-analytics
```

Environment variables:

- `CUA_ADDR`: listen address, default `:4318`
- `CUA_DB`: SQLite path, default `data/codex-usage.sqlite`

## Verification

```bash
go test ./...
go build ./...
docker build .
```
