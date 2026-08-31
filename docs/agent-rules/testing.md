---
description: "테스트 작성 및 실행 가이드라인"
globs: ["**/*_test.go", "**/tests/**"]
---

# Testing Guidelines

- Go tests: `go test ./...` (or `make go-test`). Tests are `*_test.go` colocated with source.
- Run `make test` before pushing when you touch logic.
- Treat tests as executable contracts: name the subject, condition, and outcome; assert an observable result; and cover the failure, cancellation, rollback, or malformed-input obligation introduced by the production code.
- Do not add repetitive test bodies to improve a line/count metric. Prefer named tables for genuinely distinct input classes and keep generated matrices tied to a checked-in generator plus drift check.
- Shell-script tests must stub every non-Unix command the driven script can reach. `scripts/dev/test_shell_isolation.py` proves it: it shadows each host binary outside `/usr/bin`, `/bin`, `/usr/sbin`, `/sbin` with a logging shim, runs every lane, and requires an empty log. A reached toolchain does real work whose duration tracks machine load, which is how a lane starts timing out — the lane passing is not evidence it was isolated.
- Keep tests discoverable from the production subject. Large cross-domain `contracts`, `hardening`, or `helpers` files should be split by responsibility unless they describe one explicit boundary.
- `make health-v2-test` verifies the scorer's anti-gaming behavior. `make health-v2-check` ratchets behavior evidence, test maintainability, and every other Health Bench 2.0 pillar independently; see `codebase-health-v2.md`.
- `make health-v3-test` covers Health Bench 3.0 composite/anti-compensation/runtime-cache fixtures; `make health-v3-check` ratchets Structure+Runtime (Fitness advisory); see `codebase-health-v3.md`.
- Agents MUST NOT modify baseline, inventory, ignore, snapshot, or expected-failure files to silence failing checks without explicit approval in this chat.
- Changelog: 루트 `CHANGELOG.md`는 release-please가 Conventional Commit에서 자동 생성한다 — **직접 편집 금지** (섹션도 `### ✨ Features`/`### 🐛 Bug Fixes` 등 자동 관리). 사용자에게 보이는 릴리스 노트는 커밋 제목 품질로 결정된다.
- 네이티브 클라 사용자 표시 패치노트는 `client-android/app/changelog.d/` 조각 파일로 추가한다 (user-facing 변경만, 내부/메타 노트 금지 — `docs/agent-rules/release-and-deploy.md` 참조).
- Pure test additions/fixes generally do **not** need a patch-note fragment unless they alter user-facing behavior or the user asks for one.
