# MCP catalog and capability map

Tool names are the sealed application command names. Discovery is rebuilt at startup in this order: configured toolsets, policy decision for the fixed principal/guild/mode, independent read-only removal, duplicate elimination, lexical sort.

| Toolset | Scope | Representative capabilities |
|---|---|---|
| `build` | Channels, roles, overwrites, application commands, onboarding, webhooks | `channel.manage`, `role.manage`, `application_command.manage`, `onboarding.manage`, `webhook.manage` |
| `moderation` | Members, bans/kicks, message moderation, auto-moderation | `member.manage`, `ban.manage`, `message.manage`, `auto_moderation.manage` |
| `news` | Messages, invites, scheduled events | `message.create`, `invite.manage`, `scheduled_event.manage` |
| `diagnostic` | Guild/channel/role/member/message/invite/event/rule inspection | all corresponding `.read` capabilities |

Read commands may belong to `diagnostic` and their domain toolset. Registration de-duplicates them by command name. `readOnly: true` removes every command whose application contract exposes a mutation, regardless of annotations. Descriptions state capability, mutation class, and approval behavior; annotations are hints and never authorize execution.

No tool exposes approvals, policy mutation, tokens, raw HTTP method/path/URL, resource fetching, prompts, scripts, or Discord transport access.