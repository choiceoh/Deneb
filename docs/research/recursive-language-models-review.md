# Recursive Language Models (RLM) × Deneb 도입 요소 검토

> **출처**: 영상 [*"Context as a Variable: The Architecture Killing Context Rot"*](https://youtube.com/shorts/AvIujLlbmks) (Cloud Codes) → 원 출처 Alex L. Zhang · Tim Kraska · Omar Khattab (MIT CSAIL), *"Recursive Language Models"* ([arXiv:2512.24601](https://arxiv.org/abs/2512.24601), [저자 블로그](https://alexzhang13.github.io/blog/2025/rlm/), [코드](https://github.com/alexzhang13/rlm)).
> **방법**: RLM 핵심 기제 추출 → Deneb 의 out-of-context 기구(`gateway-go/internal/ai/agent/spillover.go`, `gateway-go/internal/pipeline/polaris/`, `gateway-go/internal/pipeline/compaction/`)와 코드 대조 → 채택/실험/스킵 판정. 영상 자체는 자막 접근이 막혀 oEmbed 메타 + 원 논문/저자 블로그로 대체 확인.
> **일시**: 2026-08-22
> **한 줄 결론**: RLM 의 프리미티브 중 **대부분을 Deneb 는 이미 갖고 있다** — 컨텍스트를 창 밖 변수로 두는 저장소(spillover + Polaris), 프로그래밍적 peek/grep/slice(`read_spillover(offset/limit/grep)`), 원문 무손실 보존(Polaris JSONL — 단 검색 인덱스는 잘린 투영이다), 그리고 **깊이-1 재귀 서브콜**(`polaris(action="expand", question=…)` — 원문을 루트에 안 올리고 서브 LLM 답변만 반환). 없는 건 **범용 파티션+맵리듀스 팬아웃**(YouTube 자막 전용 구현은 이미 있다)과 **루트 무맥락화**뿐이고, 후자는 **도입하면 안 된다**(RLM 이 스스로 인정한 한계 = prefix caching 부재·초~분 단위 레이턴시인데, 이는 Deneb 의 prompt-cache 원칙과 체감 레이턴시(main 턴 평균 54~73초 + 모바일 데일리드라이버)를 정면으로 깬다 — 하드캡 문제가 아니다: 인터랙티브 백스톱은 30분이다(§3)). 실제 nugget 은 **두 계층의 비대칭 해소** 3건: ① spillover 블롭에도 재귀 서브콜을 주고(§4-A), ② 그 전제인 "변수가 살아있다"를 실제로 성립시키고(§4-B — 스필이 **자기를 만든 턴이 끝날 때** 삭제되는데 컴팩션은 포인터를 영구히 남긴다 = 구조적 dangling), ③ 루트가 탐색 계획을 세울 메타데이터를 프리뷰에 넣는다(§4-C).

---

## 1. RLM 핵심

긴 입력을 창에 밀어넣으면 길이에 비례해 품질이 떨어진다(**context rot**). RLM 은 이를 *압축*이 아니라 **주소화**로 푼다.

**기제** — 루트 LM 은 **질의만** 받고 원본 컨텍스트는 못 본다. 원본은 Python REPL 환경의 **변수**로 올라가 있고, 루트는 그 변수의 *존재와 크기*만 안다. 루트는 코드를 실행해:

1. **peek / partition / grep** — 변수를 들여다보고, 쪼개고, 정규식으로 훑는다.
2. **재귀 서브콜(depth=1)** — 자기가 고른 부분집합에 대해 LM 을 함수처럼 호출한다. 결과는 REPL 로 돌아오고, **원문은 끝까지 루트 창에 안 올라간다**.
3. 청크 병렬 서브콜 → 결과 합성(맵리듀스)이 모델이 스스로 발현시키는 전략.

**결과**: OOLONG(132k) 에서 RLM(GPT-5-mini) 이 GPT-5 대비 **+34점(~114%)** 을 비슷한 비용에, BrowseComp-Plus(1000문서·10M+ 토큰) 에서 RLM(GPT-5) 은 무열화. 컨텍스트 창의 **2자릿수 배**를 처리.

**저자가 명시한 한계** — 여기가 Deneb 판정의 축이다:

- **prefix caching 미활용**, 재귀 호출이 blocking·비동기 아님 → 질의 하나가 **수 초~수 분**.
- **총 API 비용·런타임에 대한 보장이 없다**("strong guarantees … remain absent").
- 극단적 길이의 counting-heavy 과제에서는 여전히 열화.
- 프로덕션 배포엔 추가 최적화가 필요하다 (저자 표현).

---

## 2. Deneb 현황 대조 — 이미 대부분 갖고 있다

> **이 표는 이 PR 이전의 기준선(baseline)이다.** §3–§4의 판정이 이 상태에서 출발하므로 그대로 둔다. 이 PR로 실제 바뀐 두 행에는 **[이 PR에서 해소]** 표시를 달았고, 변경 후 상태는 §4를 보라.

| RLM 프리미티브 | Deneb 현재 (PR 이전) | 위치 |
|---|---|---|
| 컨텍스트를 창 밖 변수로 저장 | 있음 — 24K(툴별 `MaxOutput`) 초과 결과는 디스크로 spill, 창에는 head/tail 절반씩(기본 24K면 약 12K씩)과 `sp_*` 핸들을 담은 잘림 마커만 남는다 | 런타임 경로는 `capToolOutput`(`gateway-go/internal/pipeline/chat/tools.go:274`) → `TruncateHeadTail`(`gateway-go/internal/ai/agent/truncate.go:32`). `FormatPreview`/`SpillAndPreview`는 **런타임 호출자가 없는** 편의 래퍼다 |
| 무손실 원본 보존 | 부분 — 원문은 **세션별 JSONL** 에 무손실 append 되고 요약은 DAG 노드로 *추가*되지만, 검색 인덱스는 SQLite FTS 가 아니라 **로드 시 재구축되는 인메모리 `textsearch.Index`** 이고 그 투영은 tool_result 를 2,000바이트로 자른다. 즉 **JSONL 은 무손실, 검색은 무손실이 아니다** — 원문 복원은 `LoadMessages` 경로뿐. 보존도 retention GC(`sweep.go`) 전까지다 | `gateway-go/internal/pipeline/polaris/store.go` |
| 프로그래밍적 peek / grep / slice | 있음 — `read_spillover(spill_id, offset, limit, grep)`, 페이지당 400줄·20K자 바운드와 `[계속: offset=N]` 안내 | `gateway-go/internal/pipeline/chat/toolwire/schema/tool_schemas.json` |
| 구조 메타 조회(목차) **[이 PR에서 해소]** | 부분 — 대화 이력엔 있음(`polaris(action="describe")`: 총 메시지·커버리지·요약노드 목록). **spill 블롭의 잘림 마커는 생략 줄 수와 `sp_*` ID 만** 준다 | `gateway-go/internal/pipeline/chat/tools/recallops/polaris.go:92` vs `TruncateHeadTail` 의 마커 |
| **깊이-1 재귀 서브콜** **[이 PR에서 해소]** | 있음, 단 대화 이력 한정 — `expand(summary_id, question)` 이 원본 구간을 로드해 **로컬 LLM 에 위임**하고 *답변만* 루트로 반환 | `gateway-go/internal/pipeline/chat/tools/recallops/polaris.go:196` |
| 파티션 + 병렬 맵리듀스 | 있음, 단 **YouTube 자막 전용** — `splitTranscriptChunks`(≤4청크) + `summarizeTranscriptChunked` 가 청크별 팬아웃 후 교차 결론 패스를 돈다. 범용/온디맨드 인터페이스는 없다. `expand` 는 단일 노드·8000자 단일 청크이고, `research_panel` 은 "한 질문 → 여러 모델"이지 "여러 청크 → 한 모델"이 아님 | `gateway-go/internal/pipeline/chat/web/web_youtube.go:184` |
| 루트가 원본을 아예 안 봄 | 없음 — 기본 경로는 여전히 append-only 히스토리 조립과 임계값 컴팩션 | `gateway-go/internal/pipeline/chat/run_prepare.go` → `gateway-go/internal/pipeline/compaction/` |
| 비용·런타임 보장 | **Deneb 가 앞섬** — 인터랙티브 턴 백스톱 30분/소프트 20분(auto-resume 류 경로는 별도로 5분), variable prompt addition 예산, 컴팩션 튜너 | `gateway-go/internal/pipeline/chatport/runtime_contracts.go`, `gateway-go/internal/runtime/server/server_lifecycle.go:19`, `gateway-go/internal/pipeline/chat/promptbudget/`, `gateway-go/internal/pipeline/compactuner/` |

**진단** (기준선 기준): "context as a variable" 은 Deneb 에 이미 **두 계층으로** 구현돼 있다 — 툴 결과층(spillover)과 대화 이력층(Polaris). RLM 이 새로 주는 건 *개념*이 아니라 **그 두 계층의 비대칭을 메우는 구체적 형태**다. 대화 이력층은 `describe`(구조 메타)와 `expand(question)`(재귀 서브콜)을 갖췄는데, 툴 결과층은 `grep`/`offset`(탐색)만 있고 **재귀 서브콜도 구조 메타도 없다**. 정작 덩치가 큰 쪽은 툴 결과층이다 — YouTube 자막, `exec` 출력, 메일 아카이브 덤프.

---

## 3. 적용성 — 전면 도입이 왜 안 되나, 어디는 맞나

**전면 도입(루트 무맥락화)이 안 되는 이유 — 판정의 핵심**

- **prompt-cache 원칙과 충돌**. 매 턴 루트 창을 질의만으로 재구성하면 히스토리 prefix 재사용이 통째로 사라진다. 단 근거를 정확히 해두자 — **main/main2 는 현재 클라우드(kimi·glm)이고 vLLM APC 핫패스는 fallback·dsv4 헬퍼 트래픽으로 축소됐다**(`docs/agent-rules/model-roles.md`). 즉 "메인 턴이 vLLM APC 로 산다"는 서술은 틀렸다. 그럼에도 원칙은 유효하다: prompt-cache.md 의 규칙(캐시 마커 4개, per-turn 가변 바이트는 system 이 아니라 마지막 user 꼬리로)은 그 트래픽과 main 의 로컬 복귀 대비로 **여전히 준수 대상**이고, 클라우드 프로바이더도 prefix 캐싱으로 과금·지연이 갈린다. RLM 은 **prefix caching 을 안 쓰는 게 명시된 한계**다.
- **레이턴시 성격이 반대**. RLM 은 질의당 "수 초에서 수 분"을 허용하는 배치성 장문 QA 전략이다. 수치를 바로잡자면 인터랙티브 턴의 하드 백스톱은 `chatport.InteractiveTurnDeadline` = **30분**(소프트 20분)이지 `server.DefaultTurnDeadline` 5분이 아니다 — 후자는 auto-resume 류 경로용이다. 그래서 "5분에 막힌다"는 논거는 과장이었다. 다만 결론은 유지된다: main 턴 평균이 이미 54~73초이고 데일리드라이버가 모바일인 **체감 레이턴시** 문제이지 하드캡 문제가 아니었다. 블로킹 재귀 팬아웃을 모든 메인 턴에 넣는 건 여전히 비싸다.
- **문제 크기가 다르다**. RLM 의 이득은 132k~10M 토큰 단일 입력에서 나온다. Deneb 단일 사용자 세션의 통증은 "거대 단일 입력"이 아니라 **장시간 세션 드리프트**이고, 그건 Polaris 와 티어 컴팩션이 이미 담당한다.

**맞는 지점 (스코프 경계)**

- **큰 블롭 하나를 상대할 때** — YouTube 자막(이미 spill 후 요약 폴백, `gateway-go/internal/pipeline/chat/web/web_youtube.go:298`), 대용량 `exec`/`read` 출력, 메일 아카이브 덤프. 여기선 원본이 이미 창 밖에 있고 캐시 prefix 와도 무관해 **재귀 서브콜을 붙여도 캐시 도그마를 안 건드린다**.
- **자율 경로** — 위키 딥리서치·cron·긴 agentic search 는 데드라인 여유가 있어 팬아웃을 감당한다.
- **RSI 로드맵 정합** — 새 아키텍처가 아니라 **기존 도구 표면의 보강**이라 P4(스킬+도구 번들) 결에 맞는다. 새 레이어를 만드는 제안이 아님을 분명히 해둔다.

---

## 4. 도입 후보와 판정

> **상태 (2026-08-22)**: A~D 는 이 PR 에서 구현됐다. 아래 판정은 그대로 두되, 각 항목의 착지 지점을 병기한다. E·F 는 판정대로 미도입.

### A. read_spillover 에 재귀 서브콜(question) 추가 — 채택 권고 · **구현됨**

`polaris expand` 와 **동형**으로 맞춘다. `read_spillover(spill_id, question="…")` 이면 블롭을 청크로 나눠 **lightweight 역할**로 맵리듀스하고 **답변과 근거 인용만** 루트로 반환한다. 현재는 모델이 20K자 페이지를 몇 번씩 루트 창으로 끌어올려야 하고, 그게 곧 context rot 재생산이다.

- 근거: 이미 블롭·핸들·grep 이 다 있다. 빠진 건 "루트가 원문을 안 보고 답을 얻는" 마지막 홉 하나뿐이라 최저비용·최고효용이다.
- 경계: 모델 역할 하드코딩 금지(`docs/agent-rules/model-roles.md`) — 요약/추출은 lightweight. 인터랙티브 턴에서는 청크 수·팬아웃 폭을 바운드한다(컴팩션의 `maxChunksPerPass` = 4 선례를 따를 것 — `gateway-go/internal/pipeline/compaction/llm.go:29`).
- **착지**: `gateway-go/internal/pipeline/chat/tools/artifact/spillover_ask.go` (맵리듀스·청크 바운드 4·순차 실행), 스키마 `question` 파라미터, 위임자는 `tooldeps.LocalAIFunc` 로 주입. 답변에는 `[L번호]` 인용과 스캔 커버리지가 강제되고, 위임 실패는 페이징으로 폴백하되 **실패 사실을 명시**한다.
- **중복 주의 (미해소)**: YouTube 경로가 이미 같은 패턴을 갖고 있다(`web_youtube.go` 의 `splitTranscriptChunks` + `summarizeTranscriptChunked`). 지금은 두 구현이 병존한다 — 하나는 자막 전용·동시 실행, 하나는 범용 blob·순차. 공용 헬퍼로 뽑는 리팩터가 후속 과제다(`chat/web` 은 web 도구 내부 패키지라 `tools/artifact` 가 그대로 import 하면 레이어 경계 문제가 생긴다).

### B. spill 수명을 세션 수명에 결속 — 채택 권고, A 보다 선행 · **구현됨**

RLM 의 전제는 "변수는 살아 있다"인데 Deneb 는 **구조적으로 깨져 있다**.

- 실제 수명은 30분보다 **훨씬 짧았다**: `finishRun` 이 완료/실패한 모든 런 뒤에 `CleanSession` 을 무조건 불렀고(`gateway-go/internal/pipeline/chat/run_lifecycle.go`), 라이프사이클 구독자도 **터미널 런 상태**를 세션 종료로 취급했다(`gateway-go/internal/runtime/server/server_spillover_lifecycle.go`). 즉 스필은 **자기를 만든 턴이 끝나는 순간** 삭제됐다. TTL 30분(`gateway-go/internal/ai/agent/spillover.go:35`)은 그 뒤에 오는 안전망일 뿐이었다.
- 그런데 컴팩션은 오래된 툴 출력을 지우면서 **`read_spillover` 포인터만 남긴다** — "full output still available via read_spillover(…)" (`gateway-go/internal/pipeline/compaction/restore.go:401`). `gateway-go/internal/pipeline/compaction/protected.go:14` 는 그 포인터를 보호 대상으로 파싱까지 한다.
- 따라서 **다음 턴부터 그 안내는 거짓이 된다.** 모델은 살아있다고 적힌 핸들을 부르고 not found 를 받는다. A 를 붙이든 말든 지금 고쳐야 할 결함이다. (초판은 이 창을 "30분 뒤"로 잘못 적었다 — 실제로는 턴 하나다.)

수정 방향: **런 종료를 세션 종료로 취급하던 두 경로를 모두** 실제 세션 종료(`/reset`·삭제·eviction)로 좁히고, TTL sweep 은 세션이 이미 사라진 스필만 걷는 고아 수집기로 강등한다.

- **착지** (3곳):
  1. `finishRun` 의 `CleanSession` 호출 제거 — 런 종료 ≠ 세션 종료.
  2. `shouldReleaseSpillover` 에서 터미널 상태 트리거 제거 — `EventDeleted`(GC eviction 포함)와 `/reset`(빈 NewStatus)만 남긴다. 체크포인트 라우팅 테이블과의 대칭은 **의도적으로 깼다**: 체크포인트는 런 스코프 해제를 감당하지만 spill 핸들은 살아남은 히스토리에서 모델에게 계속 인용되므로 감당 못 한다.
  3. `SpilloverStore.SetSessionLiveness` 주입 + `cleanExpired` 가 살아있는 세션을 건너뛴다. 술어는 `s.mu` 를 놓고 호출한다(concurrency.md §3 — 세션 매니저 락을 우리 락 아래 중첩시키지 않는다).

  디스크는 여전히 바운드된다: 세션이 끝나면 훅이 걷고, 이벤트 없이 사라진 세션은 TTL sweep 이 걷는다.

### C. 프리뷰 메타데이터 헤더 강화 — 채택 권고, 저비용 · **구현됨**

모델이 실제로 받는 것은 `FormatPreview` 가 아니라 `TruncateHeadTail` 의 잘림 마커다 — head/tail 절반씩 사이에 `... [N lines truncated — use read_spillover("sp_…") for full content] ...` 하나. 생략된 가운데는 **주소가 없다**. RLM 루트는 크기·구조 메타만 보고 탐색 계획을 세우므로, 생략 구간의 섹션 마커를 줄번호와 함께 주면 모델이 `grep` 패턴과 `offset` 을 근거 있게 고른다. `polaris describe` 가 대화 이력에 대해 하는 일을 블롭에도 해주는 것이며, 비대칭 해소의 나머지 절반이다.

주의 둘 — ① 마커 텍스트는 트랜스크립트로 흘러들어가고 컴팩션이 `read_spillover("sp_…")` 문자열을 **정규식으로** 잡는다(`gateway-go/internal/pipeline/compaction/protected.go:14`). 그 형태를 깨면 컴팩션이 포인터를 보호하지 못한다. ② 개요는 매 턴 재전송되는 마커 안에 들어가므로 head/tail 절반에 비해 무시할 크기여야 한다.

- **착지**: `middleOutline` 이 **생략된 가운데 구간만** 훑어 섹션 마커(마크다운 헤딩·`=== x ===` 배너)를 **원문 기준 절대 줄번호**와 함께 최대 10개 나열한다. 절대 줄번호라 `read_spillover(offset=N)` 에 그대로 들어간다. 구조가 없거나(마커 2개 미만) spill 이 없으면(offset 을 넣을 대상 자체가 없음) 개요를 내지 않는다. 포인터 문자열 형태 불변은 테스트로 고정했다.
- **초판 오류**: 이 항목은 처음에 `FormatPreview` 를 대상으로 적었는데, 그 함수는 **런타임 호출자가 없다**. 거기 손대면 제품 효과가 0이다.

### D. polaris expand 잘림 신호 — **구현됨** · 다중 노드 팬아웃 — 실험(미도입)

`expand` 는 단일 `summary_id` 전용이고, 원문 직렬화는 **8000자에서 잘린다**(`gateway-go/internal/pipeline/chat/tools/recallops/polaris.go:243`). 잘림 안내는 서브 LLM 의 프롬프트 안에만 들어가고 **루트는 잘렸다는 사실을 모른다** — 루트가 부분 근거로 단정할 수 있는 경로다. 부수적으로 그 안내 문구는 전체 건수(`len(msgs)`)를 쓰고 있어 **잔여 건수가 아니다**(`gateway-go/internal/pipeline/chat/tools/recallops/polaris.go:250`). 실제 결함이다.

- 최소 수정(즉시): 잘림 사실과 **잔여 건수**를 루트 반환값에도 표기. → **착지**: `serializeExpandMessages` 가 `(text, omitted)` 를 반환하고 `expandCoverageNote` 가 위임 답변·원문 덤프 양쪽에 근거 범위를 붙인다. 기존 테스트가 버그 동작(항상 전체 건수)을 고정하고 있어 함께 교정했다.
- 실험: `describe` 로 얻은 여러 노드에 병렬 서브질의 후 합성(RLM 의 파티션 전략). 자율 경로 한정이며 인터랙티브는 팬아웃 금지.

### E. 루트 무맥락화 / 컨텍스트 전면 변수화 — 스킵

§3 사유. RLM 이 이기는 축(무한 컨텍스트)은 Deneb 의 문제가 아니고, RLM 이 스스로 진 축(prefix cache·런타임 보장)은 정확히 Deneb 가 앞서는 축이다. **바꾸면 잃는 게 얻는 것보다 크다.**

### F. exec/REPL 로 컨텍스트 조작 위임 — 보류

Deneb 엔 이미 `exec`·`code_search`·`grep` 이 있어 "코드로 데이터를 다룬다"는 표면은 존재한다. RLM 의 REPL 은 격리 샌드박스를 전제로 하는데 Deneb `exec` 는 실 워크스페이스에서 돈다. A~C 로 얻는 이득이 겹치므로 **A~C 이후에도 남는 갭이 있을 때만** 재검토한다.

---

## 5. 선결 과제와 리스크

- **효용 측정 부재 (여전히 미해결)**: "재귀 서브콜이 페이지네이션보다 낫다"를 증명할 long-blob QA eval 이 없다. 구현은 결정적 단위 테스트(바운드·커버리지 표기·폴백·부분 실패)로만 검증됐고, **효용 자체는 아직 측정되지 않았다**. 대용량 자막·`exec` 출력 골드셋으로 페이지네이션 대 서브콜의 정확도/토큰을 비교하는 것이 다음 과제다.
- **비용 폭주**: RLM 이 인정한 "비용 보장 없음"이 그대로 옮아온다. 청크 수·깊이(1 고정)·팬아웃 폭을 **하드 바운드**로 박고, 인터랙티브 턴에서는 팬아웃 자체를 끄는 게 안전 기본값이다.
- **근거 소실**: 서브콜 답변만 루트로 올리면 인용 추적이 끊긴다. 답변에 **원문 offset/줄번호를 강제**해 `read_spillover(offset=…)` 로 검증 가능하게 유지한다.
- **순서**: B(수명) → C(메타) → A(서브콜) → D(팬아웃). B 없이 A 를 얹으면 재귀 서브콜이 사라진 파일을 가리킨다.

## 검증 앵커

```bash
cd gateway-go && go test -count=1 ./internal/ai/agent                        # TTL sweep 생존·잘림 마커 개요 (B, C)
cd gateway-go && go test -count=1 ./internal/pipeline/chat                   # finishRun 이 spill 을 안 지움 (B)
cd gateway-go && go test -count=1 ./internal/runtime/server                  # 세션 종료 훅 라우팅 (B)
cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/recallops   # expand 커버리지·잔여 건수 (D)
cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/artifact    # 재귀 서브콜 (A)
cd gateway-go && go test -count=1 ./internal/pipeline/compaction             # read_spillover 포인터 보존
```
