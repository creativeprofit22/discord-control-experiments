# Snapshot semantics

Owner: Maintainers<br>
Last reviewed: 2026-08-21

`snapshot` reads a guild through the secured Discord command service, revalidates guild/application ownership on every response, loads durable bindings, and emits `discord-control/snapshot-v1` JSON. Managed webhook tokens and executable URLs are never requested or exported.

## Normalization

- Roles sort by Discord position, then remote ID.
- Categories and channels use an explicit normalized order: parent category position, channel position, then remote ID. This is stable even when Discord positions collide.
- Application commands sort by type, name, then ID; webhooks sort by ID.
- Nil collections become empty arrays before canonical hashing.
- Capture time and the hash field itself are excluded from the canonical snapshot hash.
- Mutable fingerprints exclude remote ID, guild ID, raw position, and parent binding while the binding section retains remote IDs.

## Field disposition

| Disposition | Meaning |
|---|---|
| `managed` | A durable binding gives this controller authority to compare and mutate the field. |
| `observed` | Exported for context but not owned or mutated. |
| `ignored` | Deliberately excluded from comparison. |
| `unsupported` | Discord documents the resource/feature as absent. Authorization errors are never converted to unsupported. |

Onboarding `404` after the guild itself was successfully read is represented as unavailable. `401`/`403`, malformed data, and cross-guild IDs remain fatal. Discord-managed roles, `@everyone`, application-owned/non-incoming webhooks, and explicitly protected declarations are marked protected.

Bindings map `(resource kind, logical_id)` to a remote Discord ID and non-secret fingerprint. Matching uses bindings before optional exact name/type adoption; zero candidates remain unmatched and multiple candidates produce a conflict.
