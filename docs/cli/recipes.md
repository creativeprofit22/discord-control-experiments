# Operator recipes

## Build server

Use `--non-interactive true --format json` with contained input files. Destructive work requires a valid single-use approval reference or an exact policy-preapproved operation.

## Emergency recovery

Inspect `job get`, confirm the plan-hash prefix, then use `job resume --plan-hash FULL_HASH`. Reconciliation lock, binding, deletion, and approval checks still apply.
