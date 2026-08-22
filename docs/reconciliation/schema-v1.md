# Declarative schema v1

Owner: Maintainers<br>
Last reviewed: 2026-08-21

A desired document is strict JSON with `schema_version: "discord-control/v1"`. Unknown or duplicate keys, trailing JSON, nesting deeper than 32 levels, documents over 1 MiB, secret-shaped keys, unresolved references, duplicate logical IDs, and excessive collections are rejected before planning.

## Root

| Field | Required | Meaning |
|---|---:|---|
| `schema_version` | yes | Exactly `discord-control/v1`. |
| `guild_id` | yes | Target Discord guild snowflake. |
| `application_id` | no | Application owning guild commands. |
| `ownership` | yes | Controller identity and adoption/deletion policy. |
| `categories`, `channels`, `roles`, `webhooks`, `application_commands` | yes | Resource declarations; use empty arrays when absent. |
| `onboarding` | no | Guild onboarding declaration. |

`ownership.owner` is a non-secret operator label. `adopt_by_name` permits exact name-and-type adoption only when one candidate exists. `delete_orphans` expresses deletion intent; runtime `--allow-delete true` and destructive approval remain independently required. `protect_deletes` protects managed resources omitted from desired state.

## Logical references and modes

Every resource has a lowercase `logical_id` matching `[a-z][a-z0-9_-]{0,63}`. A reference is `{"kind":"channel","logical_id":"announcements"}`; remote IDs never replace logical references in desired state. Durable ownership bindings resolve logical references first. Name adoption is optional and ambiguity is always a conflict.

`mode` is `managed` (default), `observed`, or `ignored`. Managed fields participate in diff/apply. Observed fields are exported but not mutated. Ignored fields do not participate. Unsupported desired fields are conflicts, not silent omissions.

## Resources

- **Categories:** `logical_id`, `name`, `position`, optional `mode` and `protected`.
- **Channels:** category reference, Discord `type`, normalized `position`, topic/NSFW/rate limit, voice settings, thread defaults, permission overwrites, and optional forum settings.
- **Forums/media:** `available_tags`, default reaction emoji, sort order, and layout are channel fields. Desired tags do not require remote IDs.
- **Roles:** name, arbitrary-precision decimal `permissions`, position, color, hoist, mentionable, mode, and protection.
- **Overwrites:** logical role target, overwrite type, and arbitrary-precision decimal `allow`/`deny` values.
- **Managed webhooks:** logical channel reference and name only. Tokens, executable URLs, and arbitrary headers have no schema fields.
- **Application commands:** logical identity, command type, name, description, typed options, default member permissions, NSFW, mode, and protection. Remote command IDs and versions are observed state.
- **Onboarding:** enabled/mode, logical default channels, prompts, and options whose channel/role memberships are logical references.

Permission strings are non-negative base-10 integers. Parsing accepts leading zeroes only for migration input and canonical output removes them (`"0008"` becomes `"8"`). Values are not limited to 64 bits.

## Safety limits

Each primary resource collection is capped at 1,000 entries; channel overwrites at 100; onboarding prompts at 50; and options per prompt at 100. Names and Discord-specific values are revalidated again at the sealed command boundary. Documents cannot contain scripts, arbitrary command names, URLs, tokens, authorization headers, or webhook credentials.

See [migrations](migrations.md), [snapshot semantics](snapshots.md), and the [small](examples/small.json) and [complex](examples/complex.json) examples.
