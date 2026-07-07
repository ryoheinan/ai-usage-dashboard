# AI Usage Dashboard

A local dashboard for AI coding tool usage.

The app receives opt-in OpenTelemetry from Codex CLI and Claude Code, stores analytics metadata in SQLite, and shows token usage plus estimated cost in a small web UI.

## Privacy Model

- Input source is OpenTelemetry only.
- The app does not read Codex or Claude Code internal files, local session JSONL, SQLite logs, or other private tool state.
- Prompt text, response text, command output, tool output snippets, and raw OTel payloads are not stored.
- JSON and CSV exports include only the derived telemetry metadata stored in SQLite. They do not include prompt text, response text, command output, tool output snippets, or raw OTel payloads.
- Cost is estimated from local token telemetry, telemetry-provided cost fields, and the app pricing table. It is not provider billing data.
- The pricing table is maintained by hand from [OpenAI API pricing](https://developers.openai.com/api/docs/pricing) and [Anthropic API pricing](https://platform.claude.com/docs/en/about-claude/pricing), and may become stale when prices or model names change. Claude Code telemetry cost fields are used when present, but displayed costs are still estimates rather than billing data.

## Run With Docker

```bash
docker compose up -d --build
```

Then open `http://localhost:4318`.

### Local Dockerfile

```bash
docker compose -f docker-compose.yml -f docker-compose.local.yml up --build -d
```

## Configure Codex CLI

See also [docs/codex-otel.md](docs/codex-otel.md).

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

## Data Portability

The dashboard can export the currently selected range and source from the web UI.

- Export JSON for backup, migration, and later import into another local dashboard database.
- Export CSV for spreadsheet review. CSV files are export-only and cannot be imported.
- Import JSON with `merge` to add missing events while skipping exact duplicates.
- Import JSON with `replace` to delete all existing events and restore the uploaded export file.

Import/export files contain only the derived telemetry metadata described in the privacy model. Use `replace` only when you intend to overwrite the current local database contents.

## Configure Claude Code

See also [docs/claude-code-otel.md](docs/claude-code-otel.md).

Configure Claude Code to export OTel metrics and logs over HTTP/protobuf:

```bash
export CLAUDE_CODE_ENABLE_TELEMETRY=1
export OTEL_METRICS_EXPORTER=otlp
export OTEL_LOGS_EXPORTER=otlp
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://localhost:4318/v1/metrics
export OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://localhost:4318/v1/logs
```

Keep prompt, assistant response, raw API body, and tool-content logging disabled unless you have a separate privacy review.

## Local Development

```bash
go run ./cmd/ai-usage-dashboard
```

Environment variables:

- `AUD_ADDR`: listen address, default `:4318`
- `AUD_DB`: SQLite path, default `data/ai-usage-dashboard.sqlite`
- `AUD_DEBUG_OTEL_KEYS`: when set, logs selected OTel field keys for diagnostics without storing raw payloads

## Verification

```bash
go test ./...
go build ./...
docker build .
```

## License

Licensed under [Apache License 2.0](LICENSE).
