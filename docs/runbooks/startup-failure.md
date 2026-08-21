# Startup failure

Owner: Operations
Last reviewed: 2026-08-21

1. Capture stderr JSON and the process exit status; never capture environment values.
2. Validate the local file against the [configuration reference](../reference/configuration.md): UTF-8, version 1, exact fields, allowlisted environment reference.
3. Confirm `DISCORD_BOT_TOKEN` exists and is non-empty without printing it. If exposure is suspected, rotate it before retrying.
4. For MCP failures, ensure stdout contains only newline-delimited JSON-RPC, the client sends `server/discover` for `2026-07-28`, and EOF reaches the process on shutdown.
5. For OTLP failures, temporarily select `none` to isolate startup; review the explicit HTTPS endpoint and network policy before restoring export.

Use `correlation_id`, `operation`, `adapter`, `result`, and build version/commit to join diagnostics. Escalate with redacted records and the exact build commit. See the [observability guide](../observability/guide.md).
