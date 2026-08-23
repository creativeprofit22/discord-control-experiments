# Observability guide

Owner: Operations and maintainers
Last reviewed: 2026-08-21

Every lifecycle operation emits JSON logs, an OpenTelemetry span, an operation counter, and a duration histogram with `operation`, `adapter`, `correlation_id`, `result`, `duration_ms`, `retry_count`, `build.version`, and `build.commit`. Correlation IDs use 128 bits from OS randomness when absent.

Each logical Discord execution emits `discord.command.<name>` with `discord.rest.attempt` children. Discord metrics cover requests, duration, status class, rate-limit wait/scope, retries/reason, cancellation, replay, and partial failure. Allowed dimensions are command name, fixed route template, method, status class, retry reason, rate-limit scope, result class, and API version; IDs, names, content, filenames, audit reasons, query strings, full URLs, and tokens are forbidden.

Secret values register concurrently with one dynamic redactor before each transport use. Logs, nested slog groups, span errors/status, and exporter output are sanitized; an intervention counter and immediate security alert identify defense-in-depth catches. Baggage is never copied to logs. Valid bounded trace context may propagate across MCP, but correlation metadata is never an authorization signal.

Exporters are `none`, stderr JSON, or OTLP/HTTP to explicit HTTPS. Security counters use only decision/reason, approval result, execution mode, destructive class, audit result, and event kind. Approval latency has no approval-ID dimension. Immediate cross-guild, policy, approval, redaction, and audit alerts plus bounded repeated-denial/destructive windows carry only correlation ID and stable enums. Telemetry failures never change authorization; audit failures do.

Interactions add bounded signature outcome/key-slot, acknowledgement type/latency/deadline-miss, duplicate outcome, queue depth, active handler, saturation, deferred outcome/duration, callback outcome, approval outcome, and registration outcome instruments. They never attach Discord snowflakes, names, interaction/custom IDs, tokens, signatures, keys, IPs, request/response content, policy reasons, raw errors, or plan hashes. Repeated signature failures, acknowledgement misses, callback failures, and saturation use sanitized process-local threshold windows; durable external alerts must use exported metrics.

Shutdown flushes metrics before traces. Build metadata contains version, commit, runtime, and dependencies but no wall-clock build timestamp.

See [interaction metrics](metrics.md), the [Discord REST telemetry contract](../reference/discord-rest-core.md), [security model](../security/model.md), and [incident runbook](../runbooks/security-incident.md). Troubleshoot with the [startup runbook](../runbooks/startup-failure.md) and field ownership in [module contracts](../architecture/modules.md).
