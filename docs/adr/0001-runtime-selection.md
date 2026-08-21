# ADR 0001: Select Go for the production runtime

Owner: Maintainers  
Last reviewed: 2026-08-21  
Status: Accepted

## Context

The production root needed a stable, lean runtime before product behavior landed. Go and TypeScript were benchmarked through official Discord and MCP libraries with identical loopback data and the predeclared rule in [the benchmark README](../../benchmarks/runtime/README.md). The committed [raw JSON](../../benchmarks/runtime/results/2026-08-21-windows-amd64.json) is the measurement record; its source revision is `4d25fa65f0483ba9342d66851b4eddc80da1c0a1`.

Sources were retrieved on 2026-08-21. Mutable “latest” labels were not used.

| Evidence | Exact version or revision | Integrity / immutable identity |
|---|---|---|
| Go toolchain | 1.27.0 | official Windows archive SHA-256 `f0c0a0d33ba94f4d2c5dbc887334ce678b21813504ddb3aafcb06e60a5a667c4`; Go tag commit `8af21751f066eced273ca3ce49506b366847c623` |
| Node LTS candidate | 24.19.0 | official Windows archive SHA-256 `57f71ab3652e797d84acddc79c81cc9ff1c6ddb3a1974cdb83f00fee9bff4c73` |
| pnpm | 10.26.1 | registry integrity `sha512-ZkB0q8Nn0skyT9wYA3CXzgqPEmA0Fg9wmSjp6fldmHFDRwROXDFk1lvV2mxZxr42KxB1Rikqjuy3mZGW5c5Y+g==` |
| DiscordGo | v0.29.0 | Go checksum `h1:FmWeXFaKUwrcL3Cx65c20bTRW+vOb6k8AnaP+EgjDno=`; tag commit `6e8fa27c7917ea54d8b9ec26f126becae59058d2` |
| MCP Go SDK | v1.7.0 | Go checksum `h1:yqjY2dsbKAC0LSuWZVBMrHgiG8ukXv6NRo0JiALay44=`; proxy origin commit `bc72835f62eb94d0fb484439f886b6885b075f36` |
| discord.js | 14.27.0 | registry integrity `sha512-qHbFlFG2N7y3LjPySYsL6A1+BnX6bkTVgo842EX0CqVPk/KTMwZkojPHEXKsQUpWZNyz5BISNHK1cPpQw0+m4A==` |
| MCP TypeScript server | 2.0.0 | registry integrity `sha512-YhHWdHfpFMQfd0prsEnxKeS3Qz3ytIGmsS0sth4KDjnacIT7hxk6hXHkJ9KysxlkvTM+WZAtQbbcUhdoP4Hvtw==` |
| TypeScript compiler | 6.0.3 | registry integrity `sha512-y2TvuxSZPDyQakkFRPZHKFm+KKVqIisdg9/CZwm9ftvKXLP8NRWj38/ODjNbr43SsoXqNuAisEf1GdCxqWcdBw==` |

The official MCP SDK list classified Go and TypeScript as Tier 1. The MCP Go SDK’s attached source revision `b0408f2821d7a686b6cccff52c81539a4315b229` and released v1.7.0 require Go 1.25 or newer. The TypeScript repository revision `3924de99df834302d89f5997a1b64ca268282284` was a private workspace root marked `2.0.0-alpha.0`; the candidate therefore used the independently published stable package `@modelcontextprotocol/server@2.0.0`, not mutable source. TypeScript 7.0.2 was only one day old, so aged stable 6.0.3 was used.

MCP protocol `2026-07-28` requires newline-delimited JSON-RPC over stdio, stdout reserved for protocol output, mandatory `server/discover`, and EOF shutdown. Both candidates passed 50 discovery starts. Discord’s official docs say Gateway supplies general realtime events, webhook events are limited and not ordered, and REST rate limits must be learned from responses rather than hard-coded.

## Decision

Select **Go 1.27.0**.

| Candidate | Eligible | Lean score | Peak RSS | Deployable | Cold median / p95 | MCP median / p95 | REST median / p95 | Build median |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| Go | yes | 1.061063 | 14.29 MiB | 9.24 MiB | 6.694 / 8.031 ms | 7.250 / 8.443 ms | 0.000 / 0.548 ms | 19,534.439 ms |
| TypeScript | yes | 62.661411 | 116.48 MiB | 130.04 MiB | 849.230 / 1,074.761 ms | 1,048.071 / 1,346.250 ms | 15.248 / 16.130 ms | 12,128.477 ms |

For each dimension, candidate value was divided by the best eligible value. The weighted sum used memory 25%, deployable size 20%, cold composite 20%, MCP composite 15%, REST composite 10%, and build median 10%; composites are 70% median plus 30% p95. The scores were not within 5%, so the tie-break was not applied.

The Go REST median fell below this Windows host clock’s observable granularity; the p95 and every raw sample remain committed. This limitation strengthens the case for rerunning after material client or host changes, but it does not change the decision because memory, artifact, and startup dimensions independently favor Go.

## Consequences

Production uses one Go module and a single-binary operational boundary. Direct runtime dependencies are pinned exactly and transitive module content is checksum-verified by `go.sum`. The TypeScript candidate and lockfile remain only as reproducible evidence.

The decision is host-specific, not a general language ranking. A rerun is required after material SDK, runtime, deployment-host, or workload changes; reviewed evidence is never replaced automatically.

## Rejected alternatives

- **TypeScript production runtime:** stronger Discord REST typing and faster measured clean builds, but substantially larger memory, deployable size, and startup cost under the predeclared lean score.
- **Mutable SDK branches or the private alpha workspace root:** not stable or checksum-pinned production inputs.
- **TypeScript 7.0.2:** insufficient adoption cooldown at the retrieval date without a demonstrated compatibility need.
