# Product overview

Owner: Product and maintainers
Last reviewed: 2026-08-21

Discord Control is a local-first service for explicit, policy-gated Discord administration. It separates commands, desired-state reconciliation, Discord REST/Gateway transports, interactions, jobs, and MCP stdio so later capabilities cannot bypass policy by importing an adapter directly.

The foundation exposes no general Discord request tool and defaults policy decisions to deny. Configuration references an environment secret; it never accepts an inline token. Startup performs no network egress unless an explicit adapter or HTTPS OTLP exporter is enabled.

Read the [architecture](../architecture/index.md), [security model](../security/model.md), and [operator guide](../operations/operator-guide.md).
