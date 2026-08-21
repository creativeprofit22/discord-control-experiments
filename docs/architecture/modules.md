# Module contracts

Owner: Maintainers
Last reviewed: 2026-08-21

| Package | Owns | Must not own |
|---|---|---|
| `domain` | identifiers, operations, decisions, results | adapters, configuration, telemetry |
| `application` | lifecycle and narrow ports | Discord SDK details |
| `discord` | DiscordGo client and bounded HTTP settings | policy or arbitrary raw-request export |
| `policy` | fail-closed decisions | transport calls |
| `reconcile` | desired/current comparison | transport calls |
| `cli` | strict argv parsing and error returns | process exit |
| `mcp` | stdio, discovery, bounded tools, trace metadata validation | remote identity or arbitrary requests |
| `interactions` | typed interaction dispatch | Gateway consumption |
| `gateway` | Gateway event consumption | webhook substitution |
| `jobs` | context cancellation and concurrency bounds | unbounded goroutines |
| `config` | strict v1 parsing and secret references | Discord behavior |
| `observability` | redacted logs, traces, metrics, correlation | authorization decisions |
| `buildinfo` | deterministic build/runtime dependency facts | wall-clock build time |
| `testsupport` | deterministic fakes/exporters | production composition |

See [architecture direction](index.md) and [configuration contract](../reference/configuration.md).
