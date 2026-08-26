# Upgrade and rollback

Owner: Operations
Last reviewed: 2026-08-25

## Upgrade

1. Stop the service and preserve the current binary/configuration selector.
2. Create and verify a backup using [recovery drills](../runbooks/readiness-drills.md).
3. Restore the backup into a new staging directory.
4. Run `state validate --config STAGING/config.json --backup BACKUP.zip` with required secret references present.
5. Review supported config, state, desired-state, and backup versions in `release-manifest.json`.
6. Start the new binary against staging, then run health and read-only checks before mutation.
7. Atomically select staging only after validation succeeds.

Desired-state v1 is migrated in memory to v2 during strict parsing. Never edit a reviewed plan or journal to force compatibility; regenerate and review its hash.

## Rollback

Stop the service, validate the previously preserved directory with the rollback binary, and atomically restore its selector. Never downgrade files in place or copy individual journals. Outstanding in-memory approvals do not survive restart and must be requested again.

A schema version absent from the release manifest, an audit/journal failure, missing secret reference, malformed workflow, or incompatible backup blocks upgrade and rollback.
