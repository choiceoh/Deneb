---
description: "프로젝트 구조 및 모듈 아키텍처 참조"
globs: ["gateway-go/cmd/**", "gateway-go/internal/**", "gateway-go/pkg/**", "client-android/**", "andromeda/**"]
---

# Project Structure & Module Organization

## Top-Level Directory Map

- `gateway-go/` — Go gateway server (HTTP + SSE server, RPC dispatch, session management, chat/LLM, tools, auth). The primary runtime.
- `client-android/` — Kotlin Multiplatform native client (Compose). **Mobile surface** (Android daily driver + iOS); the desktop product UI was retired — Andromeda owns desktop. The Compose Desktop target remains as a headless verification harness only (`.claude/rules/native-live-app.md`).
- `andromeda/` — desktop workstation client ("work-command cockpit") for the gateway. Tauri 2 (Rust shell) + React 18 + Refine + Vite (TypeScript). Own gate: `cd andromeda && pnpm verify`; own module guide: `andromeda/CLAUDE.md`.
- `skills/` — user-facing skill plugins organized by category (coding/, productivity/, knowledge/, …). Filesystem-discovered; adding a directory is the install.
- `docs/` — Mintlify documentation site.
- `scripts/` — build, dev, CI, audit, and release scripts.
- `.github/` — CI workflows, custom actions, issue/PR templates, labeler, CODEOWNERS.
- `Makefile` — build orchestration (Go + Kotlin gates; `make ci` does **not** cover `andromeda/` — that lane is `pnpm verify` + `.github/workflows/andromeda-ci.yml`).
- Tests: Go tests `*_test.go` (colocated); Kotlin tests under `client-android/app/composeApp/src/*Test`; Andromeda tests via Vitest.

## Gateway Module Map (`gateway-go/internal/`, one-liners)

Detailed per-file notes live in `.claude/rules/go-gateway.md`; this is the top-level orientation map.

- `runtime/` — server (HTTP/SSE), rpc (dispatch), session (lifecycle), bootstrap (4-phase startup), events (pub/sub), process (exec + approval), insights, observe.
- `pipeline/` — chat (LLM turn pipeline + tools), compaction/polaris (context compression), pilot (helper-LLM calls), chatport, autoreply, liteparse, compactuner.
- `ai/` — llm (wire types), modelrole/modelcaps (role→model registry, capabilities), modeltuner, router (effort routing), provider, localai, embedding, observatory, regressionwatch, tokenest.
- `domain/` — business domains: wiki, backup, mailpriority, knowledge, filestore, notebook, skills, push, code (coding-mode worktrees), contacts, org, approval, market, monitoring, nativesync, usage, ….
- `platform/` — external-system clients: gmail/gmailpoll, calendar/calprop/localcal, localtodo, mailarchive/mailbody/mailwork, lmtpd (LMTP intake), cron, media.
- `infra/` — clientauth (native-client token auth), config, secret, logging, metrics, middleware, httpretry, timeouts, fileshare, shortid, sparkfleet.
- `agentsys/` — standing goals loop; `hanja/` — Han→Korean transliteration; `core/`, `testutil/` — shared helpers.

## Key Architectural Flows

1. **Gateway startup:** `gateway-go/cmd/gateway/main.go` -> `internal/runtime/server` (HTTP + SSE) -> `internal/runtime/rpc` (dispatch) -> `internal/runtime/session` (state). There is no channel plugin (the Telegram bot was retired in PR #1922).
2. **Clients:** both the native client (`client-android/`) and Andromeda (`andromeda/`) talk to the gateway over the `miniapp.*` RPC surface (`POST /api/v1/miniapp/rpc`, `X-Deneb-Client-Token`) plus SSE streams. Wire types for both are generated from Go `//deneb:wire` structs (`.claude/rules/generated-code.md`).

## Cross-Cutting Concerns

- When adding a new module or doc area, update `.github/labeler.yml` and create matching GitHub labels.
