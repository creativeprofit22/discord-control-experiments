# Deployment and support matrix

Owner: Operations
Last reviewed: 2026-08-25

Supported packaged targets are Linux amd64/arm64, Windows amd64, and macOS amd64/arm64. Supported transports are CLI, MCP stdio, and optional signed interaction HTTPS. Discord Gateway and remote MCP are unsupported.

## Release prerequisite

Require protected release tags, successful CI and native smoke jobs, verified checksums, build and SBOM attestations, and repository immutable releases enabled. GitHub release immutability is an operator-owned repository setting; inability to verify it blocks publishing.

## Install

1. Download one archive plus `SHA256SUMS` from the same immutable release.
2. Verify SHA-256 and `gh attestation verify ARTIFACT --repo OWNER/REPOSITORY`.
3. Extract into a new version directory; do not overwrite a running binary.
4. Keep configuration/state outside the binary directory with owner-only permissions.
5. Run `version --format json`, `health local`, and `state validate` before selecting the version.

Run one process per state directory. Stop cleanly before backup, restore, upgrade, or rollback. Export telemetry over explicit HTTPS and keep collector credentials out of configuration files.
