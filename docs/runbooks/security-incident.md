# Security incident response

Owner: Security and operators
Last reviewed: 2026-08-21

## Triage

| Alert | First check | Immediate containment |
|---|---|---|
| repeated denial | stable reason, mode, correlation ID, actor configuration | disable the initiating adapter if unexplained |
| cross-guild attempt | resource kind and approved guild in audit | set `read_only`, preserve ownership/audit evidence |
| redaction intervention/failure | exporter and newly resolved secret | stop exporters; rotate potentially exposed credentials |
| approval bypass/mismatch/replay | operation hash, approver, expiry, correlation | invalidate by restart; disable destructive apply |
| policy error/panic | current binary/config revision | set `read_only`; do not bypass the evaluator |
| audit precommit/completion failure | disk, permissions, size, JSONL/head integrity | stop apply traffic; preserve both audit files |
| destructive-operation spike | destructive class and correlated decisions | set `read_only`; revoke bot token if activity is unauthorized |

## Response

1. Contain: stop apply mode and remote/network adapters; revoke exposed bot tokens or webhook URLs; remove broad Discord roles without destroying evidence.
2. Preserve: copy audit JSONL and `.head` read-only, logs, traces, process/version metadata, config with secret references only, Discord audit logs, and relevant host events. Record hashes and access.
3. Scope: correlate decision and completion events; inspect actor/principal mapping, guild/resource ownership, approval issuance, Discord results, and credential activity. Do not place secrets or raw message content in the incident tracker.
4. Recover: correct policy or storage, issue environment-specific credentials, restart, verify read authorization and deliberate denial, then restore the minimum apply capabilities.
5. Review: document source→sink path, blast radius, timeline, notifications required by policy/law, and a regression test. Retain only minimized evidence under the [audit retention policy](../reference/audit-retention.md).

Audit hash chaining detects many local modifications, reordering, and truncation against its head file; it is not an external immutable ledger or cryptographic non-repudiation. A privileged host operator can delete both files. Escalate to external evidence sources when local integrity is uncertain.

See [secret rotation](secret-rotation.md), [approval model](../reference/approval-model.md), and [security test catalog](../testing/security-catalog.md).
