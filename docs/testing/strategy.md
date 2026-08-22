# Testing strategy

Owner: Maintainers
Last reviewed: 2026-08-21

- **Unit/table tests:** strict config; all 52 registry security descriptors; exact policy truth tables; cross-guild substitution; mode/approval sequencing; append-only audit integrity; dynamic secret redaction/rotation; alert windows; routes/codecs/rate limits/retries.
- **Architecture test:** fixed `go list -json ./...` argv plus compiler cycle enforcement.
- **Smoke test:** real application lifecycle, fake Discord/MCP, retry, cancellation, in-memory spans/metrics, JSON logs, build fields, shared correlation ID, and canary absence.
- **Race test:** `go test -race ./...` on supported Linux CI and local hosts where supported.
- **Supply chain:** module verification, static/security/vulnerability analysis, history secret scan, license allowlist, SBOM, and duplicate deterministic builds.
- **Benchmark:** committed Windows raw samples; not part of routine unit tests.

Tests use no live Discord token and no remote network. The bounded [scripted Discord fixtures](discord-fixtures.md) redirect the fixed API origin through an injected transport and retain only redacted headers plus body hashes/lengths. Hosted CI is required before phase review; workflow presence alone is not execution evidence.

Every denied authorization, stale/replayed approval, and failed audit precommit asserts zero transport calls. See the complete [security catalog](security-catalog.md), [Discord REST core](../reference/discord-rest-core.md), [observability guide](../observability/guide.md), and [benchmark method](../../benchmarks/runtime/README.md).
