# Deterministic escalation policy

Owner: Maintainers  
Last reviewed: 2026-08-23

Escalation is explicit configuration, not model output. A policy has a stable version, bounded action caps, and priority-ordered rules. Each rule declares eligible prior action kinds, lookback, minimum count, selected action, optional duration/delete window, and maximum applications.

Rules are evaluated by ascending priority then stable rule ID. Equal-priority rules with overlapping prior kinds are rejected during configuration load. Only completed actions inside the lookback count. The result binds the policy version/hash and canonical history hash and carries a stable reason.

Evaluation never mutates Discord. Apply reloads current guild history, recomputes the recommendation, and rejects changed policy, history, selected action, or limits. The selected typed action then runs through its own capability, hierarchy, idempotency, audit, and approval controls.

Timeout recommendations cannot exceed 28 days; deletion windows cannot exceed 14 days. Per-subject and per-guild caps are mandatory. Configure reason codes separately and use one of those codes when applying a recommendation.
