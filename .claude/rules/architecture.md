---
description: "프로젝트 구조 및 모듈 아키텍처 참조"
globs: ["gateway-go/cmd/**", "gateway-go/internal/**", "gateway-go/pkg/**"]
---

# Project Structure & Module Organization

## Top-Level Directory Map

- `gateway-go/` — Go gateway server (HTTP + SSE server, RPC dispatch, session management, chat/LLM, tools, auth). The primary runtime.
- `skills/` — user-facing skill plugins organized by category (coding/, devops/, integration/, productivity/, security/).
- `docs/` — Mintlify documentation site.
- `scripts/` — build, dev, CI, audit, and release scripts.
- `.github/` — CI workflows, custom actions, issue/PR templates, labeler, CODEOWNERS.
- `Makefile` — build orchestration (Go).
- Tests: Go tests `*_test.go`.

## Key Architectural Flows

1. **Gateway startup:** `gateway-go/cmd/gateway/main.go` -> `internal/runtime/server` (HTTP + SSE) -> `internal/runtime/rpc` (dispatch) -> `internal/runtime/session` (state). The native client connects over the `miniapp.*` RPC surface; there is no channel plugin (the Telegram bot was retired in PR #1922).

## Cross-Cutting Concerns

- When adding a new module or doc area, update `.github/labeler.yml` and create matching GitHub labels.
