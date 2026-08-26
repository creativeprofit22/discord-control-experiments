# Production-readiness threat review

Owner: Security and maintainers
Last reviewed: 2026-08-25

Date: 2026-08-25
Scope: argv/stdin/config/environment/repository files, MCP stdio, signed interactions, feeds/model output, Discord responses, filesystem/network/audit/telemetry/release sinks
Not reviewed: live Discord or OTLP deployments, host permissions, GitHub organization controls, operator endpoints, or third-party systems

This is a source-to-sink code review and test record, not certification.

## Exposure summary

The product is a local Go control plane plus an opt-in public signed-interaction HTTPS endpoint. MCP remains local stdio. Discord, configured feeds, and model responses are untrusted network data. Local operators control configuration, state, backup archives, and release tags. The primary assets are Discord credentials and mutation authority, audit/state integrity, webhook URLs, and release artifacts.

## Threat model

- An internet caller sends malformed or replayed interaction bodies seeking unauthorized Discord mutations.
- A feed or model supplies hostile content seeking filesystem, network, policy, or tool execution.
- A local user supplies a hostile backup/config/state file seeking writes outside staging or corrupted recovery.
- A dependency or workflow change seeks CI credentials or replacement of shipped artifacts.

## Sources

| Source | Location | Controller |
| --- | --- | --- |
| CLI argv, JSON stdin, confirmation input | `internal/cli/command.go`, `internal/cli/input.go` | local operator/process |
| MCP JSON-RPC and tool arguments | `internal/mcp/server.go`, `internal/mcp/schema.go` | local stdio client/model |
| Interaction headers/body | `internal/interactions/server.go:134` | anonymous internet |
| Configuration, workflows, desired state, archives | `internal/config/config.go`, `internal/domain/automation.go`, `internal/state/archive.go` | local operator or supplied file |
| RSS/GitHub/model responses | `internal/jobs/sources.go`, `internal/jobs/summarizer.go` | remote service/content author |
| Discord responses and rate-limit headers | `internal/discord/client.go` | Discord/network path |
| Tag, source tree, dependencies, Actions | `.github/workflows/release.yml`, `tools/release/main.go` | maintainers/build system/supply chain |

## Sinks and assets

| Sink | Location | Protected asset |
| --- | --- | --- |
| Discord REST/callback egress | `internal/discord/client.go`, `internal/interactions/callback.go` | bot authority, webhook tokens, guild state |
| Policy/approval/mutation dispatch | `internal/application/service.go`, `internal/policy` | authorization and destructive actions |
| Audit and state writes | `internal/audit/jsonl.go`, `internal/state` | forensic and recovery integrity |
| Backup extraction/file replacement | `internal/state/archive.go` | host files and staged configuration |
| OTLP/log output | `internal/observability` | secrets, content, identifiers |
| Build subprocesses/release publication | `tools/release/main.go`, `.github/workflows/release.yml` | downstream artifact integrity and CI token |

## Existing controls

- Configuration defaults deny mutation; principals map to policy actors; concrete destructive effects require hash-bound approval.
- Interaction requests require Ed25519 verification, bounded timestamp skew/body/headers, TLS or trusted proxy, bounded concurrency, and deduplication.
- MCP is stdio-only, schema-bound, timeout/concurrency bounded, policy checked, and audit joined by correlation.
- Feed egress requires fixed HTTPS origins, public resolved IPs, same-origin bounded redirects, response limits, and strict parsing.
- State journals and audits are hash chained, sequential, bounded, synced, and fail closed on interior corruption.
- Telemetry uses registered-secret redaction, bounded dimensions, identifier-free metrics, sampling-independent audits, and exporter-failure containment.
- CI actions use immutable commits, tokens are job-scoped, tools are checksum pinned, and packaging rebuilds each target.

## Risk matrix after remediation

| Severity | Open | Resolved in this phase |
| --- | ---: | ---: |
| Critical | 0 | 0 |
| High | 0 | 1 |
| Medium | 0 | 2 |

## Resolved findings

### BP-001 — Windows archive path traversal — High

- Location: `internal/state/archive.go`, `validArchivePath`
- Category: path/file operations; CWE-22; confidence 0.95; evidence: CODE and RUNTIME
- Source → sink: supplied ZIP entry with Windows separators/drive syntax → `filepath.FromSlash`/`os.Rename` during restore.
- Impact: a restore on Windows could write an archive entry outside the new staging directory with the operator's permissions.
- Fix: reject backslash, drive-colon, NUL, absolute, traversal, link, and duplicate names before any staging write; recheck containment at extraction.
- Verification: `go test ./internal/state` covers slash, Windows separator, drive, link, duplicate, and digest cases.

### BP-002 — Aggregate ZIP expansion was not bounded — Medium

- Location: `internal/state/archive.go`, `VerifyBackup`
- Category: exceptional conditions; CWE-409; confidence 0.90; evidence: CODE
- Source → sink: compressed archive → repeated bounded-per-entry decompression.
- Impact: many individually valid entries could consume excessive memory/disk during operator-requested verification/restore.
- Fix: reject archives whose aggregate declared uncompressed bytes exceed the global archive budget before reading entries.

### BP-003 — Upgrade validator accepted malformed trailing JSON — Medium

- Location: `internal/state/validate.go`, `validateStateJSON`
- Category: data integrity; CWE-20; confidence 0.90; evidence: CODE and RUNTIME
- Source → sink: staged state JSON with a valid first value and malformed suffix → upgrade acceptance.
- Impact: an operator could select a staging tree that later fails strict state loading.
- Fix: require the second decoder read to return EOF; any trailing or second value now fails validation.
- Verification: `go test ./internal/state` includes malformed trailing data.

## Candidates dropped

Seven candidates were disproved: interaction replay reaches bounded deduplication; feed SSRF reaches public-IP and redirect controls; model output reaches only validated Discord content, not tools/shell; MCP has no listener and is policy checked; telemetry secrets reach dynamic redaction; tag metadata reaches argv and a strict allowlist, not shell interpolation; health endpoints expose only fixed capability status. Test fixture credentials were recognized as non-live.

## Review result and remaining boundary

No reachable critical/high finding remains in reviewed code after the fixes above. Automated scans and this review cover only known classes. Live credentials, Discord behavior, OTLP collector policy, runner images, repository release immutability, branch protection, and attestation verification require operator/platform evidence before release. Gateway and remote MCP remain unsupported and were not implemented.
