# Interaction metrics

| Instrument | Bounded attributes |
|---|---|
| `discord.interaction.signature.count` | outcome; current/previous key slot or none |
| `discord.interaction.ack.duration` | interaction type |
| `discord.interaction.ack.deadline_miss_count` | interaction type |
| `discord.interaction.duplicate.count` | cached, in-flight, digest mismatch |
| `discord.interaction.queue.depth` | none |
| `discord.interaction.handler.active` | none |
| `discord.interaction.saturation.count` | none |
| `discord.interaction.deferred.duration` | ok, unavailable, error |
| `discord.interaction.callback.count` | success, failure |
| `discord.interaction.callback.duration` | operation (`edit_original`, `followup`); HTTP status class (`1xx`–`5xx`, `transport_error`) |
| `discord.interaction.approval.count` | issued, accepted, denied, expired, replayed, wrong actor/guild, changed plan |
| `discord.interaction.registration.count` | no-op, updated, failed |

Callback duration is measured in milliseconds for every outbound HTTP attempt, so retried original-response edits contribute one observation per attempt; follow-up posts remain single-attempt to avoid duplicate messages. No interaction token, URL, concrete status code, or response data is attached.

Alert externally on sustained invalid/stale signatures, any acknowledgement deadline misses, callback-failure rate or latency, and saturation/queue depth. The in-process window monitor is a fast local signal only and resets on restart.
