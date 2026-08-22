# MCP client compatibility

The server is pinned to MCP protocol `2026-07-28` and Go SDK `v1.7.0`. Unsupported revisions are rejected rather than negotiated to another security model. The transport is local stdio; Streamable HTTP is intentionally absent.

Read-only, dry-run, and non-destructive calls work with clients implementing 2026-07-28 tools over newline-delimited stdio. Interactive destructive apply additionally requires 2026-07-28 multi-round-trip form elicitation. Clients without it receive a plan/refusal and never an implicit downgrade.

The official `modelcontextprotocol/conformance` server runner currently accepts an HTTP URL and cannot launch this stdio-only server directly. Tests therefore pin the official 2026-07-28 schema revision/checksum and run a black-box stdio harness. This is not a claim that the upstream HTTP runner was executed.