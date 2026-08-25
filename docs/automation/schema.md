# Automation workflow schema

Owner: Maintainers<br>
Last reviewed: 2026-08-23

Automation reads strict JSON version 1. Unknown fields and trailing JSON fail startup. Definitions are normalized, hashed, and immutable. A workflow contains `version`, `id`, `enabled`, one `trigger`, static `targets.guildIds`, bounded `retry`, and ordered `steps`. Each step has ordered all-match `conditions`, pure `transforms`, and exactly one typed `action`.

## Trigger catalog

| Kind | Required fields | Bounds |
|---|---|---|
| `manual` | none | operator invocation only |
| `once` | `at` | RFC 3339 UTC instant |
| `interval` | `every` | 1 minute–30 days |
| `calendar` | `time`, optional `weekdays` | UTC `HH:MM`; seven weekdays maximum |
| `news` | configured `source`, `limit` | 1–100 items |
| `member_scan` | `scanEvery`, `limit` | 1 minute–24 hours; 1–1000 members |

Calendar schedules are daily when `weekdays` is empty and weekly otherwise. Cron expressions are not accepted.

## Conditions and transforms

Conditions are `source_kind`, `text_contains`, and `member_has_role`. Every condition must match. Transforms run in order and are only `render` or `summarize`. Render templates replace `{title}`, `{text}`, and `{link}`; template execution and `{{...}}` expressions are rejected. Summaries set `maxBytes` and require the action's `requireReview:true`.

## Action catalog

`message`, `webhook`, `forum_thread`, `media_thread`, `poll`, `welcome`, `role_add`, `role_remove`, `cleanup`, and `snapshot` are the complete action set. Channels, roles, guilds, and webhook aliases are static IDs or configured names. Polls have 2–10 `choices`. Cleanup requires a configured `guildId:channelId` allowlist entry, age, per-run limit, destructive policy, and review. Generated message-like actions always send empty `allowed_mentions`.

## Rejected capabilities

The schema has no script, shell, expression, import, route, method, URL, header, credential, operation-name, MCP-tool, DAG, or arbitrary HTTP field. Unknown trigger, condition, transform, and action kinds fail closed. Workflows cannot select a Discord token, webhook URL, source URL, summarizer endpoint, or request header.

## Limits and hashing

Configuration caps workflow bytes, steps, target guilds, total actions, destructive actions, workers, sources, and cleanup bindings. IDs and snowflakes are bounded and validated. The SHA-256 definition hash covers normalized versioned content and drives run, child, step, lease, and idempotency identities. Editing a scheduled definition requires operator reconciliation rather than silently replacing its schedule.
