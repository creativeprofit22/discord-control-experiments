# Runtime benchmark

Owner: Maintainers  
Last reviewed: 2026-08-21

This isolated harness compares the pinned Go and TypeScript candidates before either runtime becomes the production root.

## Prerequisites

- Windows x64 target host.
- Repository-local Go 1.27.0 at `.tools/go` and Node 24.19.0 at `.tools/node-v24.19.0-win-x64`, verified against `toolchains.json`.
- pnpm 10.26.1 prepared by Corepack under `.tools/corepack`.
- Dependency caches may be warm; candidate compiler caches are reset for each measured build.

Download archives only from the official URLs and verify their SHA-256 values in `toolchains.json`. Prepare pnpm with `COREPACK_HOME=.tools/corepack .tools/node-v24.19.0-win-x64/corepack.cmd prepare pnpm@10.26.1 --activate`.

## Complete rerun

From the repository root:

```powershell
$env:PATH = "$PWD\.tools\go\bin;$env:PATH"
go run ./benchmarks/runtime/runner -output benchmarks/runtime/results/2026-08-21-windows-amd64.json
```

The command uses fixed argv arrays, refuses fixture URLs outside literal loopback, applies per-process deadlines, terminates failed process trees, and writes JSON plus Markdown. It prefetches and audits dependencies outside measured sections.

## Metrics and interpretation

1. **Cold start:** 50 fresh processes, ending at a validated readiness record after logger, Discord client, and MCP server construction.
2. **Warm REST:** five runs of 100 warmups plus 1,000 measured keep-alive requests to the random-port loopback fixture; Discord libraries decode the same typed user.
3. **MCP startup:** 50 fresh stdio processes, ending at a conforming `server/discover` response supporting `2026-07-28`; extra stdout fails the candidate.
4. **Peak memory:** externally sampled OS resident working set during every REST run.
5. **Artifact size:** uncompressed deployable bytes. Go is the stripped executable. TypeScript is compiled output, production dependencies, and the pinned portable Node runtime. App-only bytes are informational.
6. **Build time:** five full clean compiles after dependency downloads. Go receives an empty `GOCACHE`; TypeScript output is removed before each compile.

Lower is better. Each eligible candidate is normalized against the best value and weighted: memory 25%, deployable artifact 20%, cold 20%, MCP 15%, REST 10%, and build 10%. Cold, MCP, and REST use 70% median plus 30% p95. Scores within 5% select Go for the single-binary boundary.

This is a target-host comparison, not a universal language ranking. Antivirus, power settings, filesystem cache, and background load can alter results. Loopback REST excludes Discord network latency and cannot predict live response-driven rate limiting.
