# Troubleshooting

Owner: Operations
Last reviewed: 2026-08-25

Start with `health local --format json`, then create `diagnostics bundle --output diagnostics.zip`. Bundles are bounded and sanitized, but review them before sharing. Join audits, logs, and traces with `correlation_id`; logs inside spans also carry `trace_id` and `span_id`.

| Symptom | Action |
| --- | --- |
| Startup or readiness failure | Follow [startup failure](../runbooks/startup-failure.md). |
| Discord 429/5xx or timeout pressure | Follow [Discord outage](../runbooks/discord-outage.md); never replay uncertain creates. |
| Scheduler lag or dead letters | Follow [stuck workflow](../runbooks/stuck-workflow.md). |
| Drift or partial reconciliation | Follow [reconciliation recovery](../runbooks/reconciliation-recovery.md). |
| Audit failure or unexplained denial | Follow [audit investigation](../runbooks/audit-investigation.md). |
| Redaction intervention or destructive spike | Follow [security incident](../runbooks/security-incident.md). |
| Gateway/identify requested | Report capability as unsupported; do not add tokens or listeners. |

Exporter failures never change authorization or mutation results. Preserve local audit/state evidence while restoring the collector separately.
