# Moderation action matrix

Owner: Maintainers  
Last reviewed: 2026-08-23

| Typed action | Capability | Discord operation | Approval |
|---|---|---|---|
| Warn | `moderation.warn` | Local case record | No |
| Timeout / remove | `moderation.timeout` | Modify member | Policy-dependent |
| Kick | `moderation.kick` | Remove member | Required |
| Ban / unban | `moderation.ban` | Create/delete ban | Ban required |
| Delete / bulk delete | `moderation.message_delete` | Delete messages | Required |
| Quarantine add/remove | `moderation.quarantine` | Member role change | Required |
| Invite list/create/revoke | `moderation.invite_control` | Guild invite operations | Create/revoke required |
| Escalation evaluation | `moderation.escalation.evaluate` | No Discord mutation | No |
| Lockdown plan/apply/recover | matching lockdown capability | Channel overwrites | Apply/recover require plan approval |

Discord prerequisites include `MODERATE_MEMBERS`, `KICK_MEMBERS`, `BAN_MEMBERS`, `MANAGE_MESSAGES`, `MANAGE_ROLES`, `MANAGE_GUILD`, and `MANAGE_CHANNELS` as applicable. The bot's highest role must exceed target and quarantine roles. Discord's role hierarchy and guild-owner protections remain authoritative.

Raw Discord commands remain available for compatibility, but they never create or update moderation cases. Grant typed capabilities to moderator actors instead of relying on broad raw-command access.
