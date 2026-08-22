# Recursive Language Models (RLM) × Deneb 도입 요소 검토

> **출처**: 영상 [*"Context as a Variable: The Architecture Killing Context Rot"*](https://youtube.com/shorts/AvIujLlbmks) (Cloud Codes) → 원 출처 Alex L. Zhang · Tim Kraska · Omar Khattab (MIT CSAIL), *"Recursive Language Models"* ([arXiv:2512.24601](https://arxiv.org/abs/2512.24601), [저자 블로그](https://alexzhang13.github.io/blog/2025/rlm/), [코드](https://github.com/alexzhang13/rlm)).
> **방법**: RLM 핵심 기제 추출 → Deneb 의 out-of-context 기구(`gateway-go/internal/ai/agent/spillover.go`, `gateway-go/internal/pipeline/polaris/`, `gateway-go/internal/pipeline/compaction/`)와 코드 대조 → 채택/실험/스킵 판정. 영상 자체는 자막 접근이 막혀 oEmbed 메타 + 원 논문/저자 블로그로 대체 확인.
> **일시**: 2026-08-22
> **한 줄 결론**: RLM 의 프리미티브 중 **대부분을 Deneb 는 이미 갖고 있다** — 컨텍스트를 창 밖 변수로 두는 저장소(spillover + Polaris), 프로그래밍적 peek/grep/slice(`read_spillover(offset/limit/grep)`), 무손실 원본 보존(Polaris FTS), 그리고 **깊이-1 재귀 서브콜**(`polaris(action="expand", question=…)` — 원문을 루트에 안 올리고 서브 LLM 답변만 반환). 없는 건 **파티션+맵리듀스 팬아웃**과 **루트 무맥락화**뿐이고, 후자는 **도입하면 안 된다**(RLM 이 스스로 인정한 한계 = prefix caching 부재·초~분 단위 레이턴시인데, 이는 Deneb 의 prompt-cache 불가침 원칙과 5분 턴 데드라인·모바일 데일리드라이버를 정면으로 깬다). 실제 nugget 은 **두 계층의 비대칭 해소** 3건: ① spillover 블롭에도 재귀 서브콜을 주고(§4-A), ② 그 전제인 "변수가 살아있다"를 실제로 성립시키고(§4-B — 현재 TTL 30분 vs 컴팩션이 남기는 영구 포인터 = 구조적 dangling), ③ 루트가 탐색 계획을 세울 메타데이터를 프리뷰에 넣는다(§4-C).

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

| RLM 프리미티브 | Deneb 현재 | 위치 |
|---|---|---|
| 컨텍스트를 창 밖 변수로 저장 | 있음 — 24K 초과 툴 결과는 디스크로 spill, 창에는 head 1K + tail 1K 프리뷰와 `sp_*` 핸들만 | `gateway-go/internal/ai/agent/spillover.go`, 임계 `gateway-go/internal/ai/agent/truncate.go:18` (`DefaultMaxOutput` = 24K) |
| 무손실 원본 보존 | 있음 — 모든 메시지가 SQLite FTS 에 무손실 영속, 요약은 DAG 노드로 *추가*되지 그 자리를 지우지 않음 | `gateway-go/internal/pipeline/polaris/store.go` |
| 프로그래밍적 peek / grep / slice | 있음 — `read_spillover(spill_id, offset, limit, grep)`, 페이지당 400줄·20K자 바운드와 `[계속: offset=N]` 안내 | `gateway-go/internal/pipeline/chat/toolwire/schema/tool_schemas.json` |
| 구조 메타 조회(목차) | 부분 — 대화 이력엔 있음(`polaris(action="describe")`: 총 메시지·커버리지·요약노드 목록), **spill 블롭엔 없음**(char 수 하나) | `gateway-go/internal/pipeline/chat/tools/recallops/polaris.go:92` vs `spillover.go` 의 `FormatPreview` |
| **깊이-1 재귀 서브콜** | 있음, 단 대화 이력 한정 — `expand(summary_id, question)` 이 원본 구간을 로드해 **로컬 LLM 에 위임**하고 *답변만* 루트로 반환 | `gateway-go/internal/pipeline/chat/tools/recallops/polaris.go:196` |
| 파티션 + 병렬 맵리듀스 | 없음 — `expand` 는 단일 노드·8000자 단일 청크. `research_panel` 은 "한 질문 → 여러 모델"이지 "여러 청크 → 한 모델"이 아님 | `gateway-go/internal/pipeline/chat/tools/recallops/polaris.go:243` |
| 루트가 원본을 아예 안 봄 | 없음 — 기본 경로는 여전히 append-only 히스토리 조립과 임계값 컴팩션 | `gateway-go/internal/pipeline/chat/run_prepare.go` → `gateway-go/internal/pipeline/compaction/` |
| 비용·런타임 보장 | **Deneb 가 앞섬** — 턴 데드라인 5분, variable prompt addition 예산, 컴팩션 튜너 | `gateway-go/internal/runtime/server/server_lifecycle.go:19`, `gateway-go/internal/pipeline/chat/promptbudget/`, `gateway-go/internal/pipeline/compactuner/` |

**진단**: "context as a variable" 은 Deneb 에 이미 **두 계층으로** 구현돼 있다 — 툴 결과층(spillover)과 대화 이력층(Polaris). RLM 이 새로 주는 건 *개념*이 아니라 **그 두 계층의 비대칭을 메우는 구체적 형태**다. 대화 이력층은 `describe`(구조 메타)와 `expand(question)`(재귀 서브콜)을 갖췄는데, 툴 결과층은 `grep`/`offset`(탐색)만 있고 **재귀 서브콜도 구조 메타도 없다**. 정작 덩치가 큰 쪽은 툴 결과층이다 — YouTube 자막, `exec` 출력, 메일 아카이브 덤프.

---

## 3. 적용성 — 전면 도입이 왜 안 되나, 어디는 맞나

**전면 도입(루트 무맥락화)이 안 되는 이유 — 판정의 핵심**

- **prompt-cache 불가침과 정면 충돌**. Deneb 의 레이턴시·비용은 byte-stable prefix 위의 vLLM APC 재사용에 얹혀 있다(캐시 마커 정확히 4개, per-turn 가변 바이트는 system 이 아니라 마지막 user 꼬리로만 — `docs/agent-rules/prompt-cache.md`). RLM 은 **prefix caching 을 안 쓰는 게 명시된 한계**다. 매 턴 루트 창을 질의만으로 재구성하면 히스토리 prefix 재사용이 통째로 사라진다.
- **레이턴시 성격이 반대**. RLM 은 질의당 "수 초에서 수 분"을 허용하는 배치성 장문 QA 전략이다. Deneb 는 `DefaultTurnDeadline` 5분 안에서 도는 **인터랙티브 비서**이고 데일리드라이버가 모바일이다. 블로킹 재귀 팬아웃을 메인 턴 경로에 넣는 건 과거 컴팩션 팬아웃이 데드라인을 전멸시킨 사고의 재연이다.
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

### B. spill 수명을 세션 수명에 결속 — 채택 권고, A 보다 선행 · **구현됨**

RLM 의 전제는 "변수는 살아 있다"인데 Deneb 는 **구조적으로 깨져 있다**.

- spillover TTL 은 **30분**이고 게이트웨이 재시작 시 소멸한다(`gateway-go/internal/ai/agent/spillover.go:35`, `Load` 의 에러 문구가 이를 명시).
- 그런데 컴팩션은 오래된 툴 출력을 지우면서 **`read_spillover` 포인터만 남긴다** — "full output still available via read_spillover(…)" (`gateway-go/internal/pipeline/compaction/restore.go:401`). `gateway-go/internal/pipeline/compaction/protected.go:14` 는 그 포인터를 보호 대상으로 파싱까지 한다.
- 따라서 **30분 넘는 세션에서 그 안내는 거짓이 된다.** 모델은 살아있다고 적힌 핸들을 부르고 not found 를 받는다. A 를 붙이든 말든 지금 고쳐야 할 결함이다.

수정 방향: TTL sweep 이 **세션이 살아있으면 건너뛰도록** 뒤집는다. 세션 종료 hook 은 이미 있다 — `gateway-go/internal/runtime/server/server_spillover_lifecycle.go` 가 세션 종료·`/reset`·eviction 에서 즉시 정리한다. 즉 "TTL 로 지우고 세션훅으로 조기 정리"를 **"세션훅이 정본, TTL 은 고아 파일 스윕만"** 으로 바꾼다.

- **착지**: `SpilloverStore.SetSessionLiveness` 주입 + `cleanExpired` 가 살아있는 세션을 건너뛴다. 술어는 `s.mu` 를 놓고 호출한다(concurrency.md §3 — 세션 매니저 락을 우리 락 아래 중첩시키지 않는다). 술어 미주입 시에는 기존 나이 기반 동작 유지.

### C. 프리뷰 메타데이터 헤더 강화 — 채택 권고, 저비용 · **구현됨**

현재 프리뷰 헤더는 `[SpillOver: ID=… | tool | N chars]` 와 head/tail 1K 뿐이다(`spillover.go` 의 `FormatPreview`). RLM 루트는 **크기·구조 메타만 보고 탐색 계획을 세운다**. 줄 수, 가능하면 섹션/헤딩 목차, 토큰 추정치를 헤더에 넣으면 모델이 `grep` 패턴과 `offset` 을 근거 있게 고른다. `polaris describe` 가 대화 이력에 대해 하는 일을 블롭에도 해주는 것이며, 비대칭 해소의 나머지 절반이다.

주의 하나 — 프리뷰 텍스트는 트랜스크립트로 흘러들어가고 컴팩션이 `read_spillover("sp_…")` 문자열을 **정규식으로** 잡는다(`gateway-go/internal/pipeline/compaction/protected.go:14`). 그 형태를 깨면 컴팩션이 포인터를 보호하지 못한다.

- **착지**: 헤더에 줄 수를 넣고, `previewOutline` 이 섹션 마커(마크다운 헤딩·`=== x ===` 배너)를 1-기반 줄번호와 함께 최대 12개 나열한다. 구조가 없으면(마커 2개 미만) 개요를 아예 내지 않아 프리뷰에 노이즈를 얹지 않는다. 포인터 문자열 형태 불변은 테스트로 고정했다.

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
cd gateway-go && go test -count=1 ./internal/ai/agent            # spillover 수명·프리뷰 (B, C)
cd gateway-go && go test -count=1 ./internal/pipeline/compaction # read_spillover 포인터 보존 (B)
cd gateway-go && go test -count=1 ./internal/pipeline/polaris    # expand 커버리지·직렬화 (D)
```
