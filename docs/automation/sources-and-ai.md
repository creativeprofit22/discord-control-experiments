# Automation sources and AI boundary

Owner: Maintainers<br>
Last reviewed: 2026-08-23

## Configured sources

Workflows select a source name only. Administrators configure either an HTTPS RSS/Atom URL or a GitHub owner/repository. GitHub requests use the fixed `api.github.com/repos/{owner}/{repo}/releases` route. Source methods, paths, headers, credentials, and redirect targets are not workflow inputs.

The shared transport disables environment proxies, requires TLS 1.2+, resolves before dialing, and rejects every mixed or blocked DNS answer. Loopback, private, link-local, multicast, unspecified, carrier-grade NAT, benchmark, reserved, and documentation ranges are blocked. TLS keeps the configured server name. RSS permits at most two same-host HTTPS redirects; GitHub and summarization disable redirects.

Connect, TLS, header, and overall deadlines are bounded. Responses require allowlisted XML/JSON content types and configured size limits. Parsers cap items and reject malformed feeds. Requests preserve `ETag` and `Last-Modified`; stable item hashes deduplicate up to the bounded cursor history. The next cursor is persisted only after every publication or explicit skip completes.

## Rendering hostile content

Feed, API, and model text is untrusted. Rendering strips markup and controls, normalizes whitespace, limits bytes, removes URL fragments, preserves canonical HTTPS links and publication timestamps, and inserts a zero-width break after `@`. Discord payloads additionally set empty `allowed_mentions`; source text therefore cannot ping users, roles, or everyone.

## Optional summaries

The summarizer has a dedicated environment secret and fixed OpenAI Responses endpoint. Requests use the configured fixed model, fixed instruction, labeled untrusted input, JSON Schema output, `store:false`, no conversation, and an empty tools list. Discord and MCP services are not dependencies and Discord credentials never enter the request.

The adapter validates the bounded response and plain summary. Provenance retains workflow hash, source item ID, canonical link, timestamp, and model. Every summary action must set `requireReview:true`; the rendered text and operation hash are persisted before waiting. Restart discards any in-memory approval. An operator must review that exact hash and provide a fresh approval before dispatch.

Do not narrow review until representative prompt-injection, factuality, omission, and mention-suppression evaluations establish an acceptable documented threshold. Model output is never authority for routes, actions, targets, policy, approvals, or credentials.
