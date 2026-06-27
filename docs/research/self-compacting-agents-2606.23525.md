# Self-Compacting Language Model Agents × Deneb 검토

> **출처**: Tianjian Li, Jingyu Zhang, William Jurayj, Xi Wang, Chuanyang Jin, Mehrdad Farajtabar, Eric Nalisnick, Daniel Khashabi, *"Self-Compacting Language Model Agents"* ([arXiv:2606.23525](https://arxiv.org/abs/2606.23525), 2026-06-22). 직접 fetch 는 조직 egress 정책(arxiv.org 403)으로 차단 → 공개 요약/리뷰([HuggingFace](https://huggingface.co/papers/2606.23525), [alphaXiv](https://www.alphaxiv.org/abs/2606.23525), [Moonlight](https://www.themoonlight.io/en/review/self-compacting-language-model-agents)) 기반 검토.
> **방법**: 논문 핵심 추출 → Deneb 압축 파이프라인(`gateway-go/internal/pipeline/compaction/`)·캐시 도그마(`.claude/rules/prompt-cache.md`)와 대조 → 채택/실험/스킵 판정.
> **일시**: 2026-06-27
> **한 줄 결론**: 논문이 비판하는 *fixed-interval/threshold 트리거*가 **곧 Deneb 의 현재 압축 트리거**(`polaris.go`: Emergency 30k·LLM 90%·micro 4-turn)다. 핵심 인사이트(**rubric 으로 게이팅된, 모델이 직접 호출하는 압축**)는 Deneb 의 메인 오픈웨이트 모델(dsv4)과 정확히 들어맞고, 캐시-세이프 주입 기구(`run_tail_inject.go`)도 이미 있어 **수용 비용이 낮다**. 단 큰 이득은 *긴 자율 궤적*(research/cron/agentic search)에서만 나오므로, **인터랙티브 비서 턴이 아니라 자율·딥리서치 경로에 한정한 실험으로 채택 권장**.

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
| **rubric-게이팅된 모델-주도 압축** (자율·딥리서치 경로) | 🟢 **실험 채택 권장** | 우리가 곧 비판받는 baseline + 오픈웨이트 발견이 dsv4 에 직격 + 주입 기구 기존 |
| 동일 기법을 **인터랙티브 비서 턴**에 | 🟡 **스킵/보류** | 짧은 궤적이라 이득 미미 + 5분 데드라인에 mid-turn LLM 호출 추가 위험 |
| cheap pruning(Micro/Tier2b) 대체 | ⛔ 무관 | SelfCompact 는 *트리거 의사결정*을 바꾸지 fence-strip 단계를 안 건드림 — 보완재 |
| bounded digestion(P7) 대체 | ⛔ 무관 | 직교 — SelfCompact 채택 시 그 위에 얹음 |

---

## 5. 실험으로 가져갈 형태 (제안, 미구현)

논문의 전면 도입이 아니라 **트리거 의사결정만** 우리 tier 체인 앞에 끼우는 최소 단위:

1. **rubric-probe (tail-only)**: 자율 턴에서 토큰이 LLM 임계의 일정 밴드(예: 60–90%)에 들면, `injectTailAdditions` 로 마지막 메시지에 한국어 rubric 한 단락을 wire-only 주입 — "지금 sub-task 가 끝났거나 수렴 중이면 `compact` 도구를 호출하고, derivation 중이거나 막혔으면 호출하지 마라". transcript 에는 안 남겨 다음 턴 prefix 보존.
2. **`compact` 도구**: 호출 시 기존 `polaris.Compact` 의 LLM tier 를 *모델이 지명한 시점에* 한 번 돌리는 얇은 래퍼. 도구 결과 = "압축 완료, N→M 토큰". 자율 세션에만 노출(인터랙티브 tool set 불변 → 캐시·Rule B 보존).
3. **90% 하드 트리거는 유지**(safety net). 모델이 안 부르면 기존 임계가 잡는다 — 논문의 fixed-interval 도 baseline 회복용으로 남겨둔 것과 동형.
4. **검증**: `iterate.sh --metric` 으로 자율 딥리서치 시나리오의 토큰/품질을, prefix_cache 메트릭으로 캐시 회귀를 측정(prompt-cache 라이브 검증 절차). 인터랙티브 quality 스위트는 불변이어야(스코프 격리 증명).

> 이건 **제안서**다 — 실제 채택은 자율 경로 e2e 측정으로 +점수·토큰절감이 재현되고 인터랙티브 캐시/레이턴시 회귀가 0임을 확인한 뒤. 단일 사용자·로컬 GPU 환경에서 mid-turn LLM 호출 1회의 레이턴시가 토큰 절감 대비 값을 하는지가 keep/revert 기준.

---

## 6. 관련 문서

- 압축 정책 단일 진실원: `.claude/rules/prompt-cache.md` §5(컨텍스트 압축), §1.5(vLLM APC 꼬리 주입)
- 구현: `gateway-go/internal/pipeline/compaction/` (`polaris.go` 트리거, `llm.go` P7 bounded digestion, `micro.go`/`restore.go` cheap pruning)
- 캐시-세이프 주입: `gateway-go/internal/pipeline/chat/run_tail_inject.go`
- 모델 역할(압축=lightweight 로컬): `.claude/rules/model-roles.md` (컴팩션 청크 요약 → lightweight)
- 선행 검토 포맷: `docs/research/code-as-agent-harness-review.md`
