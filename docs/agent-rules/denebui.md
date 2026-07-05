---
description: "deneb-ui 카드 (라벨 HTML wire) — 3구현 동기 원칙과 변경 절차"
globs: ["gateway-go/internal/pipeline/chat/denebui/**", "gateway-go/cmd/denebui-check/**", "client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/ui/dynamicui/**", "andromeda/src/markdown/denebUi*", "andromeda/src/components/DenebUi*", "docs/research/deneb-ui-html.md", "skills/productivity/morning-letter/**"]
---

# deneb-ui 카드 (라벨 HTML wire 포맷)

> 에이전트 저작 리치 UI. 2026-07 JSON→**라벨 HTML** 전환("AST as HTML") — 모델의
> 사전학습 HTML 유창성을 쓰고, 클라 JSON 수리 계층을 은퇴시켰다. HTML은 스트림
> 절단에도 우아하게 열화된다(EOF 자동닫힘).

## 3구현 동기 (철칙)

같은 미니 토크나이저 스펙의 포팅 3개가 있다. **그래머 단일 진실원은
`docs/research/deneb-ui-html.md`** 이고, 하나를 바꾸면 셋 다 + 공유 테스트
벡터를 함께 바꾼다:

| 구현 | 파서 | 테스트 |
|---|---|---|
| 게이트웨이 (검증) | `denebui/html.go` (`ParseHTML` → 기존 nodeSpecs 재사용) | `html_test.go` + `FuzzParseHTML`(+testdata 코퍼스) |
| 네이티브 (렌더) | `ui/dynamicui/DenebUiHtml.kt` | `DenebUiHtmlTest.kt` |
| Andromeda (렌더) | `markdown/denebUiHtml.ts` | `denebUiHtml.test.ts` |

- **HTML5 파서(DOMParser·x/net/html) 사용 금지** — table foster-parenting이
  커스텀 태그를 재배열한다. 미니 토크나이저 유지.
- **전체 소문자화로 인덱스 계산 금지** — 유니코드 케이스매핑은 길이를 바꾼다
  ('İ'→"i̇"; 퍼저 실크래시). ASCII 수동 폴딩(`indexOfCloseTag`)만.
- legacy JSON 경로는 **구 트랜스크립트 표시 전용 strict 파스** — 수리 휴리스틱
  재도입 금지, 신규 저작 금지.

## 저작 계약 위치 (바꾸면 함께 갱신)

- 일반 응답: `prompt/system_prompt.go` 소통 섹션 카드 문단 (static 블록 —
  내용 수정 = 캐시 1회 무효화, 마커 4개 불변).
- 이브닝레터: `toolreg/core.go` evening_letter 도구 설명.
- 모닝레터: `skills/productivity/morning-letter/SKILL.md` 스켈레톤 —
  `letter_card_test.go` 상수와 **동기 유지**(테스트가 서버측 게이트).
- 서버 조립: `denebui/collapsed.go` (`CollapsedReportFence`) — raw-text 본문의
  백틱은 반드시 `&#96;` 이스케이프(바깥 펜스 조기종료 차단).

## 관찰/검증 사슬

- 런타임: 턴 완료 시 카드 검증 Warn 로그 (`run_lifecycle.go`
  `reportDenebUICardHealth`) — Warn 증가 = 모델/프롬프트 드리프트 신호.
- 품질 스위트: `checks.py check_deneb_ui_valid`(denebui-check 위임) +
  `quality-tests.yaml` `fmt-deneb-ui-card` 행동 게이트.
- 시각: `RenderPreview.kt`의 레터 프리뷰가 **정본 HTML 스켈레톤을 실제 파서로**
  렌더 (`:composeApp:renderPreviews` → `/tmp/deneb-render/letter_*.png`).
- 수동 CLI: `go run ./cmd/denebui-check < 응답.txt` (양 포맷).

## 스트리밍 프로그레시브 렌더

미완 펜스의 HTML 부분 트리는 실시간 렌더하되, **인터랙티브 노드(버튼·입력류)
포함 트리는 placeholder 유지** — 반쯤 그려진 폼이 스트림 중 입력을 받으면 안
된다. 판정 워커도 3구현 동기: `DenebUiInteractivity.kt` /
`denebUiParse.ts hasInteractiveNode` (게이트웨이는 해당 없음).
