# Automation operations and recovery

Owner: Maintainers<br>
Last reviewed: 2026-08-23

## Operator commands

- `workflow list` lists definition hashes, trigger kinds, guild count, and step count.
- `workflow validate --workflow ID` checks schema, limits, configured sources/webhooks, static bindings, cleanup allowlists, and review requirements.
- `workflow run --workflow ID --trigger UNIQUE_ID` starts only a manual workflow.
- `workflow get --run RUN_ID` returns bounded persisted state and provenance.
- `workflow approve --run RUN_ID --operation-hash HASH` issues and consumes a fresh, single-use review grant for the exact rendered output. The approver must be a different policy actor authorized for that guild.
- `workflow retry --run RUN_ID` retries a proven failed run while skipping completed children.
- `workflow recover --run RUN_ID --decision retry|skip --evidence TEXT` resolves an uncertain dispatch only with operator evidence; evidence is stored as a hash.
- `workflow dead-letter list` lists terminal dead letters. Uncertain mutations require evidence and are not automatically replayed.

## Monitoring and alerts

Monitor `workflow.events` by bounded `workflow.event` and `workflow.outcome`; monitor `workflow.schedule.lag` in milliseconds. Expected event classes cover queue depth, schedule due/lag, lease contention, retries, dead letters, source failures, deduplication, publication, approval waits, and partial failures. Alert on sustained schedule lag, any dead letter, recurring source failure, lease contention growth, approval backlog, and partial/uncertain mutation outcomes.

Workflow spans contain workflow/run/guild/step IDs, attempt, definition hash, source item ID, event, and outcome. They intentionally exclude source text, rendered text, model text, webhook URLs, configured source URLs, request bodies, headers, and credentials.

## Troubleshooting

1. Run `health local`; an enabled scheduler must report automation `ok`.
2. Run `workflow validate` and fix the first schema or binding error.
3. Inspect `workflow get`; compare state, attempt, next attempt, lease expiry, child results, and operation hash.
4. Check source DNS/redirect/content-type/body-limit errors without weakening the egress policy.
5. Treat unmatched `dispatching` as uncertain. Inspect Discord and audit records; never blindly repeat it.
6. Verify the journal before editing projections. Interior corruption requires restore or bounded projection rebuild, not replay.

## Backup and restore

Stop all local lifecycles, then back up the complete configured state directory while preserving permissions. Definitions, schedules, runs, journals, and dead letters must remain together. Restore to a private local filesystem, verify journal chains and projection hashes, then start one scheduler. A restored waiting approval still requires a fresh approval. A restored uncertain run remains uncertain.

Dead-letter recovery starts from `workflow get` and audit/provenance evidence. Fix policy, source, or Discord prerequisites first. Retry only proven-unsent effects. For an effect confirmed present, record an operator skip/reconciliation decision rather than sending again. There are no automatic compensation scripts.

## Explicit non-goals

No Gateway events, cron, user-selected URLs, arbitrary HTTP, scripts, shell, expressions, dynamic imports, DAG editor, MCP invocation, distributed/network-filesystem execution, blind uncertain replay, or unreviewed AI publication is supported.
