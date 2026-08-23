# Declarative schema v2

Owner: Maintainers  
Last reviewed: 2026-08-23

Schema v2 is the current strict desired-state contract: `schema_version: "discord-control/v2"`. It retains every v1 resource and adds `auto_moderation_rules`. Unknown/duplicate keys, trailing JSON, oversized/deep documents, secret-shaped keys, unresolved logical references, duplicates, and Discord limit violations are rejected before hashing.

## Root

Required fields are `schema_version`, `guild_id`, `ownership`, `categories`, `channels`, `roles`, `webhooks`, and `application_commands`. `auto_moderation_rules` may be omitted and canonicalizes to an empty array. `application_id` and `onboarding` remain optional.

## AutoMod rule

```json
{
  "logical_id": "mention-spam",
  "mode": "managed",
  "name": "Mention spam",
  "event_type": 1,
  "trigger_type": 5,
  "trigger_metadata": {"mention_total_limit": 5},
  "actions": [{"type": 1}],
  "enabled": true,
  "exempt_roles": [{"kind": "role", "logical_id": "moderators"}],
  "exempt_channels": []
}
```

Alert actions use `alert_channel`, a logical channel reference. Timeout actions use `duration_seconds`; block-message actions may use `custom_message`. Logical references resolve to remote IDs only during apply.

Canonical normalization sorts rules, metadata lists, presets, exemptions, and semantically unordered actions. Per-trigger limits and compatibility are documented in [the AutoMod reference](../moderation/automod-reference.md).

## Migration

Strict v1 input migrates in memory to v2 by setting the new schema version and an empty AutoMod collection. V1 is frozen; no AutoMod field is accepted under v1. Hashes deliberately change after migration because v2 expands the desired-state contract.
