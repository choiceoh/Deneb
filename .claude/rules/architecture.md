---
description: "프로젝트 구조 및 모듈 아키텍처 참조"
globs: ["cmd/**", "internal/**", "pkg/**", "src/**"]
---

# Project Structure & Module Organization

## Top-Level Directory Map

- `gateway-go/` — Go gateway server (HTTP/WS server, RPC dispatch, session management, channel registry, chat/LLM, tools, auth). The primary runtime.
- `proto/` — shared Protobuf schemas (gateway frames, channel types, session models). Source of truth for cross-language types.
- `skills/` — user-facing skill plugins organized by category (coding/, productivity/, devops/, integration/).
- `docs/` — Mintlify documentation site.
- `scripts/` — build, dev, CI, audit, and release scripts.
- `.agents/skills/` — maintainer agent skills (release, GHSA, PR, Parallels smoke).
- `.github/` — CI workflows, custom actions, issue/PR templates, labeler, CODEOWNERS.
- `Makefile` — build orchestration (Go + protobuf).
- Tests: Go tests `*_test.go`.

## Key Architectural Flows

1. **Gateway startup:** `gateway-go/cmd/gateway/main.go` -> `internal/server` (HTTP/WS) -> `internal/rpc` (dispatch) -> `internal/session` (state) -> `internal/telegram` (plugin).
2. **Protobuf type flow:** `proto/*.proto` -> `scripts/proto-gen.sh` -> Go (`gen/*.pb.go`).

## Cross-Cutting Concerns

- When adding channels/docs, update `.github/labeler.yml` and create matching GitHub labels.
