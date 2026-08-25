# Automation runtime and state machine

Owner: Maintainers<br>
Last reviewed: 2026-08-23

The local scheduler is an opt-in lifecycle. `run` may start automation alone, with MCP, with interactions, or with both. Automation is not an MCP tool bus. Enabling it requires the `automation/local-scheduler` principal to map to one configured policy actor.

## Persistent projections

`<state>/automation` contains immutable definitions, revisioned schedules, revisioned runs, JSONL journals, dead letters, and `transitions.lock`. Schedules persist definition hash, next run, revision, and source/member cursor. Runs persist state, attempt, next attempt, lease owner/token/expiry, idempotency key, completed steps, per-guild children, bounded errors, provenance, and approval hash/content.

One context-aware OS file lock serializes compare-and-swap transitions. The operating system releases it after process death. This is a single-host, local-filesystem design: do not run it on network filesystems or multiple hosts. Move to a transactional distributed store and coordinated Discord rate limits first.

## States

`queued → leased → running` is the normal path. Running may become `waiting_approval`, `retry_waiting`, `paused`, `succeeded`, `failed`, or `uncertain`. Exhausted work may enter `dead_letter`. Approval, due retry, lease reclaim, or explicit operator evidence may return allowed nonterminal states to `queued`; terminal success never reopens.

Workers claim expiring leases; only the matching owner and token may heartbeat. Expired leases may be reclaimed. Schedules and runs use revision compare-and-swap, so concurrent workers cannot both advance a projection. Run and step IDs are deterministic; duplicate enqueue returns the original run.

## Dispatch and recovery semantics

A hash-chained `dispatching` record is synced before each Discord mutation. Completion is then synced before the run projection advances. Replay verifies sequence, run/definition binding, previous hash, and record hash. It tolerates only a crash-truncated final record. Interior corruption, gaps, malformed complete records, or binding mismatches stop.

A final unmatched `dispatching` record means the effect is `uncertain`; automation never repeats it automatically. Definitely-unsent retryable work uses deterministic capped exponential backoff. Completed children and steps are skipped on resume. Every guild receives a fresh policy invocation and independent result. Total and destructive caps pause rather than truncate remaining scope.

News cursors advance only after all item publications complete. Member scans establish a no-send baseline, then checkpoint per-guild snowflake cursors after durable completion. The runtime intentionally provides no compensation scripts: Discord side effects are often irreversible or ambiguous.
