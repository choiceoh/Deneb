---
description: "프로젝트 구조 및 모듈 아키텍처 참조"
---

# Project Structure & Module Organization

## Top-Level Directory Map

- `gateway-go/` — Go gateway server (HTTP + SSE server, RPC dispatch, session management, chat/LLM, tools, auth). The primary runtime.
- `client-android/` — Kotlin Multiplatform native client (Compose). **Mobile surface** (Android daily driver + iOS); the desktop product UI was retired — Andromeda owns desktop. The Compose Desktop target remains as a headless verification harness only (`docs/agent-rules/native-live-app.md`).
- `andromeda/` — desktop workstation client ("work-command cockpit") for the gateway. Tauri 2 (Rust shell) + React 18 + Refine + Vite (TypeScript). Own gate: `cd andromeda && pnpm verify`; own module guide: `andromeda/CLAUDE.md`.
- `even-g2/` — Even Realities G2 Glance plugin (Even Hub). Not a full client — HUD companion. Agent ingress is gateway `internal/runtime/evenapi` (Custom AI bridge → `glasses:main`). See `even-g2/README.md` and `docs/research/even-g2-deneb-integration.md`.
- `skills/` — user-facing skill plugins organized by category (coding/, productivity/, knowledge/, …). Filesystem-discovered; adding a directory is the install.
- `docs/` — Mintlify documentation site.
- `scripts/` — build, dev, CI, audit, and release scripts.
- `.github/` — CI workflows, custom actions, issue/PR templates, labeler, CODEOWNERS.
- `Makefile` — build orchestration (Go + Kotlin gates; `make ci` does **not** cover `andromeda/` — that lane is `pnpm verify` + `.github/workflows/andromeda-ci.yml`).
- Tests: Go tests `*_test.go` (colocated); Kotlin tests under `client-android/app/composeApp/src/*Test`; Andromeda tests via Vitest.

## Development tools (multi-agent environment)

The repo supports parallel coding agents with shared infrastructure:

- **CodeGraph** (`.codegraph/`, gitignored) — SQLite symbol graph (~55K nodes) powering the `codegraph_explore` MCP tool. Auto-synced after edits via PostToolUse hooks. Config in `codegraph.json`; setup: `npm i -g @colbymchenry/codegraph && codegraph init`.
- **rpcmap** (`scripts/dev/rpcmap.py`) — fills CodeGraph's blind spot: string-keyed indirection (RPC method → handler, tool name → handler, event name → event type). ~270 mappings (`rpcmap --list`).
- **Worktree isolation** — each agent gets its own worktree (`~/.zcode/`, `~/.codex/`, `~/.cursor/worktrees/Deneb/`, or Claude's `EnterWorktree`) on a namespaced branch to prevent collisions. Guards block main-checkout edits.
- **Hook pipeline** — PreToolUse hooks (conflict detection via `clash`, rule guidance, CodeGraph nudges) and PostToolUse hooks (index sync) are shared across Claude Code, ZCode, Codex, and Cursor. Configs: `.claude/settings.json`, `.zcode/config.json`, `~/.codex/hooks.json`, `.cursor/hooks.json`.
- Full guide: [docs/tools/zcode-environment.md](../tools/zcode-environment.md).

## Gateway Module Map (`gateway-go/internal/`, one-liners)

Detailed per-file notes live in `docs/agent-rules/go-gateway.md`; this is the top-level orientation map.

- `runtime/` — server (HTTP/SSE), rpc (dispatch), bootstrap (4-phase startup), events (pub/sub), nativeapi, proactive, insights, modelpicker, briefcase, ….
- `pipeline/` — chat (LLM turn pipeline + tools), compaction + polaris (context compression / session DAG store), pilot (helper-LLM calls), chatport, autoreply, liteparse, compactuner.
- `ai/` — agent (LLM 도구 루프 executor — 병렬 read-only 턴·spillover·loop detector), llm (wire types), modelrole/modelcaps (role→model registry, capabilities), modeltuner, router (effort routing), provider, localai, embedding, observatory, regressionwatch, tokenest.
- `domain/` — business domains: session (lifecycle), wiki, backup, mailpriority, knowledge, filestore, notebook, skills, push, contacts, org, approval, market, monitoring, nativesync, usage, autonomous, goals, briefcase, ….
- `platform/` — external-system clients: gmail/mailanalysis(구 gmailpoll), calendar/calprop/localcal, localtodo, mailarchive/mailbody/mailwork, lmtpd (LMTP intake), cron, media.
- `infra/` — clientauth (native-client token auth), config, secret, process (exec + approval), logging, metrics, middleware, httpretry, timeouts, fileshare, shortid, sparkfleet.
- `hanja/` — Han→Korean transliteration. `core/`, `testutil/`, `codegen/`, `eval/` — shared helpers, generators, eval harnesses.

## Key Architectural Flows

1. **Gateway startup:** `gateway-go/cmd/gateway/main.go` -> `internal/runtime/server` (HTTP + SSE) -> `internal/runtime/rpc` (dispatch) -> `internal/domain/session` (state). There is no channel plugin (the Telegram bot was retired in PR #1922).
2. **Clients:** both the native client (`client-android/`) and Andromeda (`andromeda/`) talk to the gateway over the `miniapp.*` RPC surface (`POST /api/v1/miniapp/rpc`, `X-Deneb-Client-Token`) plus SSE streams. Wire types for both are generated from Go `//deneb:wire` structs (`docs/agent-rules/generated-code.md`).

## Cross-Cutting Concerns

- When adding a new module or doc area, update `.github/labeler.yml` and create matching GitHub labels.
