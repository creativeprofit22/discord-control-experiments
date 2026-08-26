# Stuck workflow

Owner: Operations
Last reviewed: 2026-08-25

1. Pause scheduler pickup for the affected workflow.
2. Join the run, audit, logs, and traces using `correlation_id`.
3. Classify the step as approval-waiting, retryable, uncertain, dead-lettered, or terminal.
4. For uncertain mutation, inspect Discord and adopt only one exact logical match.
5. Resume through the hash-bound workflow command; never edit journals or projections.
6. Confirm scheduler lag, queue depth, leases, and duplicate-mutation count return within budgets.

Use the deterministic drill in [readiness drills](readiness-drills.md).
