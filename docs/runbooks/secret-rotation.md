# Secret rotation

Owner: Security and operators
Last reviewed: 2026-08-21

Production rotation is restart-based for environment references. Use separate bot tokens and webhook URLs per environment; never copy production credentials into development, tests, config, arguments, issue text, or chat.

## Bot token

1. Create or reset the Discord bot token in the Discord developer control plane; place the new value in the exact allowlisted `DISCORD_BOT_TOKEN` environment binding.
2. Restart `discord-control`; secret handles resolve per use and register the current value with the redactor before transport receives it.
3. Verify process health, one explicitly authorized read, one policy denial, audit decision/completion records, and no redaction/audit alerts.
4. Revoke the previous token immediately after verification. If verification fails, stop the process rather than restoring broad credentials.

## Webhook URL

1. Rotate the webhook in Discord, update its exact allowlisted environment reference, and restart.
2. Verify only the intended named webhook in the intended guild/channel. Webhook URLs are bearer secrets and must never enter command DTOs, diagnostics, audit intent, or telemetry.
3. Delete the old webhook and review Discord webhook activity.

## Suspected leak

Stop affected apply-mode processes; revoke the bot token and webhook immediately; preserve audit JSONL, `.head`, logs, process metadata, and Discord audit logs; search current tree and full history without copying the secret into a command line; issue environment-separated replacements; then follow the [security incident runbook](security-incident.md). A committed or exported credential is treated as already used.

Automated tests prove mutable provider rotation without reconstructing the Discord client abstraction. They do not replace the production restart, authorization, and revocation checks.

See [configuration](../reference/configuration.md) and [security model](../security/model.md).
