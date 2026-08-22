# MCP destructive approvals

In `apply` mode, destructive tools first call the same `PrepareDestructive` path used by CLI. Policy-preapproved plans issue a one-time grant through `ApprovalCoordinator` and execute normally.

Otherwise, a client advertising form elicitation receives a required `confirm: true` form and opaque random request state. State expires after two minutes and is bound to the exact canonical arguments, operation plan/hash, actor, tool, and guild. Accept, rejection, expiry, mismatch, or any retry consumes it; replay is denied. The resulting grant uses `ConfirmationInteractive` and is consumed by the normal secured service.

Clients without form elicitation receive an `approval_required` plan and no mutation. Changed arguments require a new plan. Approval IDs are never exposed in tool arguments, structured results, text, logs, or metrics.