# Interaction timing and callbacks

Discord requires the initial response within 3 seconds and keeps an interaction token usable for follow-ups for 15 minutes. `ackDeadline` must remain below 3 seconds; the default is 2.5 seconds and metrics measure response commit time, not handler return time.

PING returns PONG synchronously. Autocomplete returns only static bounded choices. I/O-bearing slash commands and modal submissions reserve queue capacity first, then return an ephemeral deferred callback. Cancel/select components either open a modal or use a deferred message update. Policy, state, audit, Discord REST, and callback work runs only in a fixed worker pool under `workTimeout`.

If the request semaphore, dedup store, or queue is full, the endpoint immediately returns a generic ephemeral busy response and does not execute later work. Capacity exhaustion does not evict a live dedup or pending record. Shutdown stops HTTP acceptance first, then cancels/drains bounded workers within the runtime shutdown deadline.

Original-response PATCH is idempotent enough for a bounded retry after a transport, 5xx, or valid short 429 delay. Follow-up POST is not retried after an uncertain send because doing so can duplicate messages. Redirects, malformed/long `Retry-After`, oversized bodies, non-Discord origins, and caller-selected paths/headers are rejected.

Monitor `discord.interaction.ack.duration`, `ack.deadline_miss_count`, `queue.depth`, `handler.active`, `saturation.count`, `deferred.duration`, and `callback.count`. Process-local threshold alerts cover repeated deadline misses, callback failures, signature failures, and saturation; configure durable external alerts over exported metrics because in-process windows reset on restart.
