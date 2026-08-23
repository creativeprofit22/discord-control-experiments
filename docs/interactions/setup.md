# Discord interactions setup

Discord interactions are an opt-in webhook transport for `discord-control run`; Gateway connectivity and intents remain unsupported.

## Developer Portal

1. Add the public HTTPS URL ending exactly in `/interactions` as the application's **Interactions Endpoint URL**. Discord validates it with a signed PING.
2. Copy the application's 64-character Ed25519 public key into `signingPublicKeys`. Public signing keys are not bot-token secrets.
3. Keep a bot installation for command registration and health/operation REST calls. The bot token needs the permissions required by the configured capabilities; the generated command defaults to **Manage Guild**.
4. Use `registration.scope: guild` plus `guildId` for immediate development registration. Use exactly one `global` scope in production; global propagation is controlled by Discord.
5. Install only in guild context. The manifest sets `integration_types=[guild_install]`, guild context, and disables DMs.

## Direct TLS

```json
{
  "secretEnvironmentAllowlist": ["DISCORD_BOT_TOKEN", "INTERACTIONS_CERT_PEM", "INTERACTIONS_KEY_PEM"],
  "interactions": {
    "enabled": true,
    "listenAddress": "0.0.0.0:8443",
    "path": "/interactions",
    "applicationId": "123456789012345678",
    "signingPublicKeys": ["<64 lowercase hex characters>"],
    "registration": {"scope": "guild", "guildId": "234567890123456789"},
    "tls": {
      "mode": "direct",
      "certificate": {"source": "env", "name": "INTERACTIONS_CERT_PEM"},
      "privateKey": {"source": "env", "name": "INTERACTIONS_KEY_PEM"}
    }
  }
}
```

Certificate/private-key PEM is resolved only into `tls.Certificate`, registered with the redactor, and never serialized. Terminate only modern TLS and restrict the firewall to the intended public ingress.

## Trusted reverse proxy

Set `tls.mode: trusted_proxy`, list every immediate proxy network in `trustedProxyCidrs`, and omit certificate fields. The listener accepts a request only when its direct peer is in that CIDR list and the single `X-Forwarded-Proto` value is `https`; forwarded headers from other peers do not establish trust. Restrict the backend firewall to those proxy networks.

The only public routes are `POST /interactions`, `GET /livez`, and `GET /readyz`. Health bodies are fixed `ok` or `unavailable`; there is no public metrics, pprof, CORS, directory, build, policy, key, queue, or diagnostic route.

## Policy mapping

Map each allowed Discord user explicitly as `adapter: interactions`, `subject: <Discord user ID>`, `authenticationMethod: discord-ed25519`. Grant only the guild and `guild.inspect`, `job.read`, `job.cancel`, or `job.resume` capabilities needed. Missing/ambiguous mappings receive the same generic ephemeral refusal as other unavailable resources.
