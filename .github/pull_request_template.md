<!--
이 템플릿은 웹 UI / 손으로 작성하는 PR 용 미러입니다.
에이전트가 `gh pr create --body` 로 만드는 PR 은 이 파일을 우회하므로,
형식의 단일 진실원은 .claude/rules/git-pr.md 의 "PR Body (canonical skeleton)" 입니다.
본문은 한국어 우선. 해당 없는 섹션은 지우세요.
-->

## Summary

<!-- 무엇을 / 왜 — 문제와 동기 (1~3문단) -->

## Changes

<!-- 주요 변경점, 파일 경로 포함 불릿 (gateway-go/internal/…:line) -->
-

## Verification

<!-- `make check` 통과 명시 + 관련 시 라이브(live-test.sh)·신규 테스트 -->
- `make check`

<!-- 선택(해당 시에만 남기고 나머지는 삭제):
## Before → After      표로 회귀 방지 증거
## 한계 / 리뷰 노트
## Follow-up (out of scope)
## Cache 영향           프롬프트 캐시 경로를 건드릴 때 (.claude/rules/prompt-cache.md PR 체크리스트)
-->
