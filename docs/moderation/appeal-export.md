# Appeal-support export

Owner: Maintainers  
Last reviewed: 2026-08-23

Appeal export requires `moderation.evidence.export` independently from case read/write. The request is bound to one guild and subject. Output is deterministic JSON with schema `discord-control/appeal-export-v1`.

The export includes case IDs, subject-visible reason codes, status, timestamps, action kind/status/expiry, and unexpired eligible evidence locators. It excludes moderator notes, actor identity, policy internals, operation approvals, audit-chain hashes, unrelated subjects, secrets, and expired evidence.

Treat the export as personal data. Deliver it through an authenticated operator process, retain it only as long as needed, and do not paste it into public channels. A dry-run policy mode does not produce an export because reads require an effective read-capable decision.
