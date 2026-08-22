# Approval model

Owner: Security and operators
Last reviewed: 2026-08-21

## Boundary

Approval issuance is a trusted administrative port supplied directly by composition or a future authenticated human-control UI. It is not an MCP tool, command field, model function, Discord payload, or policy capability. The current runtime deliberately wires the store only for consumption; therefore destructive apply operations remain unavailable until a human-control adapter is explicitly added.

## Binding and lifecycle

The canonical SHA-256 operation hash covers actor ID, adapter, approved guild, stable operation, capability, sorted typed resource references, effective mode, destructive class, and the sealed command fingerprint. Raw command JSON, message content, names, topics, files, payloads, URLs, and audit bodies are not persisted.

An issued grant binds one opaque random approval ID to one actor and operation hash, one named approver, issuance time, expiry, and configured maximum TTL. Consumption is one mutex-protected atomic transition. Unknown, forged, mismatched, expired-at-boundary, stale, or already-consumed IDs deny before Discord access. IDs are references, not authority without the stored binding. Restart empties the in-memory store and invalidates all outstanding grants.

Mandatory classes are delete, bulk delete, ban, kick/removal, role escalation, channel permission change, webhook creation, invite creation, bulk application-command overwrite, and lockdown. Dry runs never consume approvals. Narrow non-destructive operations may be operator-preapproved by exact stable operation; wildcard preapproval is rejected.

## Human review

The approval screen must show actor, guild, stable operation, resource kinds, sanitized intent, destructive class, expiry, and operation-hash prefix. It must never show or accept raw bearer secrets as approval evidence. To limit approval fatigue, group no unrelated actions, keep TTL short, require a fresh grant after any command change, and measure latency without approval-ID metric dimensions.

Models cannot self-approve because no issuance tool exists and command arguments cannot select an actor, approver, grant, capability, guild, or execution ceiling. Add a human adapter only with authenticated approvers, CSRF/session controls where applicable, and a separate threat-model review.

See [capability policy](capability-policy.md), [audit retention](audit-retention.md), and [security incident runbook](../runbooks/security-incident.md).
