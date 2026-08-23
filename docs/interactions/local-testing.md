# Local interaction testing

Use a test-only Ed25519 keypair and a local TLS/reverse-proxy fixture. Never copy a production signing key, bot token, TLS private key, interaction token, or signed production body into a fixture or shell history.

For each request, serialize the JSON body once, retain its exact bytes, choose a current decimal Unix timestamp, sign `timestamp || body` with the test private key, and send the lowercase hex signature as `X-Signature-Ed25519` plus the literal timestamp as `X-Signature-Timestamp`. Set `Content-Type: application/json`. Reformatting JSON after signing must produce 401.

Exercise PING, slash, component, autocomplete, and modal envelopes with the configured application ID, numeric guild/member IDs, and unique interaction IDs. Assert PONG or deferred callbacks arrive before 3 seconds, duplicates return byte-identical initial callbacks, only one deferred call occurs, wrong signatures never reach JSON/application code, and generic denial text contains no actor, guild, run, token, hash, policy reason, signature, key, or raw error.

Run:

```sh
go generate ./internal/interactions
go test ./internal/interactions ./internal/application ./internal/discord
go test -race ./internal/approval ./internal/application ./internal/interactions
go test ./...
go vet ./...
```

Generation must leave `docs/interactions/command-reference.md` and `internal/interactions/testdata/command-catalog.json` unchanged. Guild registration is recommended for local development because command changes appear immediately. Stub Discord REST and callback `RoundTripper` calls; do not direct automated fixtures at a production guild.
