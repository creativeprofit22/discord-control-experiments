# Planning, drift, and apply

Owner: Maintainers<br>
Last reviewed: 2026-08-21

The workflow is **snapshot → plan → review hash → drift/apply → resume if partial**. Parsing, normalization, matching, diff, risk classification, and DAG construction are pure and deterministic.

## Diff and conflict rules

- A name change updates in place when logical binding and Discord type are unchanged.
- A Discord type change is a replacement and requires destructive approval.
- Parent/position changes emit moves plus deterministic reorder batches.
- Permission overwrites compare canonical role IDs and decimal allow/deny values.
- Roles/categories are prepared before channels; resources before overwrites, webhooks, and onboarding; reorder batches follow creates/updates; child/webhook deletes precede parent deletes.
- Unmanaged same-name/type resources conflict unless unique adoption was explicitly enabled.
- Unsupported desired fields, ambiguous matches, stale fingerprints, and protected deletes are conflicts.

Managed orphan deletion requires all of: recorded ownership, desired `delete_orphans`, no applicable deletion protection, runtime `--allow-delete true`, operation caps, policy authorization, and destructive approval. `@everyone`, Discord-managed roles, protected channels/categories, application-owned webhooks, and onboarding dependencies fail closed.

## Risks and immutable hash

Operations are classified `read_only`, `additive`, `mutating`, `permission_sensitive`, `replacement`, or `destructive`. Permission-sensitive, replacement, and destructive commands require the existing approval path. Replacement and destructive operations count against the separate destructive cap.

The plan hash binds schema version, desired hash, snapshot precondition hash, guild, application, actor, deletion intent, typed operation payload fingerprints, operation IDs, risks, approvals, and dependencies. Human summaries, formatting, timestamps, and the hash field do not affect it. Apply reparses strict JSON, recomputes IDs/risks/dependencies, and requires the exact caller-supplied hash.

## Drift and conflicts

`drift` refreshes live state without mutation. A changed managed snapshot marks the plan stale and reports a `stale_precondition` conflict; planner conflicts remain listed as unmanaged conflicts. Apply refuses any unresolved conflict or stale snapshot.

## CLI

```text
discord-control snapshot --config config.json --guild GUILD --application APPLICATION --output snapshot.json
discord-control plan --config config.json --desired desired.json --output plan.json --format human
discord-control drift --config config.json --plan plan.json --format human
discord-control apply --config config.json --plan plan.json --plan-hash HASH --approval-id ID --allow-delete false
discord-control resume --config config.json --run RUN_ID --plan-hash HASH --approval-id ID --allow-delete false
```

All file arguments are relative to the configuration directory, bounded, traversal-checked, and symlink-checked. Plan JSON is always written to `--output`; `--format` controls stdout rendering.
