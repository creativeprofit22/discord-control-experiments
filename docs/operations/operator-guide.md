# Operator guide

Owner: Operations
Last reviewed: 2026-08-21

1. Build with Go 1.27.0 and `go build ./cmd/discord-control`.
2. Copy [example config](../../config/example.v1.json) to an ignored local path; do not edit in a token.
3. Set `DISCORD_BOT_TOKEN` in the process environment with the minimum bot permissions needed.
4. Start with `discord-control run --config PATH`; keep MCP stdio stdout connected only to the MCP client and route diagnostics from stderr.
5. Cancel the process context for bounded reverse-order shutdown.

Default observability exporter `none` makes no telemetry network request. Use `stderr` locally or a reviewed explicit HTTPS OTLP endpoint. Rotate the token immediately if it appears outside the environment secret store; deleting a log is not rotation.

For failures follow the [startup runbook](../runbooks/startup-failure.md). Configuration fields are in the [reference](../reference/configuration.md).
