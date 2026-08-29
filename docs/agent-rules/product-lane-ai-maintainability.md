---
description: "Grounded AI navigation anchors for non-Go product lanes"
globs: ["client-android/**", "andromeda/**", "scripts/**/*.py"]
---

# Product Lane AI Maintainability

Use this when changing product-relevant Kotlin, TypeScript, or Python surfaces. The
goal is local change evidence: a maintainer should be able to name the owning entry
point, the boundary invariant, and the focused verification command before editing.

## Kotlin Native Client

- Source anchors: `client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/AppRoot.kt`,
  `client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/deneb/DenebGatewayClient.kt`,
  `client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/deneb/DenebGatewayTransport.kt`.
- Entrypoints: `App`, `DenebGatewayClient`, `readSseLines`, `sendGatewayChat`,
  `streamGatewayChat`.
- Tests: `client-android/app/composeApp/src/commonTest/kotlin/ai/deneb/AppRouteSerializationContractTest.kt`,
  `client-android/app/composeApp/src/commonTest/kotlin/ai/deneb/deneb/GatewayChatTransportContractTest.kt`,
  `client-android/app/composeApp/src/commonTest/kotlin/ai/deneb/deneb/GatewayRuntimeEndpointContractTest.kt`.
- Verify: `make ci ARGS=--kotlin`; for a visual or navigation change also run
  `scripts/dev/native-app-smoke.sh`.
- Boundary invariant: `DenebGatewayClient` is the production `DataRepository`
  implementation for the native app. The client talks to the gateway through
  `miniapp.*` RPC and generated wire models; never hand-edit
  `client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/deneb/generated/MiniappWireTypes.kt`.
  When a Go `//deneb:wire` contract changes, run `make kotlin-models-check`.

## TypeScript Desktop Workstation

- Source anchors: `andromeda/src/gateway.ts`, `andromeda/src/resources.ts`,
  `andromeda/src/WorkspaceProvider.tsx`, `andromeda/src/components/panes/index.ts`.
- Entrypoints: `gatewayFetch`, `callRpc`, `RESOURCE_DEFS`, `WorkspaceProvider`.
- Tests: `andromeda/src/gateway.behavior.test.ts`, `andromeda/src/resources.test.ts`,
  `andromeda/src/WorkspaceProvider.test.tsx`, `andromeda/src/App.test.tsx`.
- Verify: `cd andromeda && pnpm verify`; for a narrow loop use
  `cd andromeda && pnpm test`.
- Boundary invariant: `gateway.ts` owns gateway HTTP/RPC/SSE transport,
  `resources.ts` owns resource to RPC mapping, and panes register AI-readable
  text through `WorkspaceProvider`. Do not hand-edit `andromeda/src/gen/miniappWire.ts`;
  after Go wire changes run `cd andromeda && pnpm gen:wire`.

## Python Audit And Ops Scripts

- Source anchors: `scripts/audit/health_finding_miner.py`,
  `scripts/audit/runtime_health.py`, `scripts/audit/doc_ref_lint.py`.
- Entrypoints: `structural_candidates`, `runtime_candidates`,
  `pending_impact_observations_for`, `parse`, `compute`, `lint`.
- Tests: `scripts/audit/test_health_finding_miner.py`,
  `scripts/audit/test_runtime_health.py`, `scripts/audit/test_doc_ref_lint.py`.
- Verify: `make python-test`, `make python-lint`, `make health-v3-test`,
  `python3 scripts/audit/health-bench-v3.py --format json`.
- Boundary invariant: audit Python must remain deterministic and importable. It may
  read git history, checked-in source, fixtures, and local logs, but it must not
  write baselines, snapshots, JSONL ledgers, or dispatch state unless the command
  is explicitly a documented write path with operator approval.

## Python Eval Harnesses

- Source anchors: `scripts/eval/reader_tool_eval.py`, `scripts/eval/depth_probe.py`.
- Entrypoints: `parse_directive`, `resolve_ref`, `render_window`, `search_sessions`,
  `serve_directive`, `parse_verdict`, `summarize`.
- Tests: `scripts/eval/test_reader_tool_eval.py`.
- Verify: `make python-test`, `make python-lint`. An end-to-end run needs the
  wormhole and a retrieval snapshot; see the module docstring.
- Boundary invariant: an eval harness IS the measurement instrument, so the half
  that decides a score must be pure and pinned by tests — protocol parsing, ref
  resolution, window and search rendering, verdict parsing, aggregation. Only
  model I/O may be non-deterministic. Two rules follow, both learned by losing
  runs: a harness must never score an ungraded question as WRONG
  (`parse_verdict` returns a separate `UNSCORED`, surfaced in the summary —
  folding it into INCORRECT silently deflates every number the run reports),
  and it must refuse a model the wormhole is not serving rather than let a dead
  pin route somewhere else. Harnesses live in the repo, never only in `/tmp`:
  the previous reader harness produced the whole 44.5 to 77.0 ladder and was
  lost with a tmp sweep.
