# ADR 0004 — Discord REST transport

Owner: Maintainers
Last reviewed: 2026-08-21
Status: Accepted

## Context

The DiscordGo v0.29.0 REST session exposed an SDK session and could be used with arbitrary methods and paths. Its limiter keys static routes, cannot cancel sleeps, and does not remap provisional routes from `X-RateLimit-Bucket` or model `X-RateLimit-Scope`. Those properties do not meet the typed capability, cancellation, and dynamic-rate-limit requirements.

## Decision

Use a small `net/http` API-v10 transport for REST. Keep DiscordGo as the selected future Gateway runtime only; it is not a REST dependency.

Production requests always target `https://discord.com/api/v10`. Only a `RoundTripper` is injectable, so tests can redirect that origin but application input cannot select a host. The client clones the default transport, retains TLS verification, applies bot authorization and a versioned DiscordBot user agent, bounds bodies, streams multipart, and never returns raw response bytes.

The application owns the sealed command registry, validation, guild ownership, idempotency, reconciliation state, and partial outcomes. The transport owns route resolution, query and payload codecs, audit headers, HTTP errors, retries, dynamic bucket queues, and the invalid-token circuit. Adapters receive `DiscordService`, never the client, bot token, URL, method, path, or raw response.

Rate-limit state begins at `method + route template + major resource`. A returned bucket hash remaps it to `bucket hash + major resource`; guild, channel, and webhook majors remain isolated. Every response updates limit, remaining, reset, and scope. `Reset-After` is preferred; `Reset - Date` handles clock skew. A 429 requires a valid decimal-second or HTTP-date `Retry-After`; global scope advances the process-wide authenticated deadline. No Discord quota is hard-coded.

Safe reads and idempotent mutations may retry bounded network and selected 5xx failures. Unsafe creates do not repeat after an uncertain send; application reconciliation or operator resume resolves them. All waits and backoff are context-cancellable.

## Consequences

The capability surface is finite and compile-time associated with result types. New endpoints require registry and conformance work. Dynamic limits remain process-local, so initial deployment is single-process; distributed coordination requires a new ADR. Discord API drift is checked through scripted contract fixtures and scheduled compatibility review.

## References

- [REST core reference](../reference/discord-rest-core.md)
- [Fixture guide](../testing/discord-fixtures.md)
- [Discord rate limits](https://docs.discord.com/developers/topics/rate-limits)
- [Discord API reference](https://docs.discord.com/developers/reference)
