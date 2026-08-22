# JSON API v1

Every invocation emits one `discord-control.cli/v1` envelope. Field order is `apiVersion`, `kind`, `correlationId`, then `result` or `error`, followed by optional `timing` and `recovery`. JSON writes one document to stdout; sanitized diagnostics and logs use stderr. Within v1, only additive optional fields are compatible. Removals, renames, type changes, enum semantic changes, and changed omission behavior require a new API version.
