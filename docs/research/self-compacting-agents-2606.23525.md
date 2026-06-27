# Self-Compacting Language Model Agents × Deneb 검토

> **출처**: Tianjian Li, Jingyu Zhang, William Jurayj, Xi Wang, Chuanyang Jin, Mehrdad Farajtabar, Eric Nalisnick, Daniel Khashabi, *"Self-Compacting Language Model Agents"* ([arXiv:2606.23525](https://arxiv.org/abs/2606.23525), 2026-06-22). 직접 fetch 는 조직 egress 정책(arxiv.org 403)으로 차단 → 공개 요약/리뷰([HuggingFace](https://huggingface.co/papers/2606.23525), [alphaXiv](https://www.alphaxiv.org/abs/2606.23525), [Moonlight](https://www.themoonlight.io/en/review/self-compacting-language-model-agents)) 기반 검토.
> **방법**: 논문 핵심 추출 → Deneb 압축 파이프라인(`gateway-go/internal/pipeline/compaction/`)·캐시 도그마(`.claude/rules/prompt-cache.md`)와 대조 → 채택/실험/스킵 판정.
> **일시**: 2026-06-27
> **한 줄 결론**: 논문이 비판하는 *fixed-interval/threshold 트리거*가 **곧 Deneb 의 현재 압축 트리거**(`polaris.go`: Emergency 30k·LLM 90%·micro 4-turn)다. 단 논문의 두 비용 축 중 **토큰 비용은 Deneb 가 이미 중화**했고(deferred background compaction `run_prepare.go:498` 가 dsv4 STW 제거), 남은 진짜 nugget 은 **품질=압축 *시점*의 궤적-인지화**다(§5.0). 그래서 논문 그대로의 "모델-주도 `compact` 도구"(B)는 **비권장**(dsv4 uneven tool use + 5분데드라인 + APC 위험, ROI<위험)이고, rubric 의 *판정 기준*만 떼어 **lightweight 역할의 바운드 게이트 1콜**로 돌리는 **A2(suppression)** 를 본안으로 권장 — main 턴 밖이라 데드라인·APC·tool-set 위험이 전부 사라진다. 이득은 *긴 자율 궤적*(research/cron/agentic search)에 한정, **인터랙티브 비서 턴은 불변**. 선결 과제 = long-trajectory 압축-품질 eval 부재(§5.3).

---

## 1. 논문 핵심

긴 에이전트 궤적(chain-of-thought + tool call)은 **stale content 가 누적되어 후속 생성을 잘못 anchor** 하고 결국 컨텍스트 윈도를 넘긴다. 기존 스캐폴드는 **토큰 임계값에서 발화하는 fixed-interval 압축**으로 대응하는데, 이 트리거는 *궤적 구조를 무시*해서 derivation/search 도중에 부분 결과를 날려버릴 위험이 있다.

**SelfCompact** = 학습/외부감독 없는(training-free) inference-time 스캐폴드. 두 요소를 **짝지을 때만** 작동한다:

1. **압축 도구** — 모델이 직접 호출해 누적 컨텍스트를 요약하는 tool. 요약 지시를 궤적 뒤에 *append* 하므로 앞 궤적의 캐시를 재사용한다(비용 ≈ `O(Lℓ) + O(ℓ²)`, L=원궤적 길이, ℓ=요약 길이). 50–100k 궤적이 ~1–3k 로 붕괴 → **20–80× 축소**.
2. **rubric** — 주기적 probe 시점에 "지금 압축할까?"를 모델이 자기 궤적에 대조해 판정하게 하는 경량 프롬프트. **발화 조건**(sub-task 해결됨 / 궤적이 수렴 중) vs **억제 조건**(derivation 중 / stuck)을 *cite-able 한 구체 조건*으로 번역.

**핵심 ablation**: 도구 단독은 부족하다. 오픈웨이트 7종에서 rubric 을 떼면 **어떤 모델은 엉뚱한 시점에 호출, 어떤 모델은 아예 호출 안 함**. rubric 단독은 행동을 못 한다(도구가 없으니). **둘이 합쳐야** fine-tuning 없이 adaptive 압축이 나온다.

**결과** (6 벤치마크 = 경쟁수학 + agentic search, 7 모델):

| 축 | 결과 |
|---|---|
| 정확도 순서 | baseline < fixed-interval ≤ **SelfCompact** (전 모델 일관) |
| 경쟁수학 | no-compaction 대비 **최대 +18.1점** |
| agentic search | **+5~9점** while **per-question 토큰 30~70% 절감** (BrowseComp-Plus: GLM-4.7-Flash +8.5, MiniMax-M2.5 +9.2, Mimo-V2-Flash +5.3) |
| fixed-interval 대비 | 잘못 타이밍된 압축을 피해 **최대 +6.3점** |

즉 fixed-interval 은 baseline 손실분의 대부분을 회복하고, SelfCompact 의 *자율 트리거*가 그 위에 추가 이득을 얹는다 — **싸지면서 동시에 정확**.

---

## 2. Deneb 현황 대조 — 우리가 곧 그 baseline 이다

| 논문 요소 | Deneb 현재 | 위치 |
|---|---|---|
| fixed-interval/threshold 트리거 | ✅ **정확히 이것** — Emergency `lastInput≥30k`, LLM `≥90% budget`, micro-prune `4 turn`, target `20%` | `compaction/polaris.go:19-22,126,171` |
| 궤적-인지 트리거 | ❌ 없음. 토큰 임계만 본다(구조 무시) | — |
| 모델이 호출하는 압축 도구 | ❌ **없음** — 압축은 전적으로 파이프라인이 결정, 모델에 `compact` tool 미노출 | `toolreg/tool_schemas.json` (해당 tool 부재 확인) |
| cheap pruning 선행 | ✅ 있음 — MicroCompact(코드펜스만) + TruncateOldToolResults(256 rune↑ stub) | `compaction/micro.go`, `restore.go` |
| LLM tier bounded digestion | ✅ 있음 — `maxChunksPerPass=4`, per-chunk 1–2k cap, prefix-tolerance (P7) | `compaction/llm.go` |
| 캐시-세이프 per-turn 주입 기구 | ✅ **있음** — wire-only tail suffix | `chat/run_tail_inject.go` |

**진단**: Deneb 압축은 논문이 "trajectory structure 를 무시한다"고 지적한 그 임계값 트리거 그대로다. fallback 체인(LLM→Embedding+MMR→Recency)·bounded digestion 으로 *비용/안정성*은 정제됐지만, **"언제 압축하나"의 의사결정은 여전히 토큰 카운트 하나**다. 논문의 +6.3점(fixed-interval 대비)이 바로 이 갭에서 나온다.

---

## 3. 적용성 — 왜 Deneb 에 유독 잘 맞나 (그리고 어디서 안 맞나)

**잘 맞는 이유 (강한 신호)**

- **메인 모델이 오픈웨이트 로컬(dsv4-flash)**. 논문의 중심 발견 = "오픈웨이트는 bare 압축 도구를 일관되게 못 쓴다, rubric 이 본질". 즉 Deneb 에 `compact` tool 만 무심코 추가하면 **dsv4 가 엉뚱한 시점에 부르거나 안 부른다** — 논문이 정확히 경고한 실패. **rubric 게이팅이 필수**라는 결론이 우리 환경에 직격.
- **캐시-세이프 주입 기구가 이미 있다**. SelfCompact 의 rubric-probe 는 per-turn 변동 텍스트라 system/history 에 넣으면 vLLM APC byte-prefix 를 깬다(§prompt-cache 1.5). 그러나 Deneb 는 이미 recall/auto-delivery 를 **마지막 user 메시지의 wire-only suffix** 로 주입하는 `buildTailAdditions`/`injectTailAdditions` 를 가졌다 → rubric-probe 를 **캐시 파괴 없이** 같은 경로로 얹을 수 있다. 도입 마찰이 낮다.
- **압축 도구의 cache 재사용 비용 모델이 우리 도그마와 일치**. 논문은 요약 지시를 궤적 *뒤에* append 해 앞 캐시를 재사용한다 — 이는 prompt-cache.md 의 "과거 메시지 mutate 금지, append-only" 와 정확히 같은 철학.

**안 맞거나 주의할 지점 (스코프 경계)**

- **큰 이득은 *긴 궤적*에서만**. +18.1점은 50–100k 토큰 경쟁수학 궤적, +5~9점은 멀티스텝 브라우징. Deneb 의 **인터랙티브 비서/메일 턴은 대개 짧아** 압축 자체가 거의 안 걸린다(90% budget 도달 전 종료). 이득이 실재하는 곳은 **자율 경로** — 위키 딥리서치(`wiki_research_task.go`), research_panel, cron 에이전트 턴, 긴 agentic search.
- **인터랙티브 5분 데드라인 압박**. 모델이 압축을 호출하면 **mid-turn LLM 라운드트립**이 하나 더 붙는다. Deneb 인터랙티브 경로는 `DefaultTurnDeadline=5분`에 묶여 있고, P7 은 *압축 fan-out 이 데드라인을 전멸시킨 사고*의 교훈이다. 자율 턴은 여유가 있지만 **인터랙티브 비서 턴에 모델-주도 압축을 넣는 건 위험**.
- **rubric-probe 의 캐시 비용**. 주기적 probe 를 매번 tail 에 주입하면 그 턴의 trailing-marker prefix 가 흔들린다(1 cache miss/probe). probe 빈도를 낮게(예: N turn 또는 토큰 밴드마다 1회) 묶어야 함 — 논문도 "periodic probe interval" 이라 상시가 아님.

---

## 4. 판정

| 항목 | 판정 | 근거 |
|---|---|---|
| **트리거/경계의 궤적-인지화** (자율·딥리서치 경로) | 🟢 **실험 채택 권장** | 논문의 진짜 nugget = 품질(시점). Deneb 트리거가 순수 토큰위치라 mid-derivation 컷 손실 존재 (§5) |
| 구현 형태 = **lightweight-게이팅 suppression(A2)** | 🟢 본안 | rubric 의 가치는 *판정 기준*이지 *판정 주체*가 아님 → main 턴 밖 helper 1콜로 5분데드라인·APC·tool-set 위험 회피 (§5.1) |
| **모델-주도 `compact` 도구(논문 그대로, B)** | 🟡 **비권장** | dsv4 uneven tool use + mid-turn 라운드트립 + APC 교란 + Rule B 위반. 우리 토큰비용 이미 낮아 ROI<위험 |
| 동일 기법을 **인터랙티브 비서 턴**에 | 🟡 **스킵** | 짧은 궤적이라 압축 밴드 도달 전 종료 → 이득 미미 |
| cheap pruning(Micro/Tier2b)·bounded digestion(P7) 대체 | ⛔ 무관 | 보완재. 트리거 *시점*만 바꾸지 fence-strip·요약 단계는 그대로 |
| 토큰-비용 절감 목적 | ⛔ 이미 해결 | deferred background compaction(`run_prepare.go:498`)이 dsv4 STW 를 이미 제거 (§5.0) |

---

## 5. 개선 방법 탐구 — Deneb 가 *실제로* 가져갈 부분은 어디인가

### 5.0 먼저: 논문의 두 비용 축 중 하나는 Deneb 가 이미 풀었다

논문이 fixed-interval 의 비용으로 드는 건 둘이다:

1. **토큰 비용** — 모델이 압축을 위해 컨텍스트를 읽고 요약하는 비용.
2. **품질 손실** — *나쁜 시점*(mid-derivation/mid-search)에 압축해 부분 결과를 날려 다음 스텝의 anchor 를 잃는 것.

**축 1 은 Deneb 가 이미 다른 방식으로 중화했다.** 메인 모델 dsv4 는 대형 윈도이고 decode rate 가 budget 한참 위까지 평평해서, `run_prepare.go:498-522` 의 **deferred background compaction** 이 "이번 턴은 raw 컨텍스트로 돌리고 요약은 백그라운드에서" 처리한다 → STW 0, 요약은 다음 턴에 도착. 거기에 cheap pruning 2티어(APC-게이팅)·incremental summary update(P7)·DAG condense 까지 얹혀 있어, **압축의 토큰/레이턴시 비용은 핫패스에서 거의 사라졌다.**

**남은 건 축 2 — 품질(시점)이다.** 그런데 Deneb 의 트리거는 *순수 토큰 위치*(`EstimateMessagesTokens > 90% budget`)다. 90% 가 mid-derivation 에 걸리면, deferred 든 STW 든 **그 시점의 경계로 요약**해 in-flight 추론을 잃는다. `snapWindowStart` 는 tool_use↔tool_result 짝만 스냅하지 *derivation 세그먼트*는 모른다. **즉 논문에서 Deneb 가 취할 진짜 nugget = "트리거/경계를 궤적 구조에 맞춰라"는 품질 인사이트뿐**이고, "모델이 도구로 직접 압축" 같은 메커니즘은 우리 토큰-비용이 이미 낮아 ROI 가 작다.

### 5.1 방법 후보 (비용·위험·적합도 순)

| # | 방법 | 무엇 | LLM 비용 | APC/데드라인 위험 | 논문 충실도 | 적합도 |
|---|---|---|---|---|---|---|
| **A1** | **휴리스틱 suppression** | 트리거가 켜져도 *궤적이 mid-derivation 이면 1턴 defer*. open tool_use 루프 / 마지막 메시지가 스텝 도중 = 억제; tool 체인이 자연어 답으로 닫힘 = 압축 호적기. `snapWindowStart` 를 derivation 세그먼트로 확장 | **0** | 없음(주입 없음) | rubric 의 suppress 절반을 무-LLM 근사 | 🟢 **최우선** |
| **A2** | **lightweight-게이팅 suppression** | 90% 밴드에서 **lightweight(로컬·바운드) 1콜**: "이 궤적이 sub-task 해결 직후/수렴 중인가, derivation 중인가?" → 압축 now vs defer. fail-open(불확실/실패 시 오늘처럼 압축) | 1콜/압축후보 (lightweight, ~로컬) | 없음(게이트는 파이프라인이, main 턴 밖) | rubric 전체를, 단 **gate 를 main 이 아니라 helper 모델이** 실행 | 🟢 **권장 본안** |
| **C** | **트리거와 직교: 압축 *경계* 인지** | *언제* 와 무관하게 *어디서* 자르나. `snapWindowStart`/`BalanceToolBlocks` 에 derivation-segment 경계 스냅 추가 — mid-reasoning 컷 금지 | 0 | 없음 | 경계 품질만 | 🟢 A 와 합산 |
| **B** | **모델-주도 `compact` 도구 (논문 그대로)** | 자율 세션에 `compact` tool 노출 + tail rubric-probe 로 게이팅. main 이 시점 지명 | mid-turn 1콜 | **있음**: ①dsv4 uneven tool use(논문 자체 발견) ②5분 데드라인에 라운드트립 ③probe 가 trailing-marker prefix 교란 ④tool-set 변경=APC 패밀리 분리(Rule B) | 100% | 🟡 **비권장** |

**왜 A2 가 본안인가 (논문의 Deneb-네이티브 번역).**
논문의 핵심 발견은 "도구 단독은 오픈웨이트가 못 쓴다, **rubric 이 본질**"이다. Deneb 의 메인도 오픈웨이트(dsv4)라 방법 B 는 그 함정에 그대로 빠진다. 그런데 **rubric 의 가치는 '판정 기준'이지 '누가 판정하느냐'가 아니다.** Deneb 는 판정을 **메인 턴 안에서 메인 모델에게** 시킬 이유가 없다 — 이미 압축 결정을 *파이프라인*이 내리고 있으니, rubric 을 **lightweight 역할의 바운드 게이트 1콜**로 돌리면:

- 5분 데드라인·APC 교란·tool-set 분리 위험이 **전부 사라진다**(main 턴 밖, 주입 0, 도구 0).
- model-roles 도그마와 정렬 — goal_task judge·컴팩션 청크 요약과 동형의 *로컬 lightweight 바운드 판정*(`.claude/rules/model-roles.md`).
- 논문의 "엉뚱한 시점 압축 회피로 +최대 6.3점"을, **dsv4 의 도구호출 신뢰성에 베팅하지 않고** 얻는다.
- fail-open 이라 게이트가 죽어도 오늘 동작으로 graceful degrade.

A1 은 그 전 단계(무-LLM 1차 방어선이자 A2 의 fallback), C 는 직교 보완. **B 는 우리 토큰-비용이 이미 낮아 추가 ROI 가 위험을 정당화 못 함** → 명시적 비권장.

### 5.2 본안(A2) 최소 구현 스케치 (미구현)

`run_prepare.go` 의 압축 분기(`currentTokens > softThreshold` 자리)에 게이트를 끼운다:

1. **게이트 진입 조건**: 토큰이 LLM 임계 밴드(예: 80–90%)이고 `engine.HasSummaries` (부트스트랩 이후) — 즉 *defer 가능한* 구간에서만. 90% 하드 천장·first-compaction·no-headroom 은 게이트 없이 **무조건 압축**(safety net, 논문이 fixed-interval 을 baseline 회복용으로 남긴 것과 동형).
2. **lightweight 1콜**: 최근 N 메시지(꼬리)만 직렬화해 `pilot.CallLocalLLM` 류로 "수렴/해결 직후 = COMPACT, derivation/stuck = DEFER" 이진 판정. 입력 바운드(꼬리만), 출력 1토큰급. 타임아웃 짧게, 에러·모호 = COMPACT(fail-open).
3. **DEFER 시**: 이번 턴 압축 스킵 + 다음 평가까지의 마진(예: +N 토큰)만 올림. 단 **하드 천장에서는 절대 defer 안 함** — 무한 defer 로 윈도 초과 방지(논문도 stuck 일 때 억제하되 천장은 별개).
4. **스코프 격리**: 게이트는 자율/딥리서치 세션 키에만(인터랙티브 비서 턴은 짧아 밴드 도달 전 종료 → 게이트가 거의 안 켜져 비용·레이턴시 영향 0, 그래도 키로 명시 격리).

> 코드 변경은 `run_prepare.go` 압축 분기 + 작은 헬퍼 하나. tool 추가·스키마·시스템프롬프트·캐시 마커 **불변**(생성코드·hub-wiring·prompt-cache 체크리스트 무관) → 표면이 좁고 되돌리기 쉽다.

### 5.4 휴리스틱(A1) 구체 설계 — 무-LLM, 결정적

**먼저 솔직한 전제: Deneb 는 이미 mid-derivation 을 3겹으로 막는다.** ① `CompactPriorToolResults`(`agentsys/agent/executor.go:608`, 매 루프 iteration)는 **`currentTurnStart` 이후 = 현재 턴 in-flight tool_result 를 절대 안 줄인다**(prior 턴 4K rank-line 축약만) → *턴 내부* mid-derivation 컷은 구조적으로 이미 차단. ② `snapWindowStart`+`BalanceToolBlocks` 는 요약 경계가 tool_use↔tool_result 짝을 깨지 않게 한다. ③ deferred background compaction 으로 STW 제거.

**그래서 A1 의 잔여 타깃은 둘뿐이다** — Polaris LLM 요약(`run_prepare.go`)이 *완료된 prior 턴*을 요약할 때: (C) 경계가 tool 체인 한가운데 떨어지는 것, (S) *멀티턴* 과제가 아직 수렴 전인데 요약해버리는 것. 둘 다 마지막 K 메시지의 구조만 본다(LLM·주입 0).

**구조 술어 3개** (메시지당, 기존 파서 재사용·소형 추가):

| 술어 | 정의 | 비고 |
|---|---|---|
| `isToolResult(m)` | user role + content 가 tool_result 블록 | 기존 `isToolResultMessage` 그대로 |
| `hasToolUse(m)` | assistant role + 블록배열에 `type:"tool_use"` 존재 | `isToolResultMessage` 미러로 신설(타입만 교체) |
| `isTerminalText(m)` | assistant role + 텍스트만(tool_use 없음) | = "모델이 그 턴을 자연어로 *닫았다*" = sub-task 종결 신호. 턴 경계 granularity 라 신뢰 가능(tool_use 있던 메시지는 continuation, 없으면 turn-final) |

**(C) 경계 스냅 — 고신뢰·저위험, 먼저 출하.**
`snapWindowStart` 를 확장: kept-window 시작이 *contiguous tool 체인*(중간에 `isTerminalText` 없는 `[tool_use→tool_result]*` 연속) 한가운데 떨어지면, **가장 가까운 앞선 `isTerminalText` assistant 메시지로 경계를 뒤로 스냅**한다. 오늘은 leading tool_result orphan 만 건너뛰지만, 이건 "derivation 세그먼트 중간을 자르지 마라"로 일반화. 순수 결정적, 기존 `snapWindowStart` 호출부에 그대로 끼움.

**(S) thrash 시 defer — 보조·가설성, eval 후 게이트.**
턴-조립 임계 밴드(80–90%)에서 마지막 K 메시지의 "정착 vs 채집" 점수로 LLM tier 를 한 밴드 미룬다:
- **정착(FIRE)**: 직전 assistant 메시지가 `isTerminalText` → 안전한 압축 호적기.
- **채집/thrash(DEFER)**: 최근 윈도가 열린/반복 tool 패턴 — 같은 tool 이름 ≥R회(예: search×4) 사이에 `isTerminalText` 없음, 또는 최근 매 턴이 tool 만 내고 합성 텍스트 0 → 아직 수렴 전.
- ★**하드 천장은 timing 무시**: `currentTokens` 가 윈도 ceiling 의 reserve 를 넘기면(기존 `deferEligible` reserve 체크 실패) **무조건 압축** — never-settles 자율 루프가 영원히 defer 해 윈도를 넘기는 함정을 막는다(논문도 stuck 일 때 억제하되 천장은 별개).

**진입점/형태**:
```go
// compaction 패키지, 순수 함수 (LLM·할당 최소)
type Timing int
const ( Settled Timing = iota; MidDerivation )
func ClassifyTiming(messages []llm.Message, tailWindow int) Timing
```
`run_prepare.go` 의 `deferEligible` 분기에서 `ClassifyTiming(...) == MidDerivation && currentTokens < ceilingReserve` 면 defer 우선. (C)는 `engine` 의 `snapWindowStart` 경로에서 항상 적용. **자율 세션 키에만** 게이트(인터랙티브는 밴드 도달 전 종료 → 거의 안 켜짐).

**반증가능 단위테스트**(편집과 함께):
- mid-chain 컷 입력 → 경계가 `isTerminalText` 로 스냅됨.
- thrash 꼬리(search×4, 텍스트 0) → `MidDerivation`.
- 정착 꼬리(마지막이 terminal text) → `Settled`.
- ceiling 초과 입력 → timing 무시하고 압축(안전바운드).

**솔직한 한계**: ①②의 기존 방어막 때문에 A1 **단독의 측정 가능한 상방은 작을 수 있다** — 특히 (S)는 deferred-background 가 이미 "raw 로 한 턴 더" 사주는 것과 효과가 겹친다. 그래서 **(C) 경계 스냅만 먼저 출하**(결정적·테스트가능·행동위험 거의 0), **(S) defer 는 §5.3 long-trajectory eval 로 +Δ 가 재현될 때만** 켠다. eval 없이 (S)를 켜는 건 운빨 개선 위험(`.claude/rules/optimization.md` 인과진단).

### 5.3 측정 — 이게 제일 어렵다 (그리고 선결 과제다)

논문은 *긴 궤적* 벤치(경쟁수학·agentic search)에서 정확도로 잰다. **Deneb 엔 그런 long-trajectory 압축-품질 eval 이 없다** — `quality-metric.sh`/`recall-metric.sh` 는 짧은 턴·회상 적중이라 mid-derivation 압축의 손실을 못 잡는다. 따라서:

- **선결**: 자율 딥리서치/agentic search 시나리오(예: `wiki_research_task` 류 멀티스텝, research_panel)를 **재현 가능한 long-trajectory 픽스처**로 만들어, "압축 후 최종 산출물 품질/사실 보존"을 채점하는 metric 을 먼저 세운다. 이게 없으면 A2 의 +효과를 *증명할 길이 없다*(운빨 개선 위험, `.claude/rules/optimization.md` 인과진단).
- **회귀 가드**: 인터랙티브 `quality` 스위트·`recall-metric` 불변(스코프 격리 증명), `iterate.sh --metric` 로 게이트 on/off 비교, vLLM `prefix_cache` 메트릭으로 APC 무영향 확인.
- **반증가능 예측**(편집 전 선언): "A2 는 long-trajectory 산출물 사실보존을 +Δ, 인터랙티브 latency·APC 적중을 ±0 으로 바꾼다 — mid-derivation 컷을 피하기 때문." Δ 가 안 나오거나 latency 가 흔들리면 revert.

**결론**: 채택 순서 = **(0) long-trajectory eval 선구축 → (1) A1+C 무-LLM 시점/경계 인지 → (2) A2 lightweight 게이트 → B 는 보류.** 단일 사용자·로컬 GPU 에서 핫패스 토큰비용이 이미 낮으므로, 가치는 "긴 자율 궤적의 *품질*"에 집중하고 인터랙티브 경로는 건드리지 않는다.

---

## 6. 관련 문서

- 압축 정책 단일 진실원: `.claude/rules/prompt-cache.md` §5(컨텍스트 압축), §1.5(vLLM APC 꼬리 주입)
- 구현: `gateway-go/internal/pipeline/compaction/` (`polaris.go` 트리거, `llm.go` P7 bounded digestion, `micro.go`/`restore.go` cheap pruning)
- 캐시-세이프 주입: `gateway-go/internal/pipeline/chat/run_tail_inject.go`
- 모델 역할(압축=lightweight 로컬): `.claude/rules/model-roles.md` (컴팩션 청크 요약 → lightweight)
- 선행 검토 포맷: `docs/research/code-as-agent-harness-review.md`
