---
description: "테스트 작성 및 실행 가이드라인"
globs: ["**/*_test.go", "**/tests/**"]
---

# Testing Guidelines

- Go tests: `go test ./...` (or `make go-test`). Tests are `*_test.go` colocated with source.
- Run `make test` before pushing when you touch logic.
- Agents MUST NOT modify baseline, inventory, ignore, snapshot, or expected-failure files to silence failing checks without explicit approval in this chat.
- Changelog: 루트 `CHANGELOG.md`는 release-please가 Conventional Commit에서 자동 생성한다 — **직접 편집 금지** (섹션도 `### ✨ Features`/`### 🐛 Bug Fixes` 등 자동 관리). 사용자에게 보이는 릴리스 노트는 커밋 제목 품질로 결정된다.
- 네이티브 클라 사용자 표시 패치노트는 `client-android/app/changelog.d/` 조각 파일로 추가한다 (user-facing 변경만, 내부/메타 노트 금지 — `docs/agent-rules/release-and-deploy.md` 참조).
- Pure test additions/fixes generally do **not** need a patch-note fragment unless they alter user-facing behavior or the user asks for one.
