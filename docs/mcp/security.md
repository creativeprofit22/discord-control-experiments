# MCP security boundary

The client, model, arguments, MCP metadata, elicitation responses, and Discord response strings are untrusted. The adapter owns protocol translation and fixed actor context only; every operation crosses `OperatorService` and the secured application service before the sole Discord REST adapter.

Controls:

- one configured guild; model arguments cannot set guild, identity, actor, adapter, capability, mode, correlation, idempotency, or approval IDs;
- closed bounded JSON Schemas and strict duplicate/unknown-key rejection before sealed command decoding;
- no URL, upload reader, arbitrary `any`, raw method/path/script, token, or authorization input surface;
- policy-scoped discovery plus execution-time policy and ownership checks;
- bounded newline messages, concurrency, call timeouts, cancellation, serialized writes, and EOF shutdown;
- recursively bounded outputs and stable errors that never serialize wrapped errors or configuration;
- stderr redaction and bounded telemetry attributes without arguments/content/secrets.

Read-only and tool annotations do not replace authorization. Client metadata can reduce capability but cannot expand configured identity, policy mode, Discord ownership, or Discord permissions.