# Observability guide

Owner: Operations and maintainers
Last reviewed: 2026-08-21

Every lifecycle operation emits JSON logs, an OpenTelemetry span, an operation counter, and a duration histogram. Logs and traces carry `correlation_id`; logs inside spans also carry `trace_id` and `span_id`. Metrics use only bounded operation, adapter, and result values and never identifiers. Correlation IDs use 128 bits from OS randomness when absent.

Each logical Discord execution emits `discord.command.<name>` with `discord.rest.attempt` children. Discord metrics cover requests, duration, status class, rate-limit wait/scope, retries/reason, cancellation, replay, and partial failure. Allowed dimensions are command name, fixed route template, method, status class, retry reason, rate-limit scope, result class, and API version; IDs, names, content, filenames, audit reasons, query strings, full URLs, and tokens are forbidden.

Secret values register concurrently with one dynamic redactor before each transport use. Logs, nested slog groups, span errors/status, and exporter output are sanitized; an intervention counter and immediate security alert identify defense-in-depth catches. Baggage is never copied to logs. Valid bounded trace context may propagate across MCP, but correlation metadata is never an authorization signal.

Exporters are `none`, stderr JSON, or OTLP/HTTP to explicit HTTPS. Parent-based ratio sampling defaults to 10%; exporter success/failure and sampling drops are measured. Security counters use only decision/reason, approval result, execution mode, destructive class, audit result, and event kind. Approval latency has no approval-ID dimension. Immediate cross-guild, policy, approval, redaction, and audit alerts plus bounded repeated-denial/destructive windows carry only correlation ID and stable enums. Telemetry failures never change authorization; audit failures do.

Interactions add bounded signature, acknowledgement, duplicate, queue, callback, approval, and registration instruments. They never attach Discord IDs, names, tokens, signatures, request content, raw errors, or plan hashes.

Automation adds `workflow.events` and `workflow.schedule.lag`. Bounded events cover queue, lag, lease contention, retry, dead letter, source failure, deduplication, publication, approval, and partial failure. Traces retain workflow/run/guild/step IDs and provenance hashes/item IDs, never source/rendered/model content, URLs, headers, or credentials. See [automation operations](../automation/operations.md).

Shutdown flushes metrics before traces. Build metadata contains version, commit, runtime, and dependencies but no wall-clock build timestamp.

See [service levels and alert policy](slos.md), [interaction metrics](metrics.md), the [Discord REST telemetry contract](../reference/discord-rest-core.md), [security model](../security/model.md), and [incident runbook](../runbooks/security-incident.md). Troubleshoot with the [startup runbook](../runbooks/startup-failure.md) and field ownership in [module contracts](../architecture/modules.md).
