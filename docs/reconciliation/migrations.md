# Schema migrations

Owner: Maintainers<br>
Last reviewed: 2026-08-23

The current desired-state version is `discord-control/v2`; strict `discord-control/v1` input remains supported through one deterministic migration. Future and unknown versions fail closed. Migration dispatch reads only `schema_version` after duplicate-key, depth, node, size, and secret-shaped-key inspection. Unknown, missing, or future versions fail closed.

A migration must be deterministic: identical bytes produce identical canonical v2 state; it may normalize decimal permission text and nil/empty collections but must not query Discord, read environment variables, invent logical IDs, adopt resources, or weaken deletion protection. Migration output is run through the current strict typed decoder and semantic validator.

The `v1 → v2` transform changes only `schema_version` and adds an empty `auto_moderation_rules` collection. It does not preserve v1 desired/plan hashes; operators must capture a fresh snapshot and review a fresh plan. V1 is frozen and rejects AutoMod fields. No plans, bindings, or journals are migrated in place.

Remove v1 only in a major release with an operator migration notice.
