# Discord Control Experiments

A local-first, policy-gated Discord control service with MCP stdio, explicit Gateway and interaction boundaries, strict reference-only secrets, and OpenTelemetry-compatible diagnostics.

## Status

The modular foundation is implemented; product control capabilities are intentionally not exposed yet. Go 1.27.0 was selected by the committed [runtime benchmark](benchmarks/runtime/results/2026-08-21-windows-amd64.md).

## Build

```powershell
$env:PATH = "$PWD\.tools\go\bin;$env:PATH"
go build ./cmd/discord-control
```

Use [configuration reference](docs/reference/configuration.md), [architecture](docs/architecture/index.md), and [operator guide](docs/operations/operator-guide.md) for owned contracts.
