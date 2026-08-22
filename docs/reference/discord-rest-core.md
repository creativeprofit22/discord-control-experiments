# Discord REST capability core

Owner: Maintainers
Last reviewed: 2026-08-21

## Compatibility and boundary

The production contract is Discord API **v10** at the fixed origin `https://discord.com/api/v10`. Domain commands and results contain no SDK type. `domain.DiscordCommand[R]` is sealed and associates one result type with each command; callers use `application.Execute`. No public request accepts an HTTP method, path, base URL, webhook URL, token, or raw body. Global application commands and token-in-URL webhook execution are unsupported because they escape guild scope or expose a bearer capability.

Compatibility is reviewed against Discord's API reference and rate-limit topic. Route templates, enums, permission mask, limits, required response fields, and fixture payloads are the v10 matrix. A Discord version change requires a new ADR, updated constants/codecs, fixtures, fuzz seeds, and this document.

## Command catalog

- **Guild inspection:** `InspectGuild` returns the guild plus channel/category, role, webhook, invite, and scheduled-event inventories.
- **Channels/categories:** `ListGuildChannels`, `GetChannel`, `CreateChannel`, `ModifyChannel`, `DeleteChannel`, `ReorderChannels`. Category is channel type 4.
- **Roles:** `ListGuildRoles`, `CreateRole`, `ModifyRole`, `DeleteRole`, `ReorderRoles`.
- **Permissions:** `SetChannelPermission`, `DeleteChannelPermission`.
- **Members:** `ListMembers`, `GetMember`, `ModifyMember`, `AddMemberRole`, `RemoveMemberRole`, `RemoveMember`, `CreateMemberBan`, `DeleteMemberBan`.
- **Messages:** `ListMessages`, `GetMessage`, `CreateMessage`, `ModifyMessage`, `DeleteMessage`, `BulkDeleteMessages`, `PinMessage`, `UnpinMessage`.
- **Webhooks:** `ListGuildWebhooks`, `ListChannelWebhooks`, `CreateWebhook`, `GetWebhook`, `ModifyWebhook`, `DeleteWebhook`.
- **Guild application commands:** `ListGuildApplicationCommands`, `GetGuildApplicationCommand`, `CreateGuildApplicationCommand`, `ModifyGuildApplicationCommand`, `DeleteGuildApplicationCommand`, `BulkOverwriteGuildApplicationCommands`.
- **Invites:** `ListGuildInvites`, `ListChannelInvites`, `GetInvite`, `CreateInvite`, `DeleteInvite`.
- **Scheduled events:** `ListScheduledEvents`, `GetScheduledEvent`, `CreateScheduledEvent`, `ModifyScheduledEvent`, `DeleteScheduledEvent`.

Every registry entry fixes name, method, low-cardinality template, major resource, ownership, request/response codec, mutation and retry class, idempotency mode, and reconciliation strategy. Registry completeness tests reject omissions.

## Validation contract

Validation runs completely before transport:

- Snowflakes are non-zero unsigned 64-bit decimal strings with no sign or surrounding whitespace. Invite codes contain 1–32 ASCII letters, digits, `_`, or `-`. IDs in bulk and overwrite collections are unique.
- All strings are valid UTF-8 and documented character limits count Unicode code points. Channel/role names are 1–100; webhook names 1–80; message content 0–2000; command names 1–32; command descriptions 0–100; scheduled-event names 1–100; audit reasons 0–512 with no CR/LF.
- JSON requests are at most 1 MiB, success responses 8 MiB, and error responses 64 KiB. There are at most 10 attachments, 25 MiB declared per file, and 50 MiB aggregate multipart. A reader and exact declared size are required; unknown, short, or long streams fail closed. Filenames must be safe basenames and are normalized before multipart encoding.
- Messages have content, an embed, or an attachment. At most 10 embeds, 25 fields per embed, and 6000 aggregate embed characters are allowed. Bulk deletion contains 2–100 unique message IDs.
- Channel types are allowlisted. Pagination and invite ranges follow v10 (`limit` and message cursors; invite max age 0–604800 and max uses 0–100). Application commands contain at most 25 options per level and depth 3; bulk overwrite contains at most 100 commands.
- Permission strings parse as decimal API-v10 bits no greater than `2251799813685247`. Overwrite type is role or member; allow and deny cannot overlap; target IDs are unique.
- Scheduled-event start is future, end follows start, external events require a location, and voice/stage events require a channel. Enum values are allowlisted rather than passed through.
- Audit reasons are percent-encoded exactly once and only sent in `X-Audit-Log-Reason`.

Guild isolation is fail closed. Guild-qualified discovery records ownership. Unqualified channel, message, webhook, and invite routes require a known owner matching `CommandContext.GuildID`; unknown or mismatched resources return `OwnershipError` with zero requests. Deletes remove ownership.

## Idempotency and reconciliation

All unsafe creates require `IdempotencyKey` and `ReconciliationKey`, namespaced by guild and command. The canonical JSON command fingerprint excludes reader bytes but includes declared attachment metadata. Reusing a key for another fingerprint is `IdempotencyConflictError`. A mutex-backed CAS store gives one pending lease; concurrent duplicates wait, successful results replay, and expired leases can be claimed.

Create reconciliation uses stable typed fields against a pre-create baseline: channel name/type/parent, role name and properties, webhook name/channel, application-command name/type, invite stable options, and scheduled-event name/type/start. `CreateMessage` additionally derives a deterministic nonce from guild and both keys and sends `enforce_nonce=true`. Zero matches remains uncertain without another unsafe create; one match succeeds; multiple matches produce `reconcile_ambiguous`. Bulk overwrite is replacement semantics, not unsafe create replay.

`Execution[R]` reports attempts and replay (`none`, `coalesced`, `succeeded`). Unsafe failure includes `PartialResult`: `not_sent`, `sent_unknown`, `response_received`, or `reconcile_ambiguous`; completed IDs, both resume keys, and recoverability are explicit.

## Errors

- `ValidationError`: field and named rule; no request.
- `OwnershipError`: unknown or cross-guild resource; no request.
- `TransportError` and `CancellationError`: sanitized network/cancellation state.
- `BodyLimitError` and `PayloadError`: oversized or malformed/trailing JSON.
- `DiscordAPIError`: status, numeric Discord code, and flattened field paths; body text is excluded from `Error()`.
- `RetryExhaustedError`: bounded attempt budget exhausted.
- `IdempotencyConflictError` and `ReconciliationAmbiguityError`: resume safety failures.
- `InvariantError`: sealed registry/codec mismatch; no untrusted detail in `Error()`.

Errors and logs never include body text, message content, attachment bytes, tokens, webhook URLs, audit reasons, or filenames.

## Rate limits and observability

The provisional key is method + route template + major resource. Bucket hashes remap and merge while keeping majors isolated. State updates from every response. Relative reset wins; absolute reset minus server `Date` is the skew fallback. Decimal seconds and HTTP-date `Retry-After` are accepted. Global scope blocks authenticated routes; user/shared state remains bucket-local. Queue, global, and retry waits are context-cancellable. 401 opens an in-process invalid-token circuit; 403/404/429 bodies are not logged.

Telemetry emits `discord.command.<name>` and `discord.rest.attempt`. Metrics cover request count/duration/status, rate-limit waits/scope, retries/reasons, cancellation, replay, and partial failure. Attributes are restricted to command name, fixed route template, method, status class, retry reason, rate-limit scope, result class, and API version.

## Adding an endpoint safely

1. Add SDK-free domain input/result DTOs and a sealed `DiscordCommand[R]` association.
2. Add complete registry metadata: fixed method/template/major, ownership, codecs, mutation/retry/idempotency, and reconciliation.
3. Add preflight enum, ID, boundary, aggregate, cross-field, and guild-ownership validation.
4. Add exact route/query/JSON or multipart codecs and bounded required-field response decoding.
5. Add sanitized telemetry and scripted fixtures for 2xx, mapped 4xx, malformed/oversized data, 429, retry/cancellation, redaction, and concurrency as applicable.
6. Update the catalog, compatibility matrix, fixture guide, and architecture tests; run focused, race, fuzz, and repository verification.

See [ADR 0004](../adr/0004-discord-rest-transport.md) and the [fixture guide](../testing/discord-fixtures.md).
