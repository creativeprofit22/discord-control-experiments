# Security model

Owner: Security and maintainers
Last reviewed: 2026-08-21

## Assets and boundaries

Discord bot tokens, webhook/callback tokens, TLS private keys, actor grants, approvals, audit integrity, guild state, user/message content, and telemetry are protected assets. Inputs from CLI/config, environment, repository files, MCP/model output, interactions, Gateway events, jobs, Discord responses, dependencies, and exporter destinations are untrusted until validated at their boundary.

| Source | Validation and authority boundary | Permitted sink |
|---|---|---|
| CLI/config JSON | bounded UTF-8, duplicate/unknown-key rejection, strict enums, exact env allowlist, contained paths | composition settings and opaque secret handles |
| local MCP/model | exact 2026-07-28 bounded stdio; fixed process principal/guild; policy/toolset/read-only discovery; closed schemas; model fields never authorize | `OperatorService` invocation only |
| signed interactions | exact method/path/TLS-proxy checks; bounded untouched body; timestamp+body Ed25519 verification before strict JSON; dedup; signed user/guild principal | interaction application facade, bounded worker, fixed Discord callback only |
| Gateway/jobs | Gateway remains uncomposed; automation uses closed workflows, constrained egress, persisted leases/journals, and typed application ports | same application chokepoint |
| feeds/model output | configured HTTPS sources, DNS/address/redirect/TLS/content/size limits, plain rendering, no model tools, reviewed operation hash | bounded data and reviewed Discord command only |
| policy rules | exact principal→actor compilation; explicit guilds/capabilities/mode; no wildcard | pure typed decision only |
| approval administration | authenticated human boundary, not a model tool; actor/hash/TTL binding | atomic in-memory consume port |
| audit | allowlisted minimized event, dynamic redaction, bounds, append/sync/hash chain | contained restrictive JSONL plus head |
| environment secret | resolved and redactor-registered immediately before use | narrow Discord transport callback |
| telemetry | low-cardinality allowlists and dynamic redaction | configured logger/trace/metric exporter |
| Discord command | sealed registry, resource extraction/ownership, fixed route/codec; configured webhook secrets resolve only inside transport | fixed TLS API-v10 origin only |

Automation's SSRF, injection, duplicate-effect, and AI controls are detailed in [sources and AI](../automation/sources-and-ai.md), [runtime recovery](../automation/runtime.md), and [operations](../automation/operations.md).

## Threats and controls

**confused deputy and forged identity:** adapters provide evidence, not actor IDs or grants. Exact principal mapping, guild/capability checks, operator mode ceilings, and runtime authorization prevent command/model fields from elevating authority. Unknown, ambiguous, malformed, erroring, or panicking policy paths deny.

**Cross-guild substitution:** every command exposes typed resource references. Known conflicting owners and unknown unqualified owners deny before transport. Ownership records arise only from guild-scoped discovery/create responses.

**Signed replay/body mutation:** signatures cover the literal timestamp header plus untouched body; skew and atomic interaction-ID/body-digest claims reject stale/reused deliveries. Dedup is process-local and capacity fails closed without live eviction.

**Approval replay or fatigue:** mandatory destructive classes consume one random grant bound to actor, guild, scope kind, and canonical operation/plan hash. MCP form request state additionally binds canonical arguments, tool, actor, guild, plan, and a two-minute expiry and is consumed on every terminal attempt. Expiry, mismatch, replay, restart, or storage error denies. Dry runs do not consume. No issuance tool exists; exact narrow workflows avoid wildcard or bundled approval.

**Secret disclosure:** config stores references/handles, Discord stores a use callback rather than plaintext, and each value is registered before request construction. Telemetry/audit schemas avoid content; dynamic redaction is defense in depth and emits interventions. Crash output is generic. Rotation uses separate environment credentials and restart verification.

**Audit fail-open/privacy:** a durable decision is required before Discord access. Completion failure after send alerts while preserving the pending precommit. Audit records omit command JSON, content, names, files, payloads, URLs, subjects, and secret values. Containment, restrictive permissions, record bounds, sequence, hash chain, and head verification detect many changes; they do not stop a privileged host operator deleting both files or provide non-repudiation.

**Public listener/callback abuse:** direct TLS or allowlisted trusted proxy, exact routes, header/body/concurrency/queue limits, panic containment, generic ephemeral errors, and private health output bound exposure. Callback tokens stay in short-lived work, and egress cannot select another origin/path or follow redirects.

**Transport and supply chain:** there is no arbitrary request escape hatch. Discord uses a fixed HTTPS origin, bounded bodies, exact multipart, typed routes, retries, and dynamic rate limits. CI checks format, module integrity, static analysis, vulnerabilities, history/current-tree secrets, licenses, SBOM, and reproducible builds; these checks reduce specific risks but do not establish absence of defects.

## Fail-closed sequence

`dispatchDiscord` validates command/invocation/registry/resources, evaluates policy with panic recovery, hashes the operation, consumes approval for destructive apply, appends audit precommit, enforces mode, calls Discord only in apply, and appends completion for every result. Internal baseline and reconciliation inherit the authenticated principal and use linked child correlations through the same chokepoint. Policy, approval, or audit-precommit errors make zero Discord calls. Telemetry errors never grant or deny authority.

## Exposure limits

Remote MCP, Streamable MCP HTTP, a remote human approval UI, tenant isolation beyond explicit guild/resource grants, an external immutable audit ledger, automatic retention deletion, live Discord permission probing, and raw lockdown APIs are not implemented. Remote MCP and remote approval issuance require separate authenticated adapters and threat-model review. The bot must not receive Discord `ADMINISTRATOR` by default.

Report concerns through [SECURITY.md](../../SECURITY.md). Review [capability policy](../reference/capability-policy.md), [Discord permissions](../reference/discord-permissions.md), [approval model](../reference/approval-model.md), [audit retention](../reference/audit-retention.md), [secret rotation](../runbooks/secret-rotation.md), and [incident response](../runbooks/security-incident.md).
