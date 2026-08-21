# Contributor guide

Owner: Maintainers
Last reviewed: 2026-08-21

Use Go 1.27.0. Keep domain free of project imports, application dependent only on domain, and adapters independent. Add capability-specific ports instead of raw Discord requests or giant MCP endpoint catalogs.

Run `bash scripts/verify.sh` before review. It installs exact scanner versions into ignored `.tools/bin`, checks formatting, modules, static analysis, tests and race behavior, vulnerabilities, full-history secrets, licenses, SBOM, and reproducible hashes. For a quick pre-commit secret check use `.tools/bin/gitleaks git . --config .gitleaks.toml --log-opts=--all --no-banner --redact=100` after the first verification install.

Never commit `.env`, credentials, local config, reports, binaries, or dependency caches. Dependency updates follow [ADR 0003](../adr/0003-dependency-update-policy.md); architecture changes require tests and an ADR. Hosted CI success is distinct from local verification.

See [testing strategy](../testing/strategy.md) and [module contracts](../architecture/modules.md).
