# Agent Rules (조건부 로딩)

> **필요한 규칙만, 필요한 시점에, 필요한 만큼.** 이 디렉토리의 룰은 세션에
> 자동 주입되지 않는다 — 코딩 에이전트는 루트 `CLAUDE.md`의 룰 인덱스를 보고
> 필요할 때 Read 한다. 예전 `.claude/rules/`의 전량 자동 주입(~193KB ≈ 6만
> 토큰/세션, 대부분 미사용)을 없애기 위해 2026-07 여기로 이동했다.

## 동작 방식

1. **항상-온 코어**는 루트 `CLAUDE.md` 하나뿐이다 (안전·게이트·Git·스타일·운용
   + 룰 인덱스, ~6KB). 모듈 상세는 각 모듈의 `CLAUDE.md`가 담당한다 (Claude
   Code가 해당 디렉토리 접근 시 자동 로드).
2. **첫 접촉 안내 훅**: `.claude/settings.json`의 PreToolUse 훅
   (`scripts/dev/claude-rules-gate.py`)이 Edit/Write 대상 경로를 각 룰의
   frontmatter `globs`와 대조해, 세션당 룰별 1회 편집을 차단하며 해당 룰을
   안내한다. 에이전트는 관련 룰을 Read 후 같은 편집을 재시도하면 된다
   (마커: `$TMPDIR/claude-rules-gate-<session>/`). 훅은 fail-open — 어떤
   오류에서도 편집을 막지 않는다.
3. Bash 경유 파일 수정(sed 등)은 훅 범위 밖이다 — 인덱스 계약이 커버한다.

## 룰 파일 규약

- frontmatter 필수: `description`(한 줄 — 훅 안내문과 CLAUDE.md 인덱스에 쓰임),
  `globs`(선택 — 이 경로를 만질 때 읽어야 하는 룰만. 없으면 인덱스 전용).

  ```markdown
  ---
  description: "Go 게이트웨이 구조·빌드"
  globs: ["gateway-go/**"]
  ---
  ```

- `globs`는 JSON 배열 또는 쉼표 구분 문자열. `**`는 경로 전체를 가로지른다.
- **넓은 globs를 남발하지 말 것** — `**` 같은 전역 매치는 모든 에이전트의 첫
  편집을 차단한다. 항상 필요한 내용이면 룰이 아니라 루트 `CLAUDE.md` 코어에
  들어가야 한다 (단, 코어는 ~6KB 예산 — 넣기 전에 "정말 모든 작업에
  필요한가?"를 자문).
- 룰을 추가/삭제/이름 변경하면 루트 `CLAUDE.md`의 룰 인덱스 표도 갱신한다.
- 운영 런북 성격의 장문(예: sidecar-models)은 여기보다 위키/docs가 맞는지
  먼저 검토한다.
