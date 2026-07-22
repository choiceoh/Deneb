---
title: Coding-agent session memory
description: Auto-capture + always-surface memory for the Claude Code coding agent working on Deneb
---

# 코딩 에이전트 세션 기억

클로드코드(코딩 에이전트)가 **세션이 끝나면 그날 한 일을 잊는** 문제를 줄인다.
세션 종료 시 작업 골격을 자동으로 기록하고, 다음 세션 시작 시 최근 결정·교훈을
항상 표면화한다. 외부 도구([[agentmemory-adoption-review]]에서 통째 도입 반려)
없이, 이미 있는 훅·로컬 모델·파일만으로 만든 얇은 루프.

## 세 조각

| 조각 | 훅 | 하는 일 | LLM |
|---|---|---|---|
| **수집** | `SessionEnd` → `scripts/dev/session-memory-capture.py` | git 골격(브랜치·커밋·바뀐 파일·dirty) + 세션의 첫 질문(topic)을 `episodes.jsonl`에 1줄 추가 | 없음 (순수 git) |
| **표면화** | `SessionStart` → `scripts/dev/session-memory-surface.py` | `card.md` + 최근 세션 몇 건을 `additionalContext`로 주입 | 없음 (파일 읽기) |
| **압축(의미층)** | 에이전트가 직접 | 중요한 결정·교훈·자기교정을 `card.md`에 요약 | 인-루프 에이전트 |

- **넓게 수집 + 항상 표면화**가 운영자 선택. 그래서 표면화 블록은 **상한(카드 2500자,
  최근 세션 4건)** 을 둬 매 세션 컨텍스트가 부풀지 않게 한다(런타임 하트비트에서 고친
  것과 같은 교훈).
- `SessionStart`는 `compact` 소스에도 뜨므로 **컴팩션 후에도 기억을 재주입**한다(보너스).

## 저장 위치

레포가 아니라 **`~/.claude/deneb-session-memory/`** (사용자 홈, 모든 워크트리 공유):

- `episodes.jsonl` — 자동 기록된 세션 골격(append-only).
- `card.md` — 항상 표면화되는 짧은 요약(에이전트가 갱신).

세션 데이터라 레포에 커밋하지 않는다. 훅 스크립트만 버전 관리 대상.

## Fail-safe

두 스크립트 모두 **어떤 에러도 exit 0 + 무출력**으로 끝난다 — 기억 훅이 코딩 세션을
절대 못 깨게. git 실패·파일 없음·페이로드 파손은 조용히 무시된다.

## 끄기

`.claude/settings.json`의 `SessionStart`(surface)·`SessionEnd`(capture) 항목을 지우면
즉시 비활성. 스크립트·데이터는 남는다.

## 범위 (v1 → 다음)

- **v1 (지금)**: 수집 + 표면화 + 에이전트 압축.
- **다음**: 로컬 모델 자동 압축(엔드포인트·지연 검증 후) · 검색 기반 회상(codesearch) ·
  정리/decay · 반복 패턴의 영구 메모리 승격.
