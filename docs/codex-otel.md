# Codex OTel Setup

Codex CLI can export structured OpenTelemetry logs when `[otel]` is enabled.

Use this local endpoint with the app:

```toml
[otel]
environment = "dev"
log_user_prompt = false
exporter = { otlp-http = {
  endpoint = "http://localhost:4318/v1/logs",
  protocol = "binary"
}}
```

Keep `log_user_prompt = false` unless you have a separate privacy review. This app does not store prompt or response text, but avoiding prompt export in the first place is the safer default.

This project deliberately avoids Codex internal storage. Do not add importers for `~/.codex/sessions`, `~/.codex/logs_*.sqlite`, or similar files without a new design decision.
