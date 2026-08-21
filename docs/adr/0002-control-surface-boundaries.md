# ADR 0002: Bound every Discord and MCP control surface

Owner: Maintainers
Last reviewed: 2026-08-21
Status: Accepted

## Decision

Use Discord bot accounts only. Every capability enters through a small typed application port, is evaluated by fail-closed policy, and reaches a bounded adapter method. MCP remains local stdio in this phase, reserves stdout for JSON-RPC, sends diagnostics to stderr, supports `server/discover`, and treats EOF as graceful shutdown. The registry is capped and capability-oriented.

REST operations, Gateway events, HTTP interactions, webhook events, and jobs remain separate boundaries. Rate limits are response-driven and never hard-coded. Validated W3C trace metadata is correlation data only—never authorization input; baggage is not logged.

## Rejected approaches

- **Self-bots:** Discord user-account automation violates the intended app boundary and expands account risk.
- **One tool per Discord endpoint:** a giant catalog creates excessive model agency, review load, and unstable coupling.
- **Arbitrary Discord request tools:** caller-controlled methods, URLs, or payloads bypass capability policy and egress bounds.
- **Webhook events as a general Gateway replacement:** Discord documents webhook events as limited, one-way, not realtime, and not ordered; Gateway is the general event channel.
- **Remote MCP now:** no deployment identity, client authorization, tenant boundary, or network policy has been approved. Remote transport remains disabled until those requirements have their own ADR and tests.

See [module contracts](../architecture/modules.md) and [security model](../security/model.md).
