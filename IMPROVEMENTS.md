# Improvement Suggestions

Codebase analysis and prioritized improvement directions for OpenClaw.

## 1. Code Quality: Reduce `as any` Casts (High Priority)

**Current state:** 112 `as any` casts across the codebase, with gateway files being top offenders.

**Key files to address:**

- `src/gateway/tools-invoke-http.ts` (4 casts)
- `src/gateway/server.auth.shared.ts` (2 casts)
- Various gateway test files

**Recommendation:**

- Replace `as any` with proper type assertions or generic parameters
- Re-enable `typescript/no-unsafe-type-assertion` in `.oxlintrc.json` to prevent new occurrences
- Start with production code paths (gateway, agents), then tackle test files

## 2. Large File Refactoring (High Priority)

Several files exceed the 500-700 LOC guideline significantly:

| File                                           | LOC   | Suggested Action                                                  |
| ---------------------------------------------- | ----- | ----------------------------------------------------------------- |
| `src/agents/pi-embedded-runner/run/attempt.ts` | 2,796 | Split into execution phases (setup, run, cleanup, error handling) |
| `src/commands/doctor-config-flow.ts`           | 2,114 | Extract per-provider config flows into separate modules           |
| `src/memory/qmd-manager.ts`                    | 2,066 | Extract indexing, querying, and lifecycle into sub-modules        |
| `src/context-engine/lcm/src/engine.ts`         | 1,919 | Split context resolution from engine lifecycle                    |

**Priority:** `attempt.ts` is the largest source file and a core execution path. Splitting it would improve testability and readability.

## 3. Resolve Stale TODOs (Medium Priority)

Active code TODOs that should be triaged:

- **`src/gateway/server-plugins.ts:49`** — Startup snapshot can become stale if runtime config changes. This is a potential bug source; consider adding invalidation or refresh logic.
- **`src/acp/translator.ts:827`** — Waiting for ChatEventSchema structured errorKind field. Track or implement.
- **`src/tts/tts-core.ts:476`** — Restore logic pending Ollama provider re-addition. Remove or implement.
- **`src/agents/pi-embedded-runner/compact.ts:883`** — Issue #7175 consideration. Decide and close.

## 4. Test Coverage Gaps (Medium Priority)

**Skipped tests to re-evaluate:**

- `src/memory/index.test.ts` — 3 skipped tests for embedding/hybrid search. These may represent untested production features.
- `src/agents/pi-embedded-runner.bundle-mcp.e2e.test.ts` — Skipped MCP integration tests.
- `src/secrets/resolve.test.ts` — Conditional skips that may hide regressions.

**Large test files to split:**

- `src/plugins/loader.test.ts` (3,725 LOC) — Split by plugin lifecycle phase
- `src/security/audit.test.ts` (3,628 LOC) — Split by audit category

**Recommendation:** Run `pnpm jscpd` to identify duplicate test patterns that could be extracted into shared test helpers.

## 5. Startup Performance (Medium Priority)

Recent changelog entries show ongoing work on lazy-loading and startup latency. Additional opportunities:

- **Plugin discovery:** The gateway eagerly loads all bundled plugins. Consider a manifest-based approach where plugin metadata is read from static JSON without executing plugin code until needed.
- **Provider loading:** Multiple recent fixes addressed eager provider loading. Audit remaining eager imports with `--trace-imports` or similar tooling to find remaining heavy startup paths.
- **CLI cold start:** Profile `openclaw --help` and `openclaw config` paths to ensure they don't trigger unnecessary module loads.

## 6. Plugin SDK Surface Hardening (Medium Priority)

The plugin SDK exposes 50+ subpaths. Suggestions:

- **Versioning:** Consider semver-independent SDK version tracking so plugin authors know which SDK features are available.
- **Deprecation policy:** Add explicit `@deprecated` JSDoc tags and runtime warnings for SDK paths being phased out, with a documented migration timeline.
- **Type-only exports:** Where possible, export types separately from runtime code to reduce plugin bundle sizes.

## 7. Security Hardening (Ongoing)

Recent security work is thorough. Additional suggestions:

- **Webhook signature verification:** Ensure all channel webhook handlers use constant-time comparison (Feishu was recently fixed; audit remaining channels).
- **Secret rotation:** The `device.token.rotate` hardening is good. Consider adding automated rotation reminders or expiry for long-lived tokens.
- **Dependency audit:** With 200+ dependencies, run `pnpm audit` regularly and consider adding it to CI if not already present.

## 8. Developer Experience (Low Priority)

- **Error messages:** Standardize error message format across CLI commands for consistent troubleshooting.
- **Debug logging:** Add structured logging levels so `--verbose` output is useful without being overwhelming.
- **Extension template:** Provide a `create-openclaw-extension` scaffold command to reduce boilerplate for new plugin authors.

## 9. Documentation Gaps (Low Priority)

- **Architecture diagram:** Add a high-level architecture diagram showing gateway, channels, agents, and plugin boundaries.
- **Plugin SDK reference:** Auto-generate API docs from TypeScript types for the 50+ SDK subpaths.
- **Troubleshooting guide:** Consolidate common error patterns from GitHub issues into a searchable troubleshooting doc.

## 10. Observability (Low Priority)

OpenTelemetry integration exists. Suggestions:

- **Structured metrics:** Add request latency, token usage, and error rate metrics per provider and channel.
- **Health dashboard:** Expose a `/health` endpoint with structured JSON for monitoring tools.
- **Trace correlation:** Ensure trace IDs propagate through subagent execution chains for end-to-end debugging.
