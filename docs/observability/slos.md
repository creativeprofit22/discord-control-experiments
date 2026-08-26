# Service levels, ownership, and alert policy

Owner: Operations and maintainers
Last reviewed: 2026-08-25

These objectives apply to supported local CLI, MCP stdio, signed interactions, automation, reconciliation, and Discord REST. They are release-readiness targets, not guarantees. Discord Gateway and identify are unsupported: no listener/client is composed, no Gateway SLO exists, and dashboards must report the capability as `unsupported` rather than unhealthy.

## Objectives

| Signal | Objective | Window | Page when |
| --- | --- | --- | --- |
| Process availability | 99.9% successful local health probes | 30 days | error budget burns rapidly for 15 minutes |
| CLI and MCP | 99% complete below 2 seconds, excluding Discord mutation latency | 7 days | error ratio exceeds 5% for 15 minutes |
| Signed interaction acknowledgement | 99.9% below 2.5 seconds | 7 days | any deadline miss repeats within 5 minutes |
| Discord REST | 99% non-rate-limited requests below 2 seconds | 7 days | 5xx/error ratio exceeds 10% for 15 minutes |
| Scheduler | 99% of due work begins within 60 seconds | 7 days | lag exceeds 5 minutes for 15 minutes |
| Reconciliation | 99% of accepted runs finish without partial outcome | 30 days | partial outcomes repeat within 15 minutes |
| Audit durability | 100% required precommit and completion records persist | 30 days | any write or chain failure occurs |

Security denials, destructive actions, lockdowns, drift, dead letters, source failures, redaction interventions, and exporter failures are operational indicators, not availability failures. Their alerts use bounded thresholds and links to a runbook. Symptom alerts page; cause/detail alerts create tickets to prevent duplicate paging.

## Cardinality contract

Metrics may use only registered operation/tool names, fixed route templates, HTTP method/status classes, stable result/retry/risk/resource/event enums, and build support state. Each attribute has at most 128 values; each instrument has at most 2,000 active series. Unknown values map to `other` or are dropped.

Metrics never contain actor, guild, channel, workflow, run, resource, correlation, plan, URL, content, secret, or free-form error values. `correlation_id` joins audits, logs, and traces. Logs inside recorded spans also include `trace_id` and `span_id`. Traces sample parent-first at the configured ratio; security and audit records are never sampled.

## Retention and ownership

Keep metrics for 30 days, sampled traces for 7 days, application logs for 14 days, and audits for the configured policy period (30 days by default). Shorter local retention is acceptable when exported records meet these windows. Operations owns dashboards, paging routes, retention, and exporter credentials. Maintainers own signal schemas and runbook links. Security owns denial/destructive/redaction thresholds.

Review objectives and budgets quarterly, after a Discord API behavior change, or after an incident. Changes require measured evidence; never widen a threshold solely to pass a failing release.
