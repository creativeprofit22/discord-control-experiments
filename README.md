# Discord Control Experiments

A local-first, policy-gated Discord control service with REST operations, reconciliation, deterministic automation, MCP stdio, signed interactions, reference-only secrets, and OpenTelemetry-compatible diagnostics.

## Status

The modular Discord control plane, reconciliation workflow, moderation operations, and persisted deterministic automation runtime are implemented. Go 1.27.0 was selected by the committed [runtime benchmark](benchmarks/runtime/results/2026-08-21-windows-amd64.md).

## Build

```powershell
$env:PATH = "$PWD\.tools\go\bin;$env:PATH"
go build ./cmd/discord-control
```

Use [configuration reference](docs/reference/configuration.md), [architecture](docs/architecture/index.md), [operator guide](docs/operations/operator-guide.md), [reconciliation planning](docs/reconciliation/planning.md), and [automation schema](docs/automation/schema.md) for owned contracts.

## Reconciliation

Desired state uses strict `discord-control/v1` JSON and stable logical references. Review the immutable plan hash before apply; deletion requires both desired-state intent and explicit runtime opt-in.

```powershell
discord-control snapshot --config config.json --guild 123 --application 456 --output state/snapshot.json
discord-control plan --config config.json --desired desired.json --output state/plan.json --format human
discord-control drift --config config.json --plan state/plan.json --format human
discord-control apply --config config.json --plan state/plan.json --plan-hash HASH --allow-delete false
```

One-shot CLI policy uses principal `cli/local-process/os-process`; map that exact principal to one configured actor. `resume` without a guild flag requires that actor to have exactly one configured guild. See the [schema](docs/reconciliation/schema-v1.md) and [recovery runbook](docs/reconciliation/recovery.md).

## Deterministic automation

Automation is an optional local scheduler with strict typed workflows, persisted leases/checkpoints, constrained RSS/Atom and GitHub-release sources, reviewed AI summaries, and policy checks per guild. It never executes scripts, arbitrary HTTP, or MCP tools. Start with the [workflow schema](docs/automation/schema.md), [source and AI boundary](docs/automation/sources-and-ai.md), [runtime model](docs/automation/runtime.md), [recipes](docs/automation/recipes.md), and [operations runbook](docs/automation/operations.md).
