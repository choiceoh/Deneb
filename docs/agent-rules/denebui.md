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

- 일반 응답 라우터: `prompt/system_prompt.go` 소통 섹션의 **인라인 컴팩트 계약**
  (태그 인벤토리+결정 예시+deneb-html 계약, 2026-07-18). 구 "스킬을 먼저 읽어라"
  라우팅은 실측(7일 저널: 스킬 read 0회·미스 3:1·발명 태그 리젝트)으로 은퇴 —
  static 블록이 최소 인벤토리를 직접 들고, 스킬은 복잡 조합용 상세 계약으로 잔존.
- 일반 응답 상세 저작 계약: `skills/productivity/deneb-ui-authoring/SKILL.md`.
- **웹페이지형 HTML 응답**(```deneb-html — 자유 HTML 문서, 샌드박스 인라인 렌더)은
  별도 wire: 정본 [docs/research/deneb-html.md](../research/deneb-html.md),
  서버 정규화 `denebui/htmlanswer.go`, 렌더러는 `DenebHtml.tsx`(iframe) /
  `DenebHtmlView.android.kt`(WebView). deneb-ui 3구현 토크나이저 동기 대상 아님
  (파서가 아니라 샌드박스 컨테이너).
- 이브닝레터: `toolwire/chrono/register.go` evening_letter 도구 설명.
- 모닝레터: `tools/routine/morning_card.go`의 서버 조립이 정본이고
  `morning_card_test.go`가 실제 validator로 게이트한다. 스킬은 완성된
  `delivery`를 그대로 반환하는 짧은 호출 계약만 가진다.
- 서버 조립: `denebui/collapsed.go` (`CollapsedReportFence`) — raw-text 본문의
  백틱은 반드시 `&#96;` 이스케이프(바깥 펜스 조기종료 차단). 단 **생산자가 이미
  카드로 저작한 본문(```deneb-ui로 시작)은 relay가 아코디언 래핑을 생략**하고
  verbatim 전달한다 (`proactive_relay.go` collapse 분기).
- 메일 분석 (능동): `mailanalysis/analyzer.go` `analysisSystemPrompt` +
  `DefaultPrompt` — 구조적 보고는 카드 도입부. 시스템 프롬프트 쪽 계약이
  운영자 커스텀 프롬프트 파일과 무관하게 적용되는 정본.
- 하트비트 보고 (능동): `runtime/heartbeat/heartbeat_task.go` `heartbeatTriggerTemplate`
  — 비 NO_REPLY 보고는 카드 기본, 결정 요청은 인터랙티브 카드.
- 채택률 관측: `run_lifecycle.go` `looksStructuredWithoutCard` — 카드 없이
  구조적 형태로 나간 턴을 Info 로그("deneb-ui adoption miss")로 계수.
  읽는 방법은 **`make card-adoption`** (`scripts/audit/card-adoption.py`,
  advisory·읽기전용): 저널의 `card authored` vs `adoption miss`를 세션 클래스별로
  갈라 비율을 낸다. **operator 값만 본다** — 자동 레인이 훨씬 높아 평균에 섞으면
  정작 중요한 수치가 가려진다 (2026-07-25 실측: mailpoll 80.6% vs client:main 26%).
  miss는 휴리스틱이라 절대값보다 **클래스 간 격차와 추세**가 신호다.

## 관찰/검증 사슬

- 런타임: 최종 전달 전 `denebui.NormalizeFinalReply`가 모든 펜스를 `Validate`
  하고, invalid/추가 펜스를 평문으로 내린다. 단 **내용 보존형 이슈(unknown tag
  unwrap — `Issue.Recoverable`)만 있는 카드는 카드로 배달**된다(2026-07-18) —
  3구현 파서가 동일하게 unwrap 하므로 안전하며, Issue 는 드리프트 텔레메트리로
  계속 로그된다. 그 뒤 카드 health 로그가 채택률과 남은 드리프트를 관찰한다.
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
