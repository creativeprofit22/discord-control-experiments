# Configuration reference

Owner: Maintainers
Last reviewed: 2026-08-21

Configuration is UTF-8 JSON capped at 65,536 bytes and validated before startup against [schema v1](../../config/schema/v1.schema.json). Unknown fields, duplicate keys, trailing JSON, missing fields, unsupported versions, and invalid exporter combinations fail closed.

Use [the committed example](../../config/example.v1.json). The only supported secret reference is `{"source":"env","name":"DISCORD_BOT_TOKEN"}`. Inline values, files, commands, arbitrary environment names, and remote secret URLs are unsupported. Missing or empty environment values stop startup.

`observability.exporter` is `none` (default local no-egress behavior), `stderr`, or `otlp-http`. OTLP requires an explicit HTTPS endpoint without credentials, query, or fragment. Diagnostics expose only config version, enabled adapters, and exporter name. A resolved `Secret` formats, logs, and serializes as `[REDACTED]`.

See the [operator guide](../operations/operator-guide.md) and [startup runbook](../runbooks/startup-failure.md).
