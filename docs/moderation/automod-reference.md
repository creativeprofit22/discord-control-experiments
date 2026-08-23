# Discord AutoMod reference

Owner: Maintainers  
Last reviewed: 2026-08-23

Declarative AutoMod requires `schema_version: "discord-control/v2"` and `auto_moderation_rules`. Each rule has a logical ID, mode, name, event type, trigger type, trigger metadata, actions, enabled state, and logical role/channel exemptions.

Supported triggers are keyword (`1`), spam (`3`), keyword preset (`4`), mention spam (`5`), and member profile (`6`). Member profile uses member-update event `2`; all others use message-send event `1`. Supported actions are block message (`1`), send alert (`2`), timeout (`3`), and member-profile block interaction (`4`).

Validation enforces current Discord limits before hashing: six keyword rules and one rule for each other trigger; up to 1,000 keywords, 10 regex patterns, and 100 allow-list values (1,000 for presets), three actions, 20 role exemptions, and 50 channel exemptions. Mention limits are 1–50 and timeout duration is at most 28 days. Regex strings are length-checked but not executed locally because Discord uses Rust regex semantics.

Alert channels and exemptions are logical references resolved only during apply. Canonical hashing sorts semantically unordered metadata, actions, and exemptions. Adoption requires exactly one unmanaged rule with the normalized name and immutable trigger type. Trigger changes replace the rule. Enabled creation is always create-disabled then update-enable. Deletes are destructive and approval-protected; uncertain creates are never blindly retried.

Contract reference: <https://docs.discord.com/developers/resources/auto-moderation>.
