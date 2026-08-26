# Audit investigation

Owner: Security
Last reviewed: 2026-08-25

1. Stop mutation if a required audit precommit/completion or chain check fails.
2. Copy the audit JSONL and head files read-only before repair attempts.
3. Run `state validate` against a staged configuration tree.
4. Join audit records to logs/traces using `correlation_id`; compare sequence, previous hash, and result.
5. Treat gaps, reorder, truncation, binding mismatch, or interior malformed records as integrity failures.
6. Preserve evidence, determine scope, rotate reachable credentials when exposure is plausible, then restore from verified staging.

Audit validation is evidence of chain consistency, not proof that every event is benign.
