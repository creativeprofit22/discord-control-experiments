# Capability policy

Owner: Security and maintainers
Last reviewed: 2026-08-21

## Identity and evaluation

An adapter supplies `Principal{Adapter, Subject, AuthenticationMethod}` from its authenticated boundary. It cannot supply `ActorID` or grants. Policy maps one exact principal tuple to one configured actor; empty, unknown, duplicate, ambiguous, wildcard, or malformed values deny. Local MCP is stdio-only and uses composition-root subject `local-stdio`; model arguments, trace fields, Discord payloads, and command fields never establish identity.

Every transport attempt passes `dispatchDiscord` in this order: sealed command and invocation validation; registry and resource ownership validation; principal resolution; guild, capability, mode, destructive-class and resource decision; canonical operation hash; single-use approval consumption when required; durable audit decision; transport; durable completion. Errors and recovered evaluator panics become `policy_error`. Audit precommit failure prevents network access.

`read_only` removes mutations before discovery and rejects any stale mutation at runtime. `dry_run` returns a content-free `OperationPlan` with no network, baseline, reconciliation, idempotency claim, or approval consumption. `apply` is the only network-capable mode. Invocation mode can be lower than the actor and operator ceilings, never higher.

## Complete command matrix

Commands not listed by the sealed 52-entry registry do not exist at runtime.

| Capability | Read operations | Mutation operations | Mandatory destructive classes |
|---|---|---|---|
| `guild.inspect` | `inspect_guild` | — | — |
| `channel.read` | `get_channel`, `list_guild_channels` | — | — |
| `channel.manage` | — | `create_channel`, `modify_channel`, `reorder_channels`, `delete_channel` | `permission_change`: reorder; `delete`: delete |
| `role.read` | `list_guild_roles` | — | — |
| `role.manage` | — | `create_role`, `modify_role`, `reorder_roles`, `delete_role`, `set_channel_permission`, `delete_channel_permission` | `role_escalation`: permission-bearing create/modify; `permission_change`: channel permissions; `delete`: delete role |
| `member.read` | `get_member`, `list_guild_members`, `get_guild_bans` | — | — |
| `member.moderate` | — | `modify_member`, `add_member_role`, `remove_member_role`, `remove_member`, `create_member_ban`, `remove_member_ban` | `role_escalation`: member role changes; `kick`, `ban`, or `delete` as classified |
| `message.read` | `get_message`, `list_messages`, `list_pinned_messages` | — | — |
| `message.manage` | — | `create_message`, `modify_message`, `delete_message`, `bulk_delete_messages`, `pin_message`, `unpin_message` | `delete` or `bulk_delete` |
| `webhook.read` | `get_webhook`, `list_channel_webhooks`, `list_guild_webhooks` | — | — |
| `webhook.manage` | — | `create_webhook`, `modify_webhook`, `delete_webhook`, `execute_webhook` | `webhook_create` or `delete` |
| `invite.read` | `get_invite`, `list_channel_invites`, `list_guild_invites` | — | — |
| `invite.manage` | — | `create_invite`, `delete_invite` | `invite_create` or `delete` |
| `application_command.read` | `list_guild_application_commands`, `get_guild_application_command` | — | — |
| `application_command.manage` | — | `create_guild_application_command`, `modify_guild_application_command`, `delete_guild_application_command`, `bulk_overwrite_guild_application_commands` | `delete` or `bulk_overwrite` |
| `scheduled_event.read` | `list_scheduled_events`, `get_scheduled_event`, `list_scheduled_event_users` | — | — |
| `scheduled_event.manage` | — | `create_scheduled_event`, `modify_scheduled_event`, `delete_scheduled_event` | `delete` |

`lockdown` remains a reserved approval-required workflow class; there is no raw `LockdownGuild` request escape hatch. Any role request carrying permissions is conservatively `role_escalation`; requesting Discord `ADMINISTRATOR` is never implicit.

## Resource ownership

Guild IDs must be explicitly granted. Registry extractors enumerate guild, channel, role, member, message, webhook, invite, application-command, and scheduled-event IDs before policy. A known owner from another guild denies. Unqualified resources require a known matching owner. Guild-qualified resources still deny when a known ownership record conflicts. Unknown capabilities, resource kinds, modes, or destructive classes deny.

## Example

```json
{"policy":{"mode":"apply","actors":[{"id":"reader","principals":[{"adapter":"mcp","subject":"local-stdio","authenticationMethod":"os-process"}],"guilds":["123456789012345678"],"capabilities":["guild.inspect","channel.read"],"mode":"read_only"}]}}
```

See [Discord permissions](discord-permissions.md), [approval model](approval-model.md), and [configuration](configuration.md).
