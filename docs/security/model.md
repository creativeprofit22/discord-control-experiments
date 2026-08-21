# Security model

Owner: Security and maintainers
Last reviewed: 2026-08-21

## Assets and trust boundaries

Discord credentials, policy decisions, MCP tool calls, interaction/Gateway payloads, config bytes, environment values, repository content, dependency code, and telemetry destinations cross trust boundaries. Credentials must not reach logs, traces, metrics, diagnostics, source, history, or CI artifacts.

## Controls

Configuration is bounded and strict; environment names are allowlisted; secrets redact every standard representation. Policy defaults deny. Adapters expose no arbitrary Discord request escape hatch and cannot import each other. MCP stdout is protocol-only; trace context is bounded and format-validated but never authorizes. Jobs are cancellable and concurrency-bounded. OTLP requires explicit HTTPS and default startup has no network egress.

CI scans full history for secrets, reviews dependency changes, checks known Go vulnerabilities and static findings, validates licenses, emits an SBOM, and compares deterministic build hashes. These checks reduce specific risks; they do not establish absence of defects.

## Out of scope now

Remote MCP, tenant isolation, production deployment identity, user data retention, and live Discord permissions are not implemented. They require explicit threat-model updates before exposure.

Report concerns through [SECURITY.md](../../SECURITY.md). Operate through the [operator guide](../operations/operator-guide.md).
