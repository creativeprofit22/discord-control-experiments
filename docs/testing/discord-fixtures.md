# Discord scripted fixtures

Owner: Maintainers
Last reviewed: 2026-08-21

`internal/testsupport.ScriptedDiscord` is the only REST fixture server. It accepts an ordered queue of `DiscordStep` values and exposes a redirecting `RoundTripper`; production code still constructs the fixed `https://discord.com/api/v10` URL.

Each step can assert method, request URI, selected headers, and a bounded body predicate. It can return status, headers, a body, delayed handling, chunked bodies, or a hijacked forced EOF. Captures store method, URI, redacted headers, body length, and SHA-256 only. Authorization, cookies, and audit reasons are always replaced with `[REDACTED]`; attachment bytes and message content are never retained in diagnostics.

Tests must close the fixture and assert all three outcomes: no fixture failures, zero pending steps, and the expected bounded request count. Rejected validation and ownership cases assert zero requests. Never print request bodies when an assertion fails; compare decoded safe fields, hashes, and lengths.

Fixture suites cover route and query encoding, auth and audit headers, JSON/multipart shape, 2xx models, mapped 4xx fields, malformed and oversized JSON, slow bodies, forced EOF, 5xx exhaustion, 429 scope/reset behavior, cancellation, invalid-token circuit behavior, and concurrent bucket/idempotency operations. Multipart predicates verify part count, normalized filenames, metadata, and declared lengths without retaining file bytes.

Fuzz seeds live in package `testdata/fuzz` corpora and run during normal `go test`. Bounded local fuzzing targets command validation and rate-limit header parsing; do not point fuzz traffic at Discord.

See the [REST core reference](../reference/discord-rest-core.md) and [testing strategy](strategy.md).
