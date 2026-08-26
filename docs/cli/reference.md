# CLI command reference

Generated from `internal/cli` definitions. Do not edit by hand.

## `appeal export`

Export subject-visible appeal data

- Handler: `moderation.appeal.export`
- Output kind: `AppealExport`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

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

## `case ban`

moderation.ban

- Handler: `moderation.ban`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `case bulk-delete`

moderation.bulk_message_delete

- Handler: `moderation.bulk_message_delete`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `case create`

Create a moderation case

- Handler: `moderation.case.create`
- Output kind: `ModerationCase`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `case delete-message`

moderation.message_delete

- Handler: `moderation.message_delete`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `case get`

Get a moderation case

- Handler: `moderation.case.get`
- Output kind: `ModerationCase`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `case invite-create`

moderation.invite_create

- Handler: `moderation.invite_create`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `case invite-revoke`

moderation.invite_revoke

- Handler: `moderation.invite_revoke`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `case kick`

moderation.kick

- Handler: `moderation.kick`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `case list`

List moderation cases

- Handler: `moderation.case.list`
- Output kind: `ModerationCases`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `case quarantine-add`

moderation.quarantine_add

- Handler: `moderation.quarantine_add`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `case quarantine-remove`

moderation.quarantine_remove

- Handler: `moderation.quarantine_remove`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `case remove-timeout`

moderation.timeout_remove

- Handler: `moderation.timeout_remove`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `case timeout`

moderation.timeout

- Handler: `moderation.timeout`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `case unban`

moderation.unban

- Handler: `moderation.unban`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `case warn`

moderation.warn

- Handler: `moderation.warn`
- Output kind: `DiscordResult`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
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

## `escalation apply`

Apply a reviewed escalation decision

- Handler: `moderation.escalation.apply`
- Output kind: `ModerationAction`
- Timeout: `write`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `escalation evaluate`

Evaluate deterministic escalation

- Handler: `moderation.escalation.evaluate`
- Output kind: `Escalation`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `evidence attach`

Attach a structured evidence locator

- Handler: `moderation.evidence.attach`
- Output kind: `ModerationEvidence`
- Timeout: `write`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

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

## `invite list`

moderation.invite_list

- Handler: `moderation.invite_list`
- Output kind: `DiscordResult`
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

## `lockdown apply`

Apply a reviewed lockdown plan

- Handler: `moderation.lockdown.apply`
- Output kind: `LockdownApply`
- Timeout: `workflow`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--plan-hash` (string, required)
  - `--approval-id` (string, required)

## `lockdown plan`

Build and persist lockdown/recovery plans

- Handler: `moderation.lockdown.plan`
- Output kind: `LockdownPlan`
- Timeout: `workflow`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--guild` (string, required)
  - `--input` (input, required)

## `lockdown recover`

Apply exact reviewed lockdown recovery

- Handler: `moderation.lockdown.recover`
- Output kind: `LockdownRecovery`
- Timeout: `workflow`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--guild` (string, required)
  - `--plan-hash` (string, required)
  - `--approval-id` (string, required)

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

Run enabled local service lifecycles

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

## `state backup`

Create a deterministic offline configuration and state backup

- Handler: `state.backup`
- Output kind: `StateBackup`
- Timeout: `workflow`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--output` (path, required)

## `state restore`

Restore a verified backup into new staging

- Handler: `state.restore`
- Output kind: `StateRestore`
- Timeout: `workflow`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--backup` (path, required)
  - `--staging` (path, required)

## `state validate`

Validate upgrade and rollback compatibility

- Handler: `state.validate`
- Output kind: `StateValidation`
- Timeout: `workflow`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--backup` (path, optional)

## `state verify`

Verify a backup archive without extracting it

- Handler: `state.verify`
- Output kind: `StateBackupVerification`
- Timeout: `read`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--backup` (path, required)

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

## `workflow approve`

Resume reviewed workflow output

- Handler: `workflow.approve`
- Output kind: `WorkflowRun`
- Timeout: `workflow`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--run` (string, required)
  - `--operation-hash` (string, required)

## `workflow dead-letter list`

List terminal dead-letter workflows

- Handler: `workflow.dead-letter.list`
- Output kind: `WorkflowRunList`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`

## `workflow get`

Get persisted workflow run state

- Handler: `workflow.get`
- Output kind: `WorkflowRun`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--run` (string, required)

## `workflow list`

List configured deterministic workflows

- Handler: `workflow.list`
- Output kind: `WorkflowList`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`

## `workflow recover`

Resolve an uncertain workflow with evidence

- Handler: `workflow.recover`
- Output kind: `WorkflowRun`
- Timeout: `workflow`
- Mutation: `true`; destructive: `true`
- Flags:
  - `--run` (string, required)
  - `--decision` (string, required)
  - `--evidence` (string, required)

## `workflow retry`

Retry a proven failed workflow

- Handler: `workflow.retry`
- Output kind: `WorkflowRun`
- Timeout: `workflow`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--run` (string, required)

## `workflow run`

Run a manual workflow

- Handler: `workflow.run`
- Output kind: `WorkflowRun`
- Timeout: `workflow`
- Mutation: `true`; destructive: `false`
- Flags:
  - `--workflow` (string, required)
  - `--trigger` (string, required)

## `workflow validate`

Validate configured workflow bindings

- Handler: `workflow.validate`
- Output kind: `Workflow`
- Timeout: `fast`
- Mutation: `false`; destructive: `false`
- Flags:
  - `--workflow` (string, required)
