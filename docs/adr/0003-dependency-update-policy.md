# ADR 0003: Pin and review dependency updates

Owner: Maintainers
Last reviewed: 2026-08-21
Status: Accepted

## Decision

Every direct Go module, benchmark npm package, runtime, package manager, scanner, and GitHub Action is exact. Go content is verified by `go.sum`; npm content by `pnpm-lock.yaml`; portable archives by committed SHA-256; Actions by full 40-character commit. Generated lock/checksum changes are reviewed with source release notes and audit output.

Baseline direct runtime modules are DiscordGo `v0.29.0`, MCP Go SDK `v1.7.0`, and OpenTelemetry Go modules `v1.44.0`. Security floors pin transitive `google.golang.org/grpc v1.82.1` and `golang.org/x/text v0.39.0` after `govulncheck` found reachable issues in older resolver output. Toolchain is Go `1.27.0`. CI tools are Staticcheck `2026.2`, gosec `v2.28.0`, govulncheck `v1.1.4`, gitleaks `v8.30.1`, go-licenses `v2.0.1`, and Syft `v1.51.0`.

Dependabot opens review-only updates; automatic merge is forbidden. New releases normally cool down seven days. A shorter interval requires a concrete compatibility or vulnerability reason in the pull request. Staticcheck `2026.2` is the recorded baseline exception because it adds current Go 1.27 analysis support; hosted CI must still prove it before adoption.

Unknown/custom licenses fail until explicitly reviewed. Accepted baseline licenses are Apache-2.0, BSD-2-Clause, BSD-3-Clause, ISC, MIT, and MPL-2.0. Install scripts stay disabled unless an exact package and reason are allowlisted.

Evidence older than 90 days, or evidence predating a material runtime/SDK change, must be refreshed. Benchmark results and ADRs are updated only through review, never by the manual benchmark workflow.

See [runtime evidence](0001-runtime-selection.md) and [contributor guide](../contributing/contributor-guide.md).
