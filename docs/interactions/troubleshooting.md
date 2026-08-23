# Interaction troubleshooting and incidents

## Endpoint validation fails

Confirm the public URL is HTTPS and ends exactly in `/interactions`, the configured application ID matches the Developer Portal application, the active public key is one of the one/two configured keys, proxy peers are allowlisted, and the proxy sends one `X-Forwarded-Proto: https`. A signed PING must return type 1; malformed JSON cannot be used to diagnose signature failures because verification deliberately runs first.

## Signature-failure spike

Check time synchronization, key-rotation rollout, TLS/proxy routing, and whether failures use the current/previous key slot. Do not log signatures or bodies. If configuration integrity may be compromised, restrict ingress, restore a known configuration, rotate affected bot/TLS secrets, reconcile the Portal public key, and preserve sanitized metrics/audit evidence.

## Callback outage

Initial deferrals can succeed while Discord callback edits fail. Check Discord status, DNS/TLS egress, 429 headers, and `discord.interaction.callback.count`. Restore egress before retrying operator work manually. Original-response PATCH has bounded retries; do not replay uncertain follow-up POSTs.

## Registration drift/startup failure

`/readyz` remains unavailable and HTTP serving does not start until list/semantic comparison/whole-scope overwrite succeeds. Verify the bot token, application ownership, guild ID/scope, Discord permissions, rate limits, and generated catalog. Run generation and restart; never hand-create a second `/discord-control` command to work around drift.

## Queue saturation or deadline misses

Inspect queue depth, handler active, work duration, callback failures, and upstream latency. Reduce incoming retries, restore Discord/state dependencies, or raise limits only within validated bounds and host capacity. Saturated work is rejected before enqueue; it is not waiting invisibly.

## Approval replay or wrong-user attempts

Replays and actor/guild/hash mismatches render the same refusal and cause zero mutation. Inspect bounded approval-outcome metrics and audit correlation, not Discord IDs or modal contents. Restarting intentionally invalidates all pending UI state and grants; ask the authorized operator to repeat review after the incident is contained.

Escalate suspected credential/config compromise through `docs/runbooks/security-incident.md` and rotate secrets using `docs/runbooks/secret-rotation.md`.
