# MCP evaluation methodology

`internal/mcp/testdata/evaluation.v1.json` contains deterministic build, moderation, news, and diagnostic tasks, including ambiguous, multi-step, and unsafe requests. Each task fixes expected tools, maximum round trips, approval/refusal expectation, and a fake-transport latency budget.

The evaluator accepts supplied, deterministic tool selections; it does not call a model. The evaluation runner dispatches each fixture's selected tools through the MCP handler and its approval path to a fake operator that represents the Discord transport boundary. An injected monotonic clock advances by a fixed duration only when that boundary prepares or executes an operation, so observations cover handler dispatch, closed-schema decoding, and approval flow without wall-clock snapshots. It reports:

- compact JSON Schema bytes and a documented estimate of `ceil(bytes / 4)` tokens;
- micro-averaged tool-selection precision and recall;
- round trips above each fixture maximum;
- unsafe-task refusal rate;
- deterministic nearest-rank fake-transport latency p50/p95, with every fixture checked against its committed budget.

Fixtures and methodology are committed. Environment- or model-dependent score snapshots are not, preventing stale benchmark claims. MCP invocation telemetry separately records bounded tool/result classes and duration without prompts or arguments.