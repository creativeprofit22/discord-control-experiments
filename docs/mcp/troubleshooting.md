# MCP troubleshooting

| Symptom | Action |
|---|---|
| Startup rejects MCP configuration | Set one numeric `guildId`, one or more known toolsets, valid bounds, and a matching MCP policy principal. |
| No tools are discovered | Grant the fixed MCP actor at least one selected toolset capability for the configured guild/mode; empty catalogs fail startup. |
| Destructive call returns `approval_required` | Use a client with 2026-07-28 form elicitation, accept the current form before expiry, and retry unchanged. |
| Approval is invalid | Start again; state is one-time and is consumed on rejection, mismatch, expiry, or replay. |
| Session stops | Check sanitized stderr for a generic framing/lifecycle diagnostic; stdin EOF intentionally terminates the process. |
| Message is rejected | Send exactly one JSON-RPC value per line under `maxMessageBytes`; multiline, malformed, blank, and oversized frames are denied. |
| Call is cancelled or limited | Reduce concurrent calls or increase the bounded timeout within the documented configuration limits. |
| Client cannot connect | Confirm it supports protocol `2026-07-28` over local stdio; older revisions and HTTP transports are unsupported. |

Never paste bot tokens, authorization headers, webhook secrets, full approval state, or raw configuration into diagnostics. Stdout must be treated as MCP protocol, not a log stream.