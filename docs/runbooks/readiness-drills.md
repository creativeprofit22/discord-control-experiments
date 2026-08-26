# Production-readiness drills

Owner: Operations and security
Last reviewed: 2026-08-25

Run drills from a clean checkout with live Discord credentials unset. The harness uses fixed fixtures and fake endpoints and refuses known live credential variables.

```console
scripts/drills.sh evidence/drills.json
```

The output uses `discord-control.drills/v1`; retain it with release evidence. A failed drill blocks release. Never retry or quarantine an integrity/security failure without diagnosing it.

## Backup and restore

1. Stop every process using the configuration directory.
2. Run `discord-control state backup --config config.json --output backup.zip`.
3. Run `discord-control state verify --backup backup.zip`.
4. Run `discord-control state restore --backup backup.zip --staging staging-next`.
5. Run `discord-control state validate --config staging-next/config.json --backup backup.zip`.
6. Inspect permissions, then atomically select `staging-next` as the configuration directory.

Restore never overwrites an existing directory. Digest, traversal, link, duplicate, schema, size, audit-chain, and state failures stop extraction.

## Upgrade, migration, and config rollback

Validate the current tree and backup before deployment. Desired-state v1 is parsed and normalized to v2; preserve the original reviewed plan hash. For rollback, stop the service and atomically select the previously validated configuration directory. Never copy individual state files between versions.

## Credential rotation

Create a second environment reference, update configuration, restart, and perform read-only health checks. Revoke the old credential only after success. Diagnostic, audit, log, trace, and drill evidence must not contain either value. See [secret rotation](secret-rotation.md).

## Discord outage

Keep mutation workers bounded and let configured 429/5xx backoff run. Do not manually replay uncertain creates. Confirm queues recover, no duplicate mutation exists, and dead letters retain evidence before resuming.

## Stuck workflow

Pause the scheduler, inspect the run and audit correlation, then classify it as retryable, uncertain, approval-waiting, or terminal. Resume only through the hash-bound command; uncertain mutations require operator evidence.

## Audit investigation

Copy the audit and head files read-only, run `state validate`, and join events to logs/traces with `correlation_id`. Any chain gap, reorder, truncation, or required write failure blocks mutation and release.

## Callback or webhook compromise

Disable interactions/automation, rotate callback and webhook references, inspect bounded audit records, then restart with fake/read-only validation first. Never paste URLs or signatures into tickets or diagnostics.

## Gateway and identify boundary

Gateway connectivity and identify are unsupported. Confirm health/dashboard capability state says `unsupported` and the composition test finds no Gateway client/listener. Do not supply a Gateway token or simulate identify exhaustion as if supported.
