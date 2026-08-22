# Configuration reference

Owner: Maintainers
Last reviewed: 2026-08-21

Configuration is UTF-8 JSON capped at 65,536 bytes and validated before startup against [schema v1](../../config/schema/v1.schema.json). Unknown fields, duplicate keys, trailing JSON, missing fields, unsupported versions, duplicate grants, wildcards, invalid modes/capabilities/durations/thresholds, and escaping audit paths fail closed.

Use [the committed example](../../config/example.v1.json). Bot tokens and optional named webhook URLs use `{"source":"env","name":"EXACT_NAME"}` references. `DISCORD_BOT_TOKEN` is built-in; webhook environment names must appear exactly in `secretEnvironmentAllowlist`. Inline values, files, commands, arbitrary names, and remote secret URLs are unsupported. Missing or empty values stop startup. Resolved config retains handles, not plaintext; handles format, log, and serialize as `[REDACTED]`.

`policy.mode` is the operator ceiling: `read_only`, `dry_run`, or `apply`. Actors require stable ID, one or more exact principals, explicit numeric guild IDs, explicit capabilities, and a mode no higher than the ceiling. No actors means deny all. Older v1 files without security sections receive deny-all/read-only, 15-minute approval TTL, contained `audit/events.jsonl`, bounded retention, and alert defaults.

`approvals.maxTtl` is positive and at most 24 hours. `audit.path` and `reconciliation.stateDirectory` are relative to the configuration directory and traversal is rejected. Reconciliation defaults are `state`, 100 operations, 10 destructive operations, and 30 retention days; configured limits are bounded and destructive count cannot exceed total count. `securityAlerts` configures positive repeated-denial and destructive-operation thresholds with one-second to 24-hour windows.

`mcp.enabled: true` requires one numeric `guildId`. `mode` is `read_only`, `dry_run`, or `apply`; `toolsets` contains unique values from `build`, `moderation`, `news`, and `diagnostic`; `readOnly` independently strips mutations. Defaults are diagnostic-only/read-only, 1 MiB messages, four concurrent calls, and a 30-second call timeout. Bounds are 4 KiB–8 MiB, 1–32 calls, and 1 second–5 minutes. See [MCP setup](../mcp/setup.md).

`observability.exporter` is `none` (default local no-egress behavior), `stderr`, or `otlp-http`. OTLP requires explicit HTTPS without credentials, query, or fragment. Diagnostics expose only config version, enabled adapters, and exporter name.

One-shot commands authenticate as `cli/local-process/os-process`; configure that exact principal with explicit guilds and reconciliation capabilities. Snapshot/apply may require `guild.inspect`, channel/role/webhook/application-command reads and management, plus `onboarding.read`/`onboarding.manage`. See [capability policy](capability-policy.md), [approval model](approval-model.md), [audit retention](audit-retention.md), [reconciliation planning](../reconciliation/planning.md), [operator guide](../operations/operator-guide.md), and [startup runbook](../runbooks/startup-failure.md).
