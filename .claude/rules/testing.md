---
description: "테스트 작성 및 실행 가이드라인"
globs: ["**/*_test.go", "**/tests/**"]
---

# Testing Guidelines

- Go tests: `go test ./...` (or `make go-test`). Tests are `*_test.go` colocated with source.
- Run `make test` before pushing when you touch logic.
- Agents MUST NOT modify baseline, inventory, ignore, snapshot, or expected-failure files to silence failing checks without explicit approval in this chat.
- Changelog: user-facing changes only; no internal/meta notes (version alignment, appcast reminders, release process).
- Changelog placement: in the active version block, append new entries to the end of the target section (`### Changes` or `### Fixes`); do not insert new entries at the top of a section.
- Pure test additions/fixes generally do **not** need a changelog entry unless they alter user-facing behavior or the user asks for one.
