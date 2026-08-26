# Discord API integration

Owner: Maintainers
Last reviewed: 2026-08-25

The product calls Discord REST v10 through one bounded client. Routes use fixed templates and major-resource rate-limit keys; configured webhooks are validated HTTPS endpoints. Responses, headers, and bodies are untrusted and size bounded.

Mutation flow is policy decision → optional concrete approval → required audit precommit → Discord request → completion audit. Definitely unsent idempotent requests may retry. Uncertain creates never retry blindly; recovery may adopt exactly one matching resource or requires operator review.

Signed interaction webhooks are the only inbound Discord HTTP surface. Requests require direct TLS or a configured trusted proxy, bounded headers/body/concurrency, Ed25519 signature and timestamp verification, strict application identity, deduplication, and fixed callbacks. See [interaction security](../interactions/security.md).

Discord Gateway and identify are unsupported. No listener/client is composed and no Gateway lag or identify-exhaustion promise exists. See [REST details](discord-rest-core.md) and [permission mapping](discord-permissions.md).
