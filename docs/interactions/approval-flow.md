# Interaction approval flow

## Cancel

`/discord-control jobs cancel` authorizes and loads a job in the signed guild, then edits the deferred response with a single-use confirmation button. The button `custom_id` contains only `cancel.<random nonce>`. Another user, guild, expired nonce, or replay receives the generic refusal. A valid click atomically consumes the pending state and queues cancellation.

## Resume

1. `/discord-control jobs resume` authorizes and reloads a recoverable durable job in the signed guild.
2. The ephemeral review shows the run ID and 12-character plan-hash prefix. Its button contains only `resume_review.<random nonce>`.
3. A valid button atomically consumes review state and opens a modal. The modal ID is `resume_modal.<random nonce>`; actor, guild, run, full hash, and expiry remain process memory only.
4. The caller types the displayed 12-character prefix. Submission checks the same Discord user/guild and constant-time prefix equality, atomically consumes modal state, and defers work.
5. Deferred work reloads the durable job and rechecks policy actor, guild, recoverability, and full 64-character plan hash. It issues a `reconciliation_plan` grant whose expiry is capped by both approval TTL and pending interaction expiry.
6. Resume consumes that grant once before Discord mutation. A private application context permits only operation IDs belonging to the canonical plan; unrelated operations still need their own operation grant.

Changed jobs, wrong actors/guilds/hashes, expired state, and replay cause zero Discord mutation and expose no sensitive distinction. Approval IDs never enter Discord messages or component IDs.

Both approval grants and interaction UI state are process-local. Restart invalidates every pending button/modal and approval; durable reconciliation jobs remain and must be reviewed again. Callback tokens remain usable by Discord for approximately 15 minutes, but this implementation caps pending approval state below that lifetime.
