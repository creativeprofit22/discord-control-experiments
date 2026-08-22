# Schema migrations

Owner: Maintainers<br>
Last reviewed: 2026-08-21

The only supported desired-state version is `discord-control/v1`. Migration dispatch reads only `schema_version` after duplicate-key, depth, node, size, and secret-shaped-key inspection. Unknown, missing, or future versions fail closed.

A migration must be deterministic: identical bytes produce identical canonical v1 state; it may normalize decimal permission text and nil/empty collections but must not query Discord, read environment variables, invent logical IDs, adopt resources, or weaken deletion protection. Migration output is run through the current strict typed decoder and semantic validator.

When v2 is introduced:

1. Keep v1 parsing and golden hashes while its support window remains active.
2. Add a bounded `v1 → v2` pure transform and checked fixtures.
3. Document every field default, rename, removed capability, and conflict.
4. Never migrate plans or journals in place; create a fresh snapshot and plan.
5. Remove an old version only in a major release with an operator migration notice.
