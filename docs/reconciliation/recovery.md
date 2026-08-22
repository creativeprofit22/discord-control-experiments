# Apply recovery runbook

Owner: Maintainers<br>
Last reviewed: 2026-08-21

Only one apply/resume process may hold a guild lock. A second process exits before mutation. Each run stores its immutable plan and an append-only JSONL journal beneath the configured state directory.

## Journal state machine

1. `run_started` is synced before the first mutation.
2. `step_started` is hash-chained and synced before dispatch.
3. Discord commands pass policy, approval, audit, idempotency, ownership, retry, and telemetry controls.
4. `step_completed` records only operation ID, logical reference, remote ID, and fingerprint after response ownership validation.
5. A failure records `step_failed` as failed or uncertain; a completed run ends with `run_completed`.

Each record has a monotonic sequence, previous-record hash, and canonical record hash. Replay rejects earlier corruption, hash mismatch, sequence gaps, wrong guild, and wrong plan. Only a final crash-truncated record is ignored. Records are bounded and flushed with file sync.

## Resume decisions

- Completed steps are skipped and their bindings reconstructed from journal events.
- A create that was started but not confirmed is discovered by logical kind plus typed name/fingerprint rules. Exactly one unmanaged candidate is bound; zero or multiple candidates stop for operator review. It is never blindly repeated.
- A definitely unsent failed create may retry.
- An already-absent bound delete is marked complete.
- Started update/move/reorder work retries only if its remote ID and fingerprint still satisfy the stored precondition.
- Any material drift, ambiguous candidate, invalid journal chain, plan/hash mismatch, or cross-guild result stops without further mutation.

## Operator procedure

1. Run `drift` with the reviewed plan and retain its hash.
2. If apply returns a `run_id` with partial/uncertain state, do not manually repeat creates.
3. Inspect Discord for duplicate or externally changed resources; resolve ambiguity explicitly.
4. Run `resume --run RUN_ID --plan-hash HASH` with the same authenticated actor and any required approval.
5. If resume reports a conflict, create a fresh snapshot and plan rather than editing the saved plan or journal.

State files contain no tokens, webhook URLs, approval IDs, names/topics/descriptions, or response bodies. Back up bindings and journals together; never merge guild directories.
