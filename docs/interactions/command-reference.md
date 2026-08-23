# `/discord-control` command reference

Generated from `internal/interactions/commands.go`; do not edit manually.

- `health` — check local and Discord health.
- `policy capability:<choice>` — show the caller's effective decision.
- `jobs list` — list this guild's durable jobs.
- `jobs status run_id:<id>` — inspect one job.
- `jobs cancel run_id:<id>` — prepare a one-time cancellation confirmation.
- `jobs resume run_id:<id>` — review and approve a recoverable resume.

The command is guild-install and guild-context only. Default member permission: Manage Guild.
