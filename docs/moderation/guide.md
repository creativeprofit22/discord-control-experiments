# Moderator guide

Owner: Maintainers  
Last reviewed: 2026-08-23

Moderation is case-oriented. Create a case with a configured reason code, attach structured evidence locators, then submit a typed action with a guild-scoped idempotency key. CLI, MCP, and signed interactions share one service and store.

## Safe workflow

1. Confirm the bot has the Discord permission listed in [the action matrix](action-matrix.md).
2. Create or locate the subject's case; never place message content or secrets in notes.
3. Attach only a Discord message ID, Discord audit event ID, or opaque external reference.
4. Dry-run the typed action and review its operation hash.
5. For destructive actions, obtain the required single-use approval and apply once.
6. If the result is `operator_review`, inspect Discord before retrying. An uncertain send is never replayed automatically.

The service refreshes bot identity, member records, guild ownership, and roles before hierarchy-sensitive actions. It refuses guild owners, equal/higher targets, protected operator roles, managed quarantine roles, unknown members, and unknown hierarchy. Discord may still reject a valid plan if permissions change between review and apply.

Timeouts cannot exceed 28 days. Removing an expired or already-removed timeout records a no-op without Discord egress. Duplicate idempotency keys return the original action; a changed payload fails closed.

All interaction responses are ephemeral. Guild and actor identity come from the verified interaction envelope, never command options. Gateway connectivity and AI punishment decisions are unsupported.
