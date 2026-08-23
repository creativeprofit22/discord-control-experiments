# Emergency lockdown checklist

Owner: Maintainers  
Last reviewed: 2026-08-23

## Before an incident

- Configure at least one protected operator role below the bot's highest role.
- Verify the bot has `MANAGE_CHANNELS` and can read every selected channel.
- Mark channels that must never be altered as protected resources.
- Keep operation/destructive caps small and practice plan, partial failure, resume, and recovery.
- Back up and restore-drill the complete state directory.

## Review

1. Select explicit channel/category scope; empty or cross-guild scope is rejected.
2. Build the plan from fresh guild, channel, role, and bot-member state.
3. Confirm every operation stays in scope and preserves `VIEW_CHANNEL` plus protected-role communication.
4. Record both full plan hashes; exact recovery is persisted before lockdown.
5. Obtain a single-use `lockdown_plan` approval for the reviewed hash.

## Apply and recover

Apply checks hash, expiry, actor/guild binding, hierarchy, approval, and each overwrite's exact current value. Every mutation is journaled and synced. On failure, stop; inspect completed and pending IDs. Resume only when current values match the expected before/after state. Recovery restores exact pre-lockdown overwrites and refuses drift—it never guesses or broadens scope. Invite revocation is separate because deleted invite codes cannot be restored.
