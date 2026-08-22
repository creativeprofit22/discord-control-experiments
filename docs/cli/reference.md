# CLI command reference

Generated from `internal/cli` definitions. Do not edit by hand.

## `apply`

Apply an approved immutable plan

- Handler: `apply`
- Output kind: `Apply`
- Timeout: `workflow`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--plan` (path, required)
  - `--plan-hash` (string, required)
  - `--approval-id` (string, optional)
  - `--allow-delete` (boolean, optional)

## `automod create`

create_auto_moderation_rule

- Handler: `create_auto_moderation_rule`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `automod delete`

delete_auto_moderation_rule

- Handler: `delete_auto_moderation_rule`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--rule` (string, required)

## `automod get`

get_auto_moderation_rule

- Handler: `get_auto_moderation_rule`
- Output kind: `DiscordResult`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--rule` (string, required)

## `automod list`

list_auto_moderation_rules

- Handler: `list_auto_moderation_rules`
- Output kind: `DiscordResult`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)

## `automod update`

modify_auto_moderation_rule

- Handler: `modify_auto_moderation_rule`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--rule` (string, required)
  - `--input` (input, required)

## `channel create`

create_channel

- Handler: `create_channel`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `channel delete`

delete_channel

- Handler: `delete_channel`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)

## `channel delete-permission`

delete_channel_permission

- Handler: `delete_channel_permission`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)
  - `--overwrite` (string, required)

## `channel get`

get_channel

- Handler: `get_channel`
- Output kind: `DiscordResult`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)

## `channel list`

list_guild_channels

- Handler: `list_guild_channels`
- Output kind: `DiscordResult`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)

## `channel reorder`

reorder_channels

- Handler: `reorder_channels`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `channel set-permission`

set_channel_permission

- Handler: `set_channel_permission`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)
  - `--input` (input, required)

## `channel update`

modify_channel

- Handler: `modify_channel`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)
  - `--input` (input, required)

## `command create`

create_guild_application_command

- Handler: `create_guild_application_command`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--application` (string, required)
  - `--input` (input, required)

## `command delete`

delete_guild_application_command

- Handler: `delete_guild_application_command`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--application` (string, required)
  - `--command` (string, required)

## `command get`

get_guild_application_command

- Handler: `get_guild_application_command`
- Output kind: `DiscordResult`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--application` (string, required)
  - `--command` (string, required)

## `command list`

list_guild_application_commands

- Handler: `list_guild_application_commands`
- Output kind: `DiscordResult`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--application` (string, required)

## `command replace`

bulk_overwrite_guild_application_commands

- Handler: `bulk_overwrite_guild_application_commands`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--application` (string, required)
  - `--input` (input, required)

## `command update`

modify_guild_application_command

- Handler: `modify_guild_application_command`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--application` (string, required)
  - `--command` (string, required)
  - `--input` (input, required)

## `completion bash`

Generate bash completion

- Handler: `completion.bash`
- Output kind: `Completion`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`

## `completion fish`

Generate fish completion

- Handler: `completion.fish`
- Output kind: `Completion`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`

## `completion powershell`

Generate powershell completion

- Handler: `completion.powershell`
- Output kind: `Completion`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`

## `completion zsh`

Generate zsh completion

- Handler: `completion.zsh`
- Output kind: `Completion`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`

## `diagnostics bundle`

Write a bounded sanitized diagnostic bundle

- Handler: `diagnostics.bundle`
- Output kind: `Diagnostics`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--output` (path, required)

## `drift`

Compare current state with a saved plan

- Handler: `drift`
- Output kind: `Drift`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--plan` (path, required)

## `health discord`

Run a bounded read-only Discord check

- Handler: `health.discord`
- Output kind: `Health`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)

## `health local`

Check local configuration and state without egress

- Handler: `health.local`
- Output kind: `Health`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`

## `help`

Show deterministic command help

- Handler: `help`
- Output kind: `Help`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`

## `inspect`

Inspect a guild

- Handler: `inspect_guild`
- Output kind: `Guild`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)

## `job cancel`

Persist cooperative job cancellation

- Handler: `job.cancel`
- Output kind: `Job`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--run` (string, required)

## `job get`

Get bounded workflow job metadata

- Handler: `job.get`
- Output kind: `Job`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--run` (string, required)

## `job list`

List durable workflow jobs

- Handler: `job.list`
- Output kind: `JobList`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, optional)

## `job resume`

Resume a recoverable durable job

- Handler: `job.resume`
- Output kind: `Job`
- Timeout: `workflow`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--run` (string, required)
  - `--plan-hash` (string, required)
  - `--approval-id` (string, optional)

## `member add-role`

add_member_role

- Handler: `add_member_role`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--role` (string, required)
  - `--user` (string, required)

## `member get`

get_member

- Handler: `get_member`
- Output kind: `DiscordResult`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--user` (string, required)

## `member list`

list_members

- Handler: `list_members`
- Output kind: `DiscordResult`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)

## `member remove-role`

remove_member_role

- Handler: `remove_member_role`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--role` (string, required)
  - `--user` (string, required)

## `member update`

modify_member

- Handler: `modify_member`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--user` (string, required)
  - `--input` (input, required)

## `message bulk-delete`

bulk_delete_messages

- Handler: `bulk_delete_messages`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)
  - `--input` (input, required)

## `message create`

create_message

- Handler: `create_message`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)
  - `--input` (input, required)

## `message delete`

delete_message

- Handler: `delete_message`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)
  - `--message` (string, required)

## `message get`

get_message

- Handler: `get_message`
- Output kind: `DiscordResult`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)
  - `--message` (string, required)

## `message list`

list_messages

- Handler: `list_messages`
- Output kind: `DiscordResult`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)

## `message pin`

pin_message

- Handler: `pin_message`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)
  - `--message` (string, required)

## `message unpin`

unpin_message

- Handler: `unpin_message`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)
  - `--message` (string, required)

## `message update`

modify_message

- Handler: `modify_message`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)
  - `--message` (string, required)
  - `--input` (input, required)

## `moderation ban`

create_member_ban

- Handler: `create_member_ban`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--user` (string, required)
  - `--input` (input, required)

## `moderation kick`

remove_member

- Handler: `remove_member`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--user` (string, required)

## `moderation unban`

delete_member_ban

- Handler: `delete_member_ban`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--user` (string, required)

## `plan`

Build an immutable reconciliation plan

- Handler: `plan`
- Output kind: `Plan`
- Timeout: `workflow`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--desired` (path, required)
  - `--output` (path, required)

## `policy catalog`

List effective command capabilities

- Handler: `policy.catalog`
- Output kind: `PolicyCatalog`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--mode` (string, required)

## `policy check`

Check an effective capability decision

- Handler: `policy.check`
- Output kind: `PolicyDecision`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--capability` (string, required)
  - `--mode` (string, required)

## `policy explain`

Explain a sanitized policy decision

- Handler: `policy.explain`
- Output kind: `PolicyDecision`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--capability` (string, required)
  - `--mode` (string, required)

## `resume`

Resume an interrupted reconciliation run

- Handler: `resume`
- Output kind: `Resume`
- Timeout: `workflow`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--run` (string, required)
  - `--plan-hash` (string, required)
  - `--approval-id` (string, optional)
  - `--allow-delete` (boolean, optional)

## `role create`

create_role

- Handler: `create_role`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `role delete`

delete_role

- Handler: `delete_role`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--role` (string, required)

## `role list`

list_guild_roles

- Handler: `list_guild_roles`
- Output kind: `DiscordResult`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)

## `role reorder`

reorder_roles

- Handler: `reorder_roles`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `role update`

modify_role

- Handler: `modify_role`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--role` (string, required)
  - `--input` (input, required)

## `run`

Run the MCP stdio service

- Handler: `run`
- Output kind: `Runtime`
- Timeout: `workflow`
- Mutation: `false`; destructive: `false`

## `snapshot`

Capture a declarative guild snapshot

- Handler: `snapshot`
- Output kind: `Snapshot`
- Timeout: `workflow`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--application` (string, required)
  - `--output` (path, required)

## `version`

Print build and CLI API version

- Handler: `version`
- Output kind: `Version`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`

## `webhook create`

create_webhook

- Handler: `create_webhook`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)
  - `--input` (input, required)

## `webhook delete`

delete_webhook

- Handler: `delete_webhook`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--webhook` (string, required)

## `webhook get`

get_webhook

- Handler: `get_webhook`
- Output kind: `DiscordResult`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--webhook` (string, required)

## `webhook list-channel`

list_channel_webhooks

- Handler: `list_channel_webhooks`
- Output kind: `DiscordResult`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--channel` (string, required)

## `webhook list-guild`

list_guild_webhooks

- Handler: `list_guild_webhooks`
- Output kind: `DiscordResult`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)

## `webhook update`

modify_webhook

- Handler: `modify_webhook`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--webhook` (string, required)
  - `--input` (input, required)
