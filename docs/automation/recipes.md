# Automation recipes

Owner: Maintainers<br>
Last reviewed: 2026-08-23

Workflow files are strict JSON. Replace example IDs with configured guild/channel/role bindings and grant the automation policy actor only required capabilities.

## Scheduled message

```json
{"version":1,"id":"daily-note","enabled":true,"trigger":{"kind":"calendar","time":"09:00"},"targets":{"guildIds":["123456789012345678"]},"retry":{"maxAttempts":3,"initial":"2s","maximum":"1m"},"steps":[{"action":{"kind":"message","channelId":"223456789012345678","content":"Daily stand-up starts now."}}]}
```

## Reviewed release news

Configure source `releases`, then use:

```json
{"version":1,"id":"release-news","enabled":true,"trigger":{"kind":"news","source":"releases","limit":10},"targets":{"guildIds":["123456789012345678"]},"retry":{"maxAttempts":3,"initial":"2s","maximum":"1m"},"steps":[{"transforms":[{"kind":"render","template":"{title}\n{text}\n{link}"}],"action":{"kind":"message","channelId":"223456789012345678"}}]}
```

Add `{"kind":"summarize","maxBytes":1200}` and `"requireReview":true` to use configured AI summaries.

## Weekly forum thread or poll

Use a weekly calendar trigger and `forum_thread` with static `channelId`, `title`, and `content`. For a poll, use `poll` with `title`, 2–10 `choices`, and optional content. Forum/media and poll content disables mentions.

## Welcome and role automation

Use `member_scan`. The first successful scan records a baseline and sends nothing. Later members run `welcome` and/or `role_add`; dynamic member identity comes only from the scan record. Static role IDs remain policy-checked per guild.

## Cleanup and snapshots

Cleanup requires `age`, `limit`, `requireReview:true`, an allowlisted `automation.cleanupChannels` binding, destructive policy, and a fresh approval. The action lists a bounded page, ignores pinned or newer messages, then deletes no more than its limit. Use `snapshot` for a policy-governed reconciliation snapshot without a Discord mutation.

## Cross-guild rollout

List explicit guild IDs in `targets.guildIds`. Each child runs independently through policy. A denied guild records failure without granting another guild access. Retry skips already completed guild children. Keep configured guild/action/destructive ceilings below the largest accidental blast radius you can inspect quickly.
