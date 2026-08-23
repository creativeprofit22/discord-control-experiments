# Moderation and lockdown runbook

Owner: Maintainers  
Last reviewed: 2026-08-23

## Action failure

1. Keep the correlation ID, case ID, action kind, and stable result.
2. `retryable_failure` means Discord definitely did not receive the mutation; retry with the same idempotency key.
3. `operator_review` means delivery is uncertain. Inspect Discord and the audit log before any new action.
4. Resolve hierarchy failures by changing Discord role order or scope; never bypass the check.

## AutoMod drift

Capture a fresh snapshot and plan. Review name/trigger adoption conflicts, reference bindings, and create-disabled/enable ordering. A trigger-type change is replacement and deletion approval applies. For uncertain create, adopt exactly one normalized match or stop for operator review.

## Lockdown partial failure

Do not manually continue pending operations. Compare Discord overwrites with the plan journal. If each completed operation equals `after` and each pending operation equals `before`, resume the same plan. Any other drift requires a new reviewed plan or deliberate manual remediation. Recover only with the paired recovery hash and fresh approval.

## Journal problem

Stop mutations for that guild. Preserve the state directory and `.previous` generation. Replay tolerates only an unterminated final crash record; gaps, hash mismatches, malformed interior records, and cross-guild data require investigation. Restore the complete state directory from a drill-tested copy. No repository-defined off-host backup, RPO, or RTO exists.

## Routine maintenance

Run retention compaction under normal service controls, monitor destructive spikes and lockdown partial alerts, export appeal data only through its dedicated capability, and repeat a scoped lockdown/recovery drill after permission or protected-role changes.
