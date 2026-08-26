# Reconciliation recovery

Owner: Operations
Last reviewed: 2026-08-25

1. Stop the affected apply after drift, partial outcome, or journal failure.
2. Preserve snapshot, plan, reviewed hash, journal, audit, and correlation records.
3. Take a fresh snapshot and classify managed/unmanaged conflicts without editing the old plan.
4. Resume only when guild, plan hash, operation IDs, dependencies, and current Discord state still bind.
5. Regenerate and review when drift changes the effect; never bypass stale-snapshot or conflict checks.
6. Confirm dependencies remain ordered and every uncertain create has exactly one adoptable match.

See [reconciliation recovery details](../reconciliation/recovery.md).
