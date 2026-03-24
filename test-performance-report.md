# Test Performance Report

Generated: 2026-03-24
Test runner: Vitest 4.1.0 (unit config, `pnpm test:fast`)
Total duration: 1058.84s (tests: 283.56s, setup: 3729.18s)
Results: 863 passed, 106 failed, 1 skipped (970 test files)

## Top 30 Slowest Individual Tests

| #   | Duration | File                                                           | Test Name                                                                                                           |
| --- | -------- | -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| 1   | 7645 ms  | `src/plugin-sdk/index.test.ts`                                 | plugin-sdk exports > emits importable bundled subpath entries                                                       |
| 2   | 6201 ms  | `src/plugins/loader.test.ts`                                   | loadDenebPlugins > supports legacy plugins importing monolithic plugin-sdk root                                     |
| 3   | 5943 ms  | `test/scripts/check-plugin-sdk-subpath-exports.test.ts`        | check-plugin-sdk-subpath-exports > passes on the current codebase with no violations                                |
| 4   | 4569 ms  | `src/config/doc-baseline.test.ts`                              | config doc baseline > is deterministic across repeated runs                                                         |
| 5   | 2659 ms  | `src/infra/restart.test.ts`                                    | cleanStaleGatewayProcessesSync > uses explicit port override when provided                                          |
| 6   | 2006 ms  | `src/infra/restart.test.ts`                                    | cleanStaleGatewayProcessesSync > kills stale gateway pids discovered on the gateway port                            |
| 7   | 1357 ms  | `test/web-search-provider-boundary.test.ts`                    | web search provider boundary inventory > has no remaining production inventory in core                              |
| 8   | 1288 ms  | `test/extension-plugin-sdk-boundary.test.ts`                   | extension plugin-sdk-internal boundary inventory > script json output is empty                                      |
| 9   | 1219 ms  | `test/extension-plugin-sdk-boundary.test.ts`                   | extension src outside plugin-sdk boundary inventory > script json output is empty                                   |
| 10  | 1200 ms  | `test/extension-plugin-sdk-boundary.test.ts`                   | extension relative-outside-package boundary inventory > script json output matches the checked-in baseline          |
| 11  | 1151 ms  | `test/plugin-extension-import-boundary.test.ts`                | plugin extension import boundary inventory > script json output matches the baseline exactly                        |
| 12  | 945 ms   | `test/architecture-smells.test.ts`                             | architecture smell inventory > script json output matches the collector                                             |
| 13  | 922 ms   | `test/web-search-provider-boundary.test.ts`                    | web search provider boundary inventory > script json output is empty                                                |
| 14  | 781 ms   | `src/cron/isolated-agent/run.sandbox-config-preserved.test.ts` | runCronIsolatedAgentTurn sandbox config preserved > preserves default sandbox config when agent entry omits sandbox |
| 15  | 762 ms   | `test/web-search-provider-boundary.test.ts`                    | web search provider boundary inventory > produces stable sorted output                                              |
| 16  | 694 ms   | `src/cli/command-secret-gateway.test.ts`                       | resolveCommandSecretRefsViaGateway > returns config unchanged when no target SecretRefs are configured              |
| 17  | 643 ms   | `src/memory/manager.atomic-reindex.test.ts`                    | memory manager atomic reindex > keeps the prior index when a full reindex fails                                     |
| 18  | 623 ms   | `test/plugin-extension-import-boundary.test.ts`                | plugin extension import boundary inventory > keeps web-search-providers out of the remaining inventory              |
| 19  | 607 ms   | `src/tts/tts.test.ts`                                          | tts > summarizeText > summarizes text and returns result with metrics                                               |
| 20  | 604 ms   | `src/memory/manager.vector-dedupe.test.ts`                     | memory vector dedupe > deletes existing vector rows before inserting replacements                                   |
| 21  | 601 ms   | `src/tts/edge-tts-validation.test.ts`                          | edgeTTS – empty audio validation > throws when the output file is 0 bytes                                           |
| 22  | 586 ms   | `src/media-understanding/runner.vision-skip.test.ts`           | runCapability image skip > skips image understanding when the active model supports vision                          |
| 23  | 560 ms   | `test/extension-plugin-sdk-boundary.test.ts`                   | extension src outside plugin-sdk boundary inventory > produces stable sorted output                                 |
| 24  | 554 ms   | `test/extension-plugin-sdk-boundary.test.ts`                   | extension src outside plugin-sdk boundary inventory > is currently empty                                            |
| 25  | 544 ms   | `src/media-understanding/providers/image.test.ts`              | describeImageWithModel > routes minimax-portal image models through the MiniMax VLM endpoint                        |
| 26  | 522 ms   | `src/infra/outbound/agent-delivery.test.ts`                    | agent delivery helpers > builds a delivery plan from session delivery context                                       |
| 27  | 510 ms   | `src/tts/tts.test.ts`                                          | tts > summarizeText > calls the summary model with the expected parameters                                          |
| 28  | 508 ms   | `test/plugin-extension-import-boundary.test.ts`                | plugin extension import boundary inventory > produces stable sorted output                                          |
| 29  | 457 ms   | `src/tts/tts.test.ts`                                          | tts > summarizeText > validates targetLength bounds                                                                 |
| 30  | 456 ms   | `test/web-search-provider-boundary.test.ts`                    | web search provider boundary inventory > ignores extension-owned registrations                                      |

## Analysis by Category

### Plugin-SDK / Export Validation (top 3, 5.9–7.6s)

These tests dynamically import or scan all plugin-sdk subpath exports. Heavy filesystem and module resolution overhead.

### Config Doc Baseline (#4, 4.6s)

Generates config documentation deterministically — likely involves serializing the full config schema.

### Process Management (#5–6, 2.0–2.7s)

`cleanStaleGatewayProcessesSync` tests spawn/kill real processes, inherently slow.

### Boundary/Architecture Inventory (#7–13, 0.9–1.4s)

These run grep/analysis scripts over the codebase to enforce import boundaries. I/O-bound.

### Remaining (450–780ms)

Mix of TTS, memory manager, media understanding, and delivery tests — mostly due to module import overhead or filesystem operations.
