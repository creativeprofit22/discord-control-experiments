# Observability guide

Owner: Operations and maintainers
Last reviewed: 2026-08-21

Every lifecycle operation emits JSON logs, an OpenTelemetry span, an operation counter, and a duration histogram with `operation`, `adapter`, `correlation_id`, `result`, `duration_ms`, `retry_count`, `build.version`, and `build.commit`. Correlation IDs use 128 bits from OS randomness when absent.

Resolved secret values initialize one redacting handler. Errors are sanitized before span recording. Baggage is never copied to logs. Valid `traceparent`, bounded `tracestate`, and bounded `baggage` may propagate across MCP, but correlation metadata is never an authorization signal.

Exporters are `none`, stderr JSON, or OTLP/HTTP to explicit HTTPS. Shutdown flushes metrics before traces. Build metadata contains version, commit, runtime, and dependencies but no wall-clock build timestamp.

Troubleshoot with the [startup runbook](../runbooks/startup-failure.md) and field ownership in [module contracts](../architecture/modules.md).
