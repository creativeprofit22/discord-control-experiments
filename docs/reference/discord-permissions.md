# Discord permission matrix

Owner: Security and operators
Last reviewed: 2026-08-21

Grant the bot only the union of permissions required by configured capabilities. Never grant Discord `ADMINISTRATOR` by default; it bypasses channel overwrites and expands one credential leak to the entire guild. Application policy does not replace Discord's server-side permission checks.

| Capability | Minimum Discord permissions commonly required |
|---|---|
| `guild.inspect`, `channel.read`, `role.read`, `member.read` | `VIEW_CHANNEL`; member listing may also require the privileged Guild Members intent where applicable |
| `message.read` | `VIEW_CHANNEL`, `READ_MESSAGE_HISTORY` |
| `channel.manage` | `MANAGE_CHANNELS` |
| `role.manage` | `MANAGE_ROLES`; the bot's highest role must remain below operator-controlled roles |
| `member.moderate` | only the action-specific union: `MODERATE_MEMBERS`, `KICK_MEMBERS`, or `BAN_MEMBERS`; add `MANAGE_ROLES` only for role workflows |
| `message.manage` | `SEND_MESSAGES` for sends; `MANAGE_MESSAGES` only for edit/delete/pin workflows; attachment permission only when uploads are used |
| `webhook.read`, `webhook.manage` | `MANAGE_WEBHOOKS` only when webhook workflows are enabled |
| `invite.read`, `invite.manage` | `CREATE_INSTANT_INVITE`; `MANAGE_GUILD` may be required for guild-wide invite inspection |
| `application_command.read`, `application_command.manage` | OAuth application command scope and application ownership; no guild `ADMINISTRATOR` grant |
| `scheduled_event.read`, `scheduled_event.manage` | `VIEW_CHANNEL`; add `MANAGE_EVENTS` only for mutation workflows |

A least-permission read fixture grants one guild plus `guild.inspect`, `channel.read`, and `message.read`, with Discord `VIEW_CHANNEL` and `READ_MESSAGE_HISTORY`. A moderation fixture is separate and never inherits application-command, webhook, invite, role, or channel-management capabilities.

Role creation/modification with permissions, member role assignment, channel overwrite changes, and any `ADMINISTRATOR` bit are escalation-sensitive and require human approval. The configuration and control plane must reject implicit administrator grants; operators must remove any existing administrator role before enabling apply mode.

Verify permissions after rotation using an allowed read command, then one deliberately denied capability. Do not add a live permission probe or broader role merely to make startup succeed.

See [capability policy](capability-policy.md), [approval model](approval-model.md), and [security incident runbook](../runbooks/security-incident.md).
