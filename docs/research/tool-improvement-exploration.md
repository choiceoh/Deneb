---
title: "도구 개선 방안 탐구"
summary: "채팅 도구 시스템(스키마 ~50개, deferred ~23+)의 선택 품질·토큰 효율·계측 루프·신규 도구를 코드 조사 기반으로 분석한 제안 노트."
read_when:
  - "채팅 도구 시스템(fetch_tools, 프리셋, 출력 캡)을 개선하려 할 때"
  - "도구 사용 통계·에러율 계측을 설계할 때"
  - "새 에이전트 도구 추가 후보를 검토할 때"
sidebarTitle: "도구 개선 탐구"
---

# 도구 개선 방안 탐구

**Status:** ideation / proposal backlog (도구 시스템 단일 주제)
**Audience:** Deneb 운영자 + 차기 AI 세션
**Scope:** 채팅 도구 시스템 — 선택 품질, 토큰/컨텍스트 효율, 계측, 기존 도구 개선, 신규 도구.
**Methodology:** `gateway-go/internal/pipeline/chat/{toolport,tooldeps,tools,toolreg}` + 실행 경로(`tools.go`, `ai/agent`) 전수 조사, `docs/research/` 기존 노트([improvement-ideas](/research/improvement-ideas), [tool-interception-gap](/research/tool-interception-gap)) 교차 검토, 2026-07-05 프로덕션 측정치 인용. 범용 백로그와 겹치는 항목은 여기서 재서술하지 않고 앵커로 참조한다.

> **읽는 법.** 각 제안은 **현황/근거 → 제안 → 예상 효과 → 우선순위(P0~P3 / S·M·L) → 측정 방법** 순. 채택 여부는 운영자 판단이며, 합의된 항목만 별도 PR로 진행한다.

---

## 0. 한 줄 요약 (TL;DR)

| # | 제안 | 분야 | P | 작업량 |
|---|---|---|---|---|
| B | 도구 통계 폐쇄 루프 (argrepair·unknown-tool·loop-block·출력크기 집계) | 계측 | P1 | ✅ 구현됨 (이 PR) |
| A | fetch_tools 이미-활성 단락(short-circuit) + 반복률 재측정 | 효율·선택 | P1 | ✅ 구현됨 (이 PR, 반복률 재측정은 운영 후) |
| D | deferred 설명 80자 절단 감사 (+선택적 promptSummary 필드) | 선택 품질 | P1 | ✅ 감사 완료 — 최악 5건 선두 재배치 (이 PR) |
| C | per-tool MaxOutput 감사 (B의 출력크기 계측 의존) | 효율 | P2 | S |
| E | fetch_tools 한국어 질의 매칭 보강 | 선택 품질 | P2 | S-M |
| H | 도구 이름 충돌 fail-fast | 신뢰성 | P2 | S |
| G | OnBeforeToolCall 활용 스케치 (per-run 도구 호출 예산) | 신뢰성 | P3 | M |
| F | insights 스테일 주석 수정 (발견 사항) | 위생 | P3 | ✅ 수정됨 (이 PR) |
| N1 | 신규: `weather` 도구 (morning_letter 내부 코드 승격) | 신규 도구 | P2 | S |
| N2 | 신규: `person_briefing` 인물 브리핑 합성 도구 | 신규 도구 | P2 | M |
| N3 | 신규: `followup` 회신 대기 추적 | 신규 도구 | P3 | M |
| N4 | 신규: `travel_time` 이동 시간/출발 알림 | 신규 도구 | P3 | M-L |

---

## 1. 현재 도구 시스템 지도 (2026-07-14 기준)

개선 논의의 공통 전제. 수치는 코드에서 직접 집계했다.

- **도구 스키마 ~50개** (`toolwire/schema/tool_schemas.json` — 파라미터·max_output만 보유, 설명은 등록 코드에; `toolwire/toolreg_boundary_test.go` `allSchemaCases`), 등록은 toolwire + chat 측 별도(fetch_tools, code_action). 그중 **deferred ~23+** (`Deferred: true`, `toolwire/core/register.go`).
- **deferred 메커니즘**: 초기 Tools 배열에서 스키마 제외, 시스템 프롬프트에는 이름 + 80 rune 절단 설명만 노출(`prompt/system_prompt.go`). 모델이 `fetch_tools`(exact names 또는 BM25 질의, `tools/fetchops/fetch_tools.go`)로 활성화하면 다음 턴부터 `DynamicToolsProvider`가 스키마 주입.
- **프리셋 8종** (`toolpreset/preset.go`): conversation/boot/self-review/researcher/implementer/verifier/wiki-research/coding. 노출·활성화·실행 4지점에서 게이트.
- **실행 경로 안전장치** (`chat/tools.go:97` `ToolRegistry.Execute`, 단일 평면 레지스트리): malformed JSON 복구(`tool_argrepair.go`) → 프리셋 방어 → `$ref` 해석 → RunCache(grep만 캐시) → 실행 → 24K head/tail 절단 + 스필오버 → 캐시 무효화 → 사후처리 → 선택적 LLM 압축(`compress:true`, 16000자 이상만 — `localai_hooks.go:44`).
- **루프 감지** (`ai/agent/tool_loop.go:40-42`): warn 10 / critical 20 / breaker 30, 같은 경로 편집 6회 넛지.
- **미지 도구**: Levenshtein "Did you mean" 제안(`chat/tool_suggest.go`).
- **병렬 실행은 제거됨** — 도구는 항상 모델 방출 순서대로 순차 실행.

전반적으로 실행 경로는 성숙하다. 남은 개선 여지는 (1) **계측 데이터가 쌓이는데 소비 루프가 없다**는 것, (2) **선택 품질이 의존하는 표면(80자 설명, BM25 매칭)이 미측정·미감사**라는 것에 집중된다.

---

## 2. 도구 선택 품질

### 2.1 (D) deferred 설명 80자 절단 감사 (**P1 / S**)

- **현황**: deferred 도구 ~23+개는 시스템 프롬프트에 `truncateDescription(desc, 80)` (rune 기준)으로만 보인다. 모델이 "이 도구를 fetch할까"를 판단하는 유일한 근거가 이 80자다. 실제 사례 — `mail_archive` 설명은 200+ rune인데, 80 rune 절단 시 `action=list|search|read|thread|project_history` 열거와 "업무 맥락·미팅 준비에 우선 사용" 트리거 문구가 **모두 잘린다**. 프롬프트 감사(2026-06-12)의 "HOW는 fetch 시점에 배달" 원칙(graphify 패턴)은 옳지만, **WHEN(트리거)이 80자 안에 들어있는지**는 아무도 감사하지 않았다.
- **제안**: ① deferred 설명의 첫 80 rune을 일괄 감사해 "무엇+언제 쓰나"가 앞에 오도록 재배치(설명 뒷부분의 HOW는 그대로 유지). ② 선택적으로 `tool_schemas.json`에 `prompt_summary` 필드를 신설해 절단 대신 명시적 요약을 쓰게 한다(생성기 `cmd/tool-schema-gen` 수정 포함 — 규모가 커지므로 ①로 부족할 때만).
- **예상 효과**: deferred 도구 미발견(모델이 web/exec로 우회하거나 포기) 감소. 캐시 영향 없음 — static 블록 키에 이미 deferred 목록이 포함되어 설명 변경은 1회 cache miss 후 안정.
- **측정**: (B)의 계측으로 "deferred 도구별 fetch율 vs 절단 전후" 비교. 단기로는 `scripts/dev/live-test.sh chat-check "<트리거 문장>" --expect-tool fetch_tools` 시나리오 몇 개로 스팟 확인.

### 2.2 (A) fetch_tools 이미-활성 단락 (**P1 / S**) (효율 측면은 §3.1)

- **현황**: 프로덕션 측정(2026-07-05, 14일 agent-logs): **fetch_tools 호출의 20%가 런 내 동일입력·동일결과 반복** 이력이 있었다. #3089가 컴팩션 cheap 패스에서 fetch_tools 결과를 보호했고, **§6.5에서 already-active 단락이 랜딩**(`tools/fetchops/fetch_tools.go` — 이미 활성면 스키마 재출력 대신 한 줄). 아래 제안은 역사적 설계 노트; 현행은 §6.5 기준.
- **제안**: 활성 집합을 알면 재요청 시 스키마 재출력 대신 `"already active: X — call it directly"` 한 줄을 반환한다. 구현 시 동시성 주의 — `DeferredActivation.seen`은 executor 고루틴 전용이라(`toolport/context.go` 주석 명시) 도구 고루틴에서 직접 읽으면 안 된다. 옵션 ① executor가 턴 시작 시 불변 active-set 스냅샷을 ctx에 주입(락 불필요, 같은 턴 내 중복은 놓침), 옵션 ② seen을 mutex 보호로 전환(완전하지만 concurrency.md 체크리스트 대상). ①이 단순하고 반복의 대부분(턴 경계 넘어 재fetch)을 잡는다.
- **예상 효과**: 반복 1회당 스키마 전문(수백~수천 토큰) 절약 + 모델의 "activated, call directly" 신호 강화.
- **측정**: #3089 이후 반복률을 먼저 재측정(20%는 보호 이전 수치)하고, 단락 도입 후 다시 측정. 둘 다 agent-logs의 turn.tool 입력 해시로 산출 가능.

### 2.3 (E) fetch_tools 한국어 질의 매칭 보강 (**P2 / S-M**)

- **현황**: BM25 토크나이저는 비문자·비숫자 rune 에서 분리하는 whole-token 매칭이다(`tools/runtimeops/bm25.go`, 스테밍 없음). 한글도 letter라 색인은 되지만, ① 조사·어미가 붙은 질의 토큰("메일을")은 색인 토큰("메일")과 불일치, ② 복합어 색인 토큰("자동보관")은 질의 토큰("보관")과 불일치. substring 폴백은 **질의 전체 문자열**이 설명에 그대로 들어있어야 발동해 다어절 한국어 질의를 구제하지 못한다.
- **제안**: 작은 단계 둘 중 하나 — ① 폴백을 질의 전체가 아닌 **토큰 단위** substring으로 완화(질의 토큰 중 하나라도 설명에 부분일치하면 후보 유지, 기존 `searchResultLimit=5`·score floor 유지), ② 한글 토큰에 한해 2-gram 보조 색인. ①이 코드 몇 줄로 끝나며 영어 경로는 불변. 풀 임베딩 라우팅은 [improvement-ideas](/research/improvement-ideas) §2.2의 P3/L 항목 — 이 제안은 그 전 단계의 저비용 보강이다.
- **예상 효과**: 한국어 질의(`fetch_tools(query="메일 보관함 검색")`)의 미스매치로 인한 "No deferred tools match" → 모델 우회 행동 감소.
- **측정**: 한국어 질의 세트 10~20개로 before/after 매칭률 테이블(단위 테스트로 고정 — `fetch_tools_test.go` 확장).

### 2.4 (H) 도구 이름 충돌 fail-fast (**P2 / S**)

- **현황**: 같은 이름 재등록은 last-writer-wins + `slog.Warn`이다(`gateway-go/CLAUDE.md` "Tool Interception & Safety", [tool-interception-gap §7](/research/tool-interception-gap)). 현재는 등록 지점이 `toolwire/core/register.go` 하나라 실위험이 낮지만, 스킬/플러그인이 도구를 공급하기 시작하면 조용한 교체가 된다.
- **제안**: 부트 시 중복 등록을 fail-fast(패닉이 아니라 기동 에러)로 승격하되, 의도적 교체는 명시 플래그(`Override: true`)로만 허용. 등록된 도구 이름 스냅샷 테스트 1개 추가(메서드 레지스트리 스냅샷 테스트와 동형).
- **예상 효과**: 미래 플러그인 표면에 대한 선제 방어. 지금 넣는 이유는 비용이 S일 때가 가장 싸기 때문.
- **측정**: 스냅샷 테스트 자체가 게이트.

---

## 3. 토큰/컨텍스트 효율

### 3.1 (A) fetch_tools 반복 제거의 토큰 측면

§2.2와 동일 항목. 토큰 관점 요약: 반복 fetch 1회 = 해당 도구 스키마 전문이 대화 히스토리에 중복 적재 → 이후 모든 턴의 입력에 계속 실린다(컴팩션 보호 대상이라 지워지지도 않는다 — #3089의 보호가 역설적으로 중복분도 보존한다). 단락 도입 시 중복분이 한 줄로 줄어든다.

### 3.2 (C) per-tool MaxOutput 감사 (**P2 / S**) (B 이후)

- **현황**: 도구 출력은 기본 24K chars에서 head/tail 절단+스필오버된다. per-tool 캡은 `tool_schemas.json`의 `max_output` → `ToolMaxOutputs()`로 오버라이드하는데, **~50개 중 6개만 설정**되어 있다: calendar/contacts/deal_ledger 8000, exec 32000, notebook 24000, wiki 20000 (`toolwire/schema/tool_schemas_gen.go`). 나머지 ~44개는 실측 없이 24K 기본을 쓴다.
- **제안**: (B)의 출력크기 계측(도구별 출력 chars 분포)이 쌓이면, 상위 출력 도구부터 캡을 데이터 기반으로 조정한다. 예상 후보: `sessions`(히스토리 덤프), `polaris`(회상), `web`(페이지 본문), `graphify`. 계측 전 짐작으로 조정하지 않는다 — 이 항목이 P2인 이유이자 (B)가 선행인 이유.
- **예상 효과**: 장기 런(메일분석·크론)의 히스토리 비대 완화. 컴팩션 cheap 패스 부담 감소.
- **측정**: (B) 지표에서 도구별 p50/p95 출력 chars, 스필오버 발생률, `read_spillover` 회수율(스필오버를 실제로 다시 읽는 비율 — 낮으면 캡을 더 줄여도 된다는 신호).

### 3.3 compress 플래그 현황 (제안 없음, 기록만)

`compress:true` 옵트인 LLM 압축은 16000자 임계(`localai_hooks.go:44`) + 스킵셋(`tool_classification_gen.go`) + 로컬 AI 다운 시 스킵으로 보수적으로 설계되어 있고, 결정적 무손실 정리(`tool_compact.go`)가 먼저 돈다. 자동화(옵트인 → 자동)는 (B)의 "compress 플래그 실제 사용률" 계측 없이는 판단 근거가 없다. 현 설계 유지.

---

## 4. 계측 폐쇄 루프 (개선의 전제)

### 4.1 (B) 도구 통계 폐쇄 루프 (**P1 / M**), 로드맵상 최우선

- **현황**: 데이터의 절반은 이미 쌓인다 — `agentlog`의 `TurnToolData`가 per-call duration/입출력 크기/에러를 기록하고, `Writer.Aggregate`가 `ToolStat{Calls, Errors, TotalMs, AvgMs}`로 롤업하며(`core/agentlog/reader.go`), 주석은 명시적으로 *"a tool with high Errors or absent entirely is a candidate for fixing or removal"*이라 적어놨다. `observe` 도구가 top-15를 노출하고 insights에도 배선되어 있다(`server_rpc_session.go`). **그러나 소비자가 없다**: modeltuner는 모델 단위만 다루고, 도구 단위 이상 신호(에러율 급등, 특정 도구 항상 실패)는 아무도 안 본다. 더 중요한 것 — 코드가 스스로 "측정 먼저"를 요구하는 신호 3종이 **로그로만 흩어지고 집계되지 않는다**:
  - argrepair 발동율 — `tool_argrepair.go:27-30`: *"measure the repair-Warn rate first before adding them(schema-aware repairs)"*. Warn은 찍히지만 집계 없음.
  - unknown-tool(환각 도구명) 발생율 — `tool_suggest.go` 경로, 미집계.
  - loop-block(critical/breaker 차단) 발생율 — `tool_loop.go`, 미집계.
- **제안**: turn.tool 엔트리에 `repaired`, `unknown`, `loopBlocked` bool 필드(+이미 있는 `OutputLen` 활용)를 추가하고, `Aggregate`의 `ToolStat`에 해당 카운터를 확장, `observe(what=behavior)` 출력에 노출한다. 자동 조치(modeltuner류 튜닝 루프)는 **하지 않는다** — 첫 단계는 관측 가능하게 만드는 것까지. 분포를 보고 나서 (C) 캡 조정, argrepair 확장, (A) 반복률 검증이 모두 이 위에서 이뤄진다.
- **예상 효과**: §2~3의 제안 대부분이 "측정 후 판단" 구조인데 그 측정 기반이 생긴다. 이 문서의 다른 항목들을 데이터로 채택/기각할 수 있게 된다.
- **측정**: 이 항목 자체가 측정 인프라. 완료 기준: `observe`로 14일 창의 도구별 calls/errors/avgMs/repaired/unknown/loopBlocked/출력분포를 한 번에 볼 수 있다.
- **참조**: 세션 종료 요약 카드([improvement-ideas](/research/improvement-ideas) #9(§4.2)), 도구 선택 trace 로그([improvement-ideas](/research/improvement-ideas) #13(§4.3))는 이 데이터의 소비처 후보 — 중복 구현하지 말 것.

### 4.2 (F) insights 스테일 주석 — **DONE**

`runtime/insights/engine.go` 주석은 실제 배선(`SetToolAggregator`)에 맞게 갱신됨 (§6.5).

### 4.3 (G) OnBeforeToolCall 활용 스케치 (**P3 / M**)

`StreamHooks.OnBeforeToolCall`(block 전용 pre-execution 훅, `ai/agent/hooks.go`)은 구현되어 있고 **프로덕션 소비자 둘**(goal-loop idempotency + untrusted-origin gate; [tool-interception-gap](/research/tool-interception-gap) §4/§10). 추가 유스케이스 후보: **per-run 도구별 호출 예산**(예: `web` 런당 N회 초과 시 block) — 루프 감지가 "같은 호출 반복"을 잡는 것과 달리 "다른 입력의 과다 호출"을 잡는다. 단 (B) 계측에서 과다 호출 패턴이 실재하는지 확인 전에는 착수하지 않는다.

---

## 5. 기존 도구 개선 (per-tool 소항목)

위 구조 제안과 별개로, 조사 중 발견한 도구별 소항목:

- **`morning_letter` 날씨 지역 하드코딩**: `tools/routine/morning_letter.go`가 `wttr.in/Gwangju,South+Korea`를 하드코딩. §6.1의 `weather` 도구 승격과 함께 지역을 설정/컨텍스트에서 받도록 정리.
- **RunCache 캐시 대상 확대 검토**: 현재 grep만 캐시 가능(`toolport/run_cache.go`), 시스템 프롬프트는 "find/tree results are cached within a run"이라 안내(`system_prompt.go:282`). 읽기 전용 도구 중 런 내 재호출이 잦은 것(예: `contacts`, `calendar` 조회)이 있는지 (B) 데이터로 확인 후 확대. 프롬프트 문구와 실제 캐시 대상의 정합도 이때 맞춘다.
- **deferred 설명의 언어 혼재**: 코어 도구(read/grep 등)는 영어, 업무 도구(mail_archive/phone 등)는 한국어 설명. 모델 선택 품질에 유의미한 차이가 있는지는 미검증 — (D) 감사 시 일관성 기준(트리거 문구는 한국어 우선)을 함께 정한다.

코드 위생 관련 인접 항목([improvement-ideas](/research/improvement-ideas) §1.1의 run_exec.go 분리, §3.2의 CJK 절단 테스트)은 기존 백로그 참조.

---

## 6. 신규 도구 제안

기준: 비서실장 페르소나(메일·일정·인물·거래·캡처)의 반복 시나리오 중, 현재 도구 조합으로는 여러 턴·여러 호출이 필요하거나 아예 불가능한 것. "narrow scope, deep quality" 원칙상 **적게 추가하고 깊게** — 4건 모두 deferred 등록 전제(초기 컨텍스트 비용 0에 가까움).

### 6.1 `weather` (**P2 / S**) (기존 코드 승격)

- **근거**: 날씨 fetch·파싱 코드가 **이미 존재**하지만 morning_letter 내부 전용이다(`tools/routine/morning_letter.go` — wttr.in 호출, 지역 하드코딩). "내일 비 와?", "우산 챙겨야 해?"는 현재 `web` 도구(페이지 fetch + 본문 토큰)로 처리되어 비싸고 느리다.
- **제안**: fetch 로직을 공용으로 추출해 `weather(when=now|today|tomorrow, location?)` deferred 도구로 노출. morning_letter는 같은 코드를 소비(중복 제거). 기본 지역은 설정으로.
- **리스크**: 낮음. 외부 의존은 기존과 동일(wttr.in).

### 6.2 `person_briefing` (**P2 / M**) (비서실장 핵심 시나리오)

- **근거**: "내일 김부장 미팅 준비해줘"는 현재 contacts → mail_archive(thread/project_history) → calendar → wiki(인물 페이지) → deal_ledger를 모델이 순차 오케스트레이션한다(4~6회 도구 호출, 각 결과가 히스토리에 적재). 미팅 준비는 주 반복 시나리오라 이 비용이 누적된다.
- **제안**: 인물 식별자를 받아 위 소스들을 **결정적으로**(LLM 없이) 병합한 단일 브리핑 JSON을 반환하는 복합 도구. 합성(서사화)은 main 턴이 담당 — 도구는 수집만. morning_letter의 "도구는 JSON만, 합성은 main 턴" 패턴(model-roles.md)과 동형.
- **리스크**: LLM 오케스트레이션과 기능 중복 — 모델이 개별 도구로도 할 수 있는 일이다. 정당화는 토큰(N회 호출 결과의 중간 산물이 히스토리에 안 쌓임)과 일관성(빠뜨리는 소스 없음). (B) 계측으로 "미팅 준비류 런의 평균 도구 호출 수"를 먼저 확인하고 채택 판단을 권장.

### 6.3 `followup` (**P3 / M**)

- **근거**: "지난주에 견적 요청 보낸 것 답 왔나?" — 보낸 요청의 회신 대기 추적이 도구 표면에 없다(`watch.go`는 이름과 달리 영상 시청 도구). 현재는 mail_archive 검색을 모델이 매번 재구성한다.
- **제안**: mail_archive(스레드 최종 발신자 판정) + cron(주기 점검) 조합의 대기 목록 도구. 단, **스킬(절차서)로 먼저 구현**해 사용 빈도를 확인한 뒤 도구로 승격하는 경로가 위험이 낮다 — 도구 신설은 스키마·계측·유지보수 비용이 붙는다.

### 6.4 `travel_time` (**P3 / M-L**)

- **근거**: 위치(`phone_location.go`)와 일정 장소(calendar)가 이미 있어 "지금 출발하면 되나"를 결정적으로 답할 수 있는 재료는 갖춰졌다. 능동 출발 알림(비서 모드)의 기반.
- **제안**: 외부 경로 API(Kakao/Naver 모빌리티) 연동 도구. **관문은 API 키·쿼터 의존** — 외부 API 최소화 원칙과 상충하므로 운영자가 키 발급을 결정할 때까지 보류. 채택 시 실패 폴백(직선거리+통상 속도 추정)을 함께 설계.

---

## 6.5 구현 현황 (2026-07-05 역기입)

로드맵 1~2단계는 이 문서와 같은 브랜치(PR #3117)에서 즉시 구현됐다:

- **B**: `TurnToolData.Blocked/UnknownTool` + `RunEndData.RepairedToolCalls` → `agentlog.Aggregate`가 `ToolStat.Repaired/Unknown/Blocked/TotalOutputChars/MaxOutputChars`로 폴딩 → `observe(what=behavior)` 도구 라인에 비정상 카운터·출력 크기 노출. unknown 판정은 `agent.ErrUnknownTool` 센티널(chat `unknownToolError`가 래핑), repaired는 run-scoped `toolport.ToolExecStats`(chat 레이어만 관측 가능해 run.end에 탑승).
- **A**: `DeferredActivation`에 executor 드레인 시 갱신되는 불변 active 스냅샷(`IsActive`) 추가 — 문서의 설계 옵션 ① 채택. fetch_tools는 이미 활성인 도구에 스키마 재출력 대신 "Already active — call directly" 한 줄 반환(같은 턴 내 중복은 통과, 문서화된 트레이드오프). 반복률 재측정은 (B) 지표로 운영 데이터 축적 후.
- **D**: 22개 감사 결과 최악 5건(mail_archive·research_panel·phone_read·observe·sessions)의 트리거 문구를 첫 80 rune 안으로 전진 배치. `prompt_summary` 필드 신설(②)은 불필요 판정 — ①로 충분.
- **F**: `insights/engine.go` 스테일 주석 2곳을 실제 배선 사실로 갱신.

C(캡 조정)·E(한국어 매칭)는 계획대로 (B) 데이터 축적/테스트 세트 준비 후 별도 PR.

## 7. 로드맵 (권장 순서)

1. **B — 계측 먼저** (P1/M): 이 문서의 나머지가 대부분 이 데이터 위에서 채택/기각된다. F(주석 수정) 동반.
2. **A + D** (P1/S 둘): 계측과 독립적으로 지금 근거가 충분한 두 건 — 반복률 20% 실측(A), mail_archive 절단 실례(D).
3. **B 데이터 2~4주 축적 후**: C(캡 조정), E(한국어 매칭 — 테스트 세트 기반), RunCache 확대 여부.
4. **신규 도구**: N1(weather)은 독립적으로 진행 가능. N2(person_briefing)는 B 데이터로 시나리오 빈도 확인 후. N3/N4는 보류(스킬 우선 / 키 결정 대기).
5. G·H는 트리거(플러그인 표면 등장, 과다 호출 패턴 실측) 발생 시.

각 항목의 완료 기준은 해당 절의 "측정" 줄. 코드 착수 시 이 문서의 해당 절을 PR 본문에서 참조하고, 채택/기각 결과를 이 문서에 역기입한다.
