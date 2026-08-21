# Testing strategy

Owner: Maintainers
Last reviewed: 2026-08-21

- **Unit/table tests:** strict config acceptance and each fail-closed parser/secret path.
- **Architecture test:** fixed `go list -json ./...` argv plus compiler cycle enforcement.
- **Smoke test:** real application lifecycle, fake Discord/MCP, retry, cancellation, in-memory spans/metrics, JSON logs, build fields, shared correlation ID, and canary absence.
- **Race test:** `go test -race ./...` on supported Linux CI and local hosts where supported.
- **Supply chain:** module verification, static/security/vulnerability analysis, history secret scan, license allowlist, SBOM, and duplicate deterministic builds.
- **Benchmark:** committed Windows raw samples; not part of routine unit tests.

Tests use no Discord token and no remote network. Hosted CI is required before phase review; workflow presence alone is not execution evidence.

See [observability guide](../observability/guide.md) and [benchmark method](../../benchmarks/runtime/README.md).
