# Local MCP setup

The MCP adapter is local stdio only. It uses `github.com/modelcontextprotocol/go-sdk v1.7.0` and accepts protocol `2026-07-28` only.

Configure one guild, an explicit toolset allowlist, and a read-only actor for the fixed local MCP principal:

```json
{
  "mcp": {
    "enabled": true,
    "guildId": "123456789012345678",
    "mode": "read_only",
    "toolsets": ["diagnostic"],
    "readOnly": true,
    "maxMessageBytes": 1048576,
    "maxConcurrentCalls": 4,
    "callTimeout": "30s"
  },
  "policy": {
    "mode": "read_only",
    "actors": [
      {
        "id": "mcp-reader",
        "principals": [
          {
            "adapter": "mcp",
            "subject": "local-stdio",
            "authenticationMethod": "os-process"
          }
        ],
        "guilds": ["123456789012345678"],
        "capabilities": ["guild.inspect", "channel.read", "role.read"],
        "mode": "read_only"
      }
    ]
  }
}
```

The principal mapping and guild scope are required. Missing scope, actor mapping, unknown toolsets, invalid limits, policy errors, and empty inventories stop startup.

Local-client configuration keeps the Discord token in the environment rather than process arguments:

```json
{"mcpServers":{"discord-control":{"command":"discord-control","args":["run","--config","C:/Users/me/.config/discord-control/config.json"],"env":{"DISCORD_BOT_TOKEN":"${DISCORD_BOT_TOKEN}"}}}}
```

Stdout is protocol-only. Diagnostics and telemetry go to stderr after redaction. Closing client stdin ends the session and stops the application lifecycles.