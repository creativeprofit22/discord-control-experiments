# Module contracts

Owner: Maintainers
Last reviewed: 2026-08-21

| Package | Owns | Must not own |
|---|---|---|
| `domain` | sealed Discord commands/results, declarative/snapshot/plan/apply contracts, typed identifiers | adapters, configuration, telemetry, SDK types |
| `application` | lifecycle, secured Discord service, validation, ownership, idempotency, snapshot/plan/drift/apply/resume orchestration, narrow ports | HTTP, Discord SDK details, arbitrary requests |
| `discord` | fixed-origin API-v10 HTTP, routes/codecs, multipart, errors, retries, dynamic rate limits | policy, idempotency, guild ownership, arbitrary raw-request export |
| `policy` | exact principal mapping and pure fail-closed decisions | transport calls, approval storage |
| `approval` | bounded random single-use in-memory grants | adapter/tool exposure, policy decisions |
| `audit` | contained append-only synchronized hash-chained JSONL | update/delete APIs, command content |
| `reconcile` | strict schema/migration, canonical hashes, snapshots, matching, diff, DAG, risk, deterministic rendering | network, clock, filesystem, policy, telemetry |
| `state` | contained atomic bindings/saved plans, synced hash-chained apply journals, exclusive guild locks | tokens, Discord calls, policy decisions |
| `cli` | strict argv parsing and typed command values | shell execution, process exit, Discord calls |
| `mcp` | protocol 2026-07-28 bounded stdio, scoped deterministic inventory, closed schemas, fixed invocation context, form elicitation translation | Discord transport, remote identity, policy authority, raw HTTP/URL/script requests |
| `interactions` | typed interaction dispatch | Gateway consumption |
| `gateway` | Gateway event consumption | webhook substitution |
| `jobs` | context cancellation and concurrency bounds | unbounded goroutines |
| `config` | strict v1 parsing and secret references | Discord behavior |
| `observability` | dynamic redaction, logs/traces/metrics, security alerts, correlation | authorization decisions |
| `buildinfo` | deterministic build/runtime dependency facts | wall-clock build time |
| `testsupport` | deterministic fakes/exporters | production composition |

See [architecture direction](index.md), [Discord REST core](../reference/discord-rest-core.md), [capability policy](../reference/capability-policy.md), and [configuration contract](../reference/configuration.md).
