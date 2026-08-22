# Architecture

Owner: Maintainers
Last reviewed: 2026-08-21

The dependency direction is `cmd` → adapters/application → domain. Domain imports only the standard library. Application defines narrow ports. Discord, MCP, Gateway, interactions, and jobs are peer adapters and never import each other. Configuration, observability, and build metadata contain no Discord rules.

`cmd/discord-control` is the only composition root. Policy denies unless an exact principal, guild, capability, mode, destructive class, and resource scope allow an operation. Application owns the single dispatch chokepoint and ports; `policy`, `approval`, and `audit` implement pure decisions, single-use grants, and append-only durability without importing adapters. Reconciliation snapshots and every apply mutation inherit the authenticated principal and re-enter the chokepoint.

The Discord execution core is split deliberately: application owns sealed capabilities, validation, guild ownership, idempotency, plan binding, apply/resume, and policy orchestration; the Discord adapter owns fixed API-v10 HTTP, codecs, dynamic rate limits, retries, and bounded network I/O. `reconcile` owns only pure schema/canonical/snapshot/match/diff/DAG/risk/render logic. `state` owns contained bindings, immutable saved plans, hash-chained journals, and guild locks.

The deterministic workflow is documented in [schema v1](../reconciliation/schema-v1.md), [snapshot semantics](../reconciliation/snapshots.md), [planning](../reconciliation/planning.md), and [recovery](../reconciliation/recovery.md).

See [module contracts](modules.md), [capability policy](../reference/capability-policy.md), [control-surface ADR](../adr/0002-control-surface-boundaries.md), [Discord REST ADR](../adr/0004-discord-rest-transport.md), [REST core reference](../reference/discord-rest-core.md), and [testing strategy](../testing/strategy.md).
