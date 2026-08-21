# Architecture

Owner: Maintainers
Last reviewed: 2026-08-21

The dependency direction is `cmd` → adapters/application → domain. Domain imports only the standard library. Application defines narrow ports. Discord, MCP, Gateway, interactions, and jobs are peer adapters and never import each other. Configuration, observability, and build metadata contain no Discord rules.

`cmd/discord-control` is the only composition root. Policy denies unless an explicit reviewed rule allows an operation; reconciliation compares state without transport I/O. The architecture test runs fixed `go list -json ./...` argv and enforces these constraints.

See [module contracts](modules.md), [control-surface ADR](../adr/0002-control-surface-boundaries.md), and [testing strategy](../testing/strategy.md).
