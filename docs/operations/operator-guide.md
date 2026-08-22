# Operator guide

Owner: Operations
Last reviewed: 2026-08-21

1. Build with Go 1.27.0 and `go build ./cmd/discord-control`.
2. Copy [example config](../../config/example.v1.json) to an ignored local path; do not edit in a token.
3. Set environment-backed bot/webhook references and grant only the [capability permission union](../reference/discord-permissions.md); never default to Discord `ADMINISTRATOR`.
4. Keep policy deny-all/read-only until exact `mcp/local-stdio/os-process` principals, guilds, capabilities, and ceilings are reviewed. Ensure the contained audit directory is writable and access-restricted.
5. Start with `discord-control run --config PATH`; keep MCP stdio stdout connected only to the local MCP client. No approval issuance tool exists, so destructive apply remains blocked until a separate authenticated human adapter is deployed.

Default observability exporter `none` makes no telemetry network request. Use `stderr` locally or a reviewed explicit HTTPS OTLP endpoint. Monitor policy, approval, redaction, audit, and destructive-spike warnings. Cancel the process for bounded reverse-order shutdown and audit close. Rotate a credential immediately if it appears outside the environment secret store; deleting a log is not rotation.

For failures follow the [startup runbook](../runbooks/startup-failure.md), [secret rotation](../runbooks/secret-rotation.md), or [incident response](../runbooks/security-incident.md). Configuration fields are in the [reference](../reference/configuration.md).
