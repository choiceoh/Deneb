# Architecture Decision Records (ADR)

Deneb의 설계·아키텍처 결정을 **시점별로** 기록하는 디렉터리. 코드와 `CLAUDE.md`는
"현재 어떻게 생겼는지"를 담지만, ADR은 **"언제·왜 그 선택을 했고 무엇을 기각했는지"**를
담는다. 단일 인적 의존(bus factor 1) 하에서 미래의 에이전트/인간이 결정의 의도를
재구성하는 비용을 낮추는 것이 목적이다.

## 언제 쓰나

되돌리기 어렵거나, 한 번 정해지면 넓은 표면에 영향을 주는 결정을 기록한다.

- 페르소나/경계/계약 (분석·비서 분리 금지, 단일 RPC 배선 지점, wire 단일소스)
- 의견이 갈렸거나 명시적으로 기각된 대안이 있는 결정
- "왜 이렇게 생겼지?"가 반복될 법한 비자명한 결정

사소한 구현 선택이나 되돌릴 수 있는 실험은 ADR이 아니다 (그건 코드 주석이나
research 노트로 충분).

## 형식

- 파일명: `NNNN-소문자-케밥.md` (4자리 증가, 예: `0001-no-persona-split.md`).
- 본문 4섹션 (간결하게, 각 1문단 내외):

  ```markdown
  # NNNN. 제목

  **Status:** accepted | proposed | superseded by NNNN

  ## Context
  결정이 필요했던 배경·제약.

  ## Decision
  채택한 방향 (구체적으로 — 경로/계약/불변식).

  ## Alternatives rejected
  기각한 대안과 기각 이유.

  ## Consequences
  긍정적/부정적 결과와 그로 인해 지켜야 할 것.
  ```

- ADR은 **수정하지 않는다.** 결정이 바뀌면 기존 ADR의 Status를 `superseded by NNNN`으로
  바꾸고 새 ADR을 쓴다 (immutable append-only 로그).
- **역기입(backfill)**: 과거 결정을 뒤늦게 기록할 때는 `Status: accepted (retrospective)`로
  표시하고, Context에 "기존 문서에서 재구성 — 역사적 근거는 불확실할 수 있음"을 명시한다.
  추측한 근거를 사실처럼 쓰지 않는다.

## Status 생애주기

```
proposed ──> accepted ──> superseded (새 ADR이 승계)
                 └────────> deprecated (폐기, 이유 명시)
```

## 템플릿

```markdown
# NNNN. 제목

**Status:** proposed

## Context

## Decision

## Alternatives rejected

## Consequences
```

새 결정을 내릴 때 이 템플릿을 복사해 `NNNN`을 다음 번호로 채운다.
