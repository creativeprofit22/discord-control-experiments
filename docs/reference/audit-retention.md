# Audit retention and privacy

Owner: Security and data governance
Last reviewed: 2026-08-21

The append-only JSONL audit contains sequence, previous/current SHA-256 hashes, timestamp, actor ID, approver ID when used, adapter, approved guild, stable operation, capability, resource kinds, sanitized intent, policy decision/reason, approval reference, execution mode, destructive class, result, correlation and parent correlation IDs, bounded redacted Discord audit reason, and operation hash.

It never intentionally stores actor subjects, command JSON, message content, names, topics, filenames, attachment bytes, raw payloads, URLs, webhook tokens, bot tokens, or resource values in telemetry dimensions. Intent is an allowlist of field names, counts, booleans, bounded enums, and numeric flags. Registered secrets are redacted before serialization and hashing; redaction failure rejects the append.

The configured path is relative and contained under the configuration directory. Files reject symlinks, directories, traversal, malformed tails, oversized records/files, sequence breaks, hash mismatch, reorder, and truncation against the `.head` file. Writes append one line, sync the JSONL, update the restrictive head, and return only after durability steps. Decision precommit failure blocks network; completion failure after a sent request alerts and leaves the pending precommit for incident handling.

`retentionDays` and `maxBytes` are operator limits, not an automatic deletion authority. Restrict host access to operators and incident responders. Archive by stopping the service, verifying integrity, copying JSONL plus `.head` to access-controlled storage, recording archive hashes, creating a new configured path, and restarting. Never rotate by editing or truncating an active file.

Deletion requires the organization's documented retention owner after legal/incident holds expire. Delete archives and local copies together through the authorized process; the application exposes no update/delete API. Hash chaining detects many integrity failures but does not provide non-repudiation, external timestamping, or survival against a privileged operator deleting both files.

See [approval model](approval-model.md), [incident response](../runbooks/security-incident.md), and [security model](../security/model.md).
