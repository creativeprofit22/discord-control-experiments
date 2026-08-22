# Security test catalog

Owner: Security and maintainers
Last reviewed: 2026-08-21

No security test uses a live Discord token or remote network. A counting `DiscordPort`, scripted `RoundTripper`, in-memory OpenTelemetry exporters, injected clocks, temporary audit files, and canary values make zero-network and non-disclosure assertions observable.

| Contract | Required cases |
|---|---|
| registry | all 52 sealed commands have capability, exposure, destructive class, resource extractor, and sanitizer; discovery order is deterministic |
| principal/policy | exact mapping; unknown/malformed/ambiguous principal; forged actor; adapter substitution; guild/capability/mode/class truth tables; wildcard/duplicate rejection; evaluator error and panic |
| ownership | channel, role, member/message, webhook, invite, application-command, and scheduled-event cross-guild substitution; unknown unqualified owner |
| modes | read-only discovery excludes every mutation and stale runtime mutation denies; dry-run has zero transport, attempts, baseline, reconciliation, idempotency, and approval consumption |
| destructive approvals | deletes, bulk delete, ban, kick, role escalation/administrator, permission overwrite, webhook/invite creation, bulk application-command overwrite, and lockdown classification |
| approval store | actor/hash binding, opaque ID, maximum TTL, exact expiry boundary, mismatch, replay/stale/unknown, restart invalidation, concurrent single winner, storage failure |
| audit | decision precommit precedes network; completion for success/error/cancellation/uncertain/reconciliation; restart and concurrent append; restrictive path; malformed tail; tamper/reorder/truncation; bounded sanitized intent; sink failure blocks network |
| secrets | bot and webhook canaries absent from config values, nested formatting, JSON/text, errors, snapshots, diagnostics, logs, nested slog groups, traces, status, metrics, audit JSONL, and crash stderr; mutable provider rotation needs no client reconstruction |
| alerts | immediate cross-guild, redaction, approval, policy, and audit events; repeated-denial/destructive windows, suppression, reset, injected clock; approval latency without ID dimension |
| architecture/docs | domain imports no project package; application imports only domain; peer adapters do not import each other; required documents have owners, review dates, and valid links |

Every authorization substitution test must assert the transport call count is zero. Policy, approval, and audit failures must remain fail-closed under race testing. Fuzz targets cover bounded config decoding/duplicate keys, policy identity/grants, and audit record parsing/hash validation without filesystem escape.

Run focused criterion commands first, then `go test -race` for security packages, bounded fuzzing, and `bash scripts/verify.sh`. The broad script adds formatting, module integrity, vet/staticcheck, gosec, govulncheck, full-history/current-tree secret scans, licenses, SBOM, and reproducible builds. Passing scanners does not establish absence of defects.

See [testing strategy](strategy.md), [Discord fixtures](discord-fixtures.md), and [incident response](../runbooks/security-incident.md).
