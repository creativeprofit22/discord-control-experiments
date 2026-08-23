# Evidence privacy and retention

Owner: Maintainers  
Last reviewed: 2026-08-23

The local store accepts locators only:

- Discord message: guild, channel, and message IDs.
- Discord audit event: guild and audit event ID.
- External system: bounded printable system name and opaque reference ID.

URLs, message text, attachment bytes, authorization data, arbitrary metadata, and secrets are rejected or have no field. Evidence must match the case guild and subject. Notes are bounded and redacted before persistence. Audit records contain stable kinds and correlation data, not evidence content. Metrics never label guild, subject, case, evidence, note, keyword, regex, or message values.

Retention compaction runs under the guild lock. It drops expired evidence and old closed-case action detail only after configured retention, retaining non-sensitive action tombstones for idempotency and audit continuity. A hash-chained, synced replacement generation is written before switching authority; corruption, sequence gaps, cross-guild records, and interior truncation fail closed.

## Backup limitation

This repository configures no off-host backup. Back up the complete configured state directory, including guild journals, plans, bindings, and prior generations. Restore into a separate location and run a timed drill before claiming an RPO or RTO. Approvals are intentionally in-memory and do not survive restart.
