---
description: "프로젝트 구조 및 모듈 아키텍처 참조"
globs: ["cmd/**", "internal/**", "pkg/**"]
---

# Project Structure & Module Organization

## Top-Level Directory Map

- `gateway-go/` — Go gateway server (HTTP/WS server, RPC dispatch, session management, chat/LLM, tools, auth). The primary runtime.
- `skills/` — user-facing skill plugins organized by category (coding/, productivity/, devops/, integration/).
- `docs/` — Mintlify documentation site.
- `scripts/` — build, dev, CI, audit, and release scripts.
- `.agents/skills/` — maintainer agent skills (release, GHSA, PR).
- `.github/` — CI workflows, custom actions, issue/PR templates, labeler, CODEOWNERS.
- `Makefile` — build orchestration (Go).
- Tests: Go tests `*_test.go`.

## Key Architectural Flows

1. **Gateway startup:** `gateway-go/cmd/gateway/main.go` -> `internal/server` (HTTP/WS) -> `internal/rpc` (dispatch) -> `internal/session` (state) -> `internal/telegram` (plugin).

## Cross-Cutting Concerns

- When adding channels/docs, update `.github/labeler.yml` and create matching GitHub labels.
