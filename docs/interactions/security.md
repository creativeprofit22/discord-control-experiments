# Interaction security model

The endpoint treats every network byte as hostile. It checks the exact method/path, TLS or trusted-proxy state, content type, header bounds, and a nonblocking request semaphore before reading a bounded body. It then verifies Discord's Ed25519 signature over the literal `X-Signature-Timestamp` bytes followed by the untouched body. JSON decoding, identity extraction, deduplication, policy, disk, Discord REST, and callbacks occur only after verification.

Absent, malformed, stale, and invalid signatures produce the same 401 body. Signed requests require a bounded timestamp skew and an interaction-ID/body-SHA-256 claim. The process rejects an ID with another digest, never evicts a live claim to admit a new one, and never repeats deferred work for a duplicate. Deduplication is process-local: restart clears delivery records, so timestamp checks and actor/guild/plan binding remain required.

Identity comes only from the signed envelope: `interactions/<member.user.id>/discord-ed25519`, the signed guild, and the configured application ID. Callers cannot supply an actor, guild, route, URL, token, arbitrary operation name, desired state, or JSON command payload. Job authorization and guild ownership are rechecked in the application layer. Unauthorized, wrong-guild, expired, replayed, malformed-state, changed-plan, and missing-resource cases render one generic ephemeral refusal.

Callback tokens exist only in bounded worker closures. They are never persisted, audited, logged, traced, or returned. Callback egress is fixed to Discord API v10 original-response and follow-up paths, rejects redirects, has body/response/deadline caps, retries idempotent original edits only, and does not retry uncertain follow-up POSTs.

## Signing-key rotation

1. Add the new public key as the second `signingPublicKeys` entry and deploy.
2. Change the Developer Portal signing key only after every instance accepts both keys.
3. Watch verification outcomes by key slot and signature-failure alerts through the propagation window.
4. Roll back the Portal key if valid traffic cannot verify; both configured keys preserve the rollback window.
5. Remove the old key only after all Discord deliveries use the new slot and old instances are gone.

A signing public key is not secret. A bot token or TLS private key is secret: if either may have leaked, contain access, rotate it at the source, redeploy environment references, and inspect audit/Discord activity. Do not paste production tokens, private keys, signatures, request bodies, or interaction callback tokens into tickets or command history.
