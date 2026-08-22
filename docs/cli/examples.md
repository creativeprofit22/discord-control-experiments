# Examples

```console
discord-control version --format json
discord-control inspect --config config.json --guild 123 --format json
discord-control message create --config config.json --guild 123 --channel 456 --input - --non-interactive true --format json
```

Complex or sensitive payloads use `--input` files or the explicit `-` stdin sentinel. Tokens, webhook URLs, message bodies, attachments, and approval secrets are never argv flags.
