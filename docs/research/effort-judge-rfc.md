---
title: "Effort Judge RFC"
summary: "Background local-LLM judge scoring the heuristic effort router to start a grounded label flywheel."
read_when:
  - "Designing or reviewing the effort-router accuracy-logging and shadow-label pipeline"
  - "Deciding whether an LLM judge can substitute for Ares verified labels on a personal assistant"
  - "Planning the path from the heuristic effort router to a learned shadow router"
sidebarTitle: "Effort Judge RFC"
---

# Effort Judge RFC

> Status: proposal. No code yet. This RFC fixes the schema, the judge prompts, and the calibration protocol BEFORE building, because the whole idea lives or dies on whether the judge measures the right thing.

## 동기

Deneb 의 effort 라우터(`gateway-go/internal/pipeline/chat/effort_router.go`)는 무학습 휴리스틱이다. 매 턴 thinking on/off 를 결정하고 그 결정을 agentlog 에 `effortDecision` 으로 남기지만, **그 결정이 옳았는지에 대한 ground-truth 신호가 없다.** 우리는 지금 다음을 모른다:

- 휴리스틱이 시간이 지나며 드리프트하는가 (모델 교체·트래픽 변화 후)?
- false-easy 비율(추론이 필요했는데 껐다 = 비싼 에러)은 얼마인가?
- false-hard 비율(불필요하게 켰다 = 회수 가능한 토큰)은 얼마인가?
- 어느 휴리스틱 분기(hard-signal / context-heavy / length)가 가장 자주 틀리는가?

Ares 논문의 검증 라벨 파이프라인은 개인 비서에 **이식 불가**다 — `docs/research/agent-papers-2026-deep-dive.md` 의 "정정 2" 참조: Ares 라벨은 검증가능 task 성공 + effort별 K-리플레이 + 행동등가 판정을 전제하는데, 개인 비서는 셋 다 없다(성공이 퍼지, 자연어 최종액션, task pool 부재).

이 RFC 는 그 이식 가능한 대체물을 제안한다: **로컬 LLM(lightweight 역할, qwen3.6-35b-a3b)을 백그라운드 감사자로 돌려 휴리스틱 결정의 정확도를 계속 로깅**한다. 라이브 결정은 절대 안 바꾸고, 관측만 한다. 누적된 라벨은 (1) 드리프트 모니터, (2) 향후 학습형 섀도 라우터(deep-dive 5B)의 훈련 코퍼스가 된다.

## 핵심 원칙: 입력이 아니라 결과를 판정한다

이 설계의 성패를 가르는 단 하나의 결정이다.

순진한 구현은 judge 에게 **입력을 다시 분류**시킨다 — "이 메시지, 추론이 필요했어?". 그건 ground truth 가 아니라 **effort-bias 걸린 LLM 의 선험적 의견**이다. LLM judge 는 "추론을 더 했으면 일반적으로 더 좋다"고 체계적으로 답해, 멀쩡한 non-thinking 답을 "부정확했다"고 깎는다. 이건 Ares 라벨이 신뢰받는 이유(검증된 결과)와 정반대다.

신뢰할 수 있는 버전은 **결과를 본다**, 두 단계로:

- **Tier A (결과 인지·연속):** judge 가 (메시지 + 휴리스틱이 실제로 생성한 답 + 결정)을 보고, "생성된 답에 추론 부재로 인한 **구체적** 결함이 있나"를 판정. 입력-재분류보다 낫지만 여전히 한 답만 본다 → noisy.
- **Tier B (반사실·gold):** 그 턴을 **반대 모드로 한 번 더 실행**해 두 답을 head-to-head 비교 — thinking-off 가 **실제로 뭘 잃었나**. 이게 puppet-seat 리플레이(deep-dive line 112) = Ares K-샘플링의 수동 축소판이다. 의견이 아니라 grounded 측정.

### 우리에게 Tier B 가 (상대적으로) 싼 이유

thinking 토글은 **KV-prefix-safe** 다 (`effort_router.go`: 템플릿 플래그는 생성 tail 만 바꿈, prefill 캐시 생존). 반대 모드 재실행은 비싼 prefill 을 **캐시 재진입**하고 tail 만 새로 생성한다. 한 번의 추가 생성으로 router 가 튜닝된 바로 그 비대칭을 grounded 로 측정한다:

- routed-OFF 턴 → thinking-ON 재실행, 더 나으면 = **잡힌 false-easy (비싼 에러)**
- kept-ON 턴 → thinking-OFF 재실행, 동등하면 = **false-hard (싼 에러, 회수 토큰)**

단 재실행은 **메인 모델(dsv4) 생성**이라 라이브 유저 트래픽과 GPU(unified memory)를 경합한다 → Tier B 는 **idle-gated + 매우 희소** 샘플만.

## 아키텍처

`gateway-go/internal/pipeline/chat/hindsight_recorder.go` 의 async retain 패턴과 동형이다: 턴 완료 → fire-and-forget 백그라운드 작업 → 로그. 라이브 응답 경로·레이턴시·APC 에 손대지 않는다.

```text
turn 완료 (effortDecision 확정·답 생성됨)
        │  (라이브 결정은 이미 적용됨 — judge 는 절대 못 바꿈)
        ▼
  sample gate ──no──▶ 드롭 (대부분의 턴)
        │ yes
        ▼
  Tier A: lightweight judge(qwen3.6) — 답을 결과-인지 판정 → effort-judge.jsonl
        │
        ▼  (Tier A 샘플 중 더 작은 부분 + idle 일 때만)
  Tier B: dsv4 반대모드 재실행 → blind head-to-head 비교 → delta 기록
```

- **모델:** Tier A·비교 judge = lightweight 역할(`gateway-go/internal/pipeline/pilot/localai.go` 경로, gmailpoll/genesis/pilot 와 공유). Tier B 재실행 = 메인 역할(dsv4).
- **동시성 바운드:** 공유 lightweight 모델을 굶기지 않게 judge 는 foreground 백그라운드 작업에 양보(낮은 우선순위 큐 + 단일 동시 실행).
- **kill switch:** `DENEB_EFFORT_JUDGE=off|tierA|tierA+B` env. 미설정이면 통째로 휴면(hindsight 의 `DENEB_HINDSIGHT_URL` dormant 패턴과 동일).

## 로그 스키마

`effort-judge.jsonl` (append-only, home 기반 — dev/prod 공유 주의, transcripts 와 동일). 한 줄 = 한 판정. **15 구조화 피처가 향후 로지스틱 회귀의 입력**이다.

```json5
{
  ts: "2026-06-15T01:30:00+09:00",
  run_id: "...",
  session_kind: "interactive",   // interactive | cron | heartbeat | ...
  model: "deepseek-v4-flash",
  // ── 휴리스틱이 본 것 + 결정한 것 (= 미래 학습 피처) ──
  features: {
    turn: 0,
    msg_runes: 42,
    newlines: 0,
    has_code_fence: false,
    has_attachments: false,
    is_automation: false,
    hard_signal: "",            // "" 또는 매칭된 신호 (어느 분기가 발화했나)
    ot_batch_max_runes: 0,      // per-step 관측 부하 (o_t, #2274 ToolActivity)
    ot_total_runes: 0,
    ot_any_error: false,
    history_heavy: false,       // recentContextHeavy(h_t)
  },
  decision: { thinking_off: true, reason: "short-conversational" },
  // ── Tier A: 생성된 답에 대한 결과-인지 의견 (noisy) ──
  judge_a: {
    verdict: "agree",           // agree | too-aggressive | too-conservative
    confidence: 0.8,
    cited_flaw: "",             // too-aggressive 일 때 인용된 구체 결함, 아니면 ""
  },
  // ── Tier B: 반사실 리플레이 (샘플·idle 일 때만, grounded) ──
  judge_b: {
    ran: false,
    counterfactual_mode: "",    // "thinking-on" | "thinking-off"
    winner: "",                 // "live" | "counterfactual" | "tie"
    concrete_difference: "",    // 인용된 실질 차이, tie 면 ""
    extra_tokens: 0,            // 리플레이 비용 가시화
  },
}
```

## Judge 프롬프트 설계

핵심은 **편향 제거 프레이밍**이다. 둘 다 강제 JSON(StructuredOutput) 출력.

**Tier A (결과-인지, anti-bias):**

```text
역할: 너는 effort 라우터의 사후 감사자다. 한 대화 턴과, 그 턴이 실제 생성한 답을 본다.
판정 대상은 "추론을 켰어야 했나"가 아니라 "생성된 답에 추론 부재로 인한 구체적 결함이 있나"다.
- 답이 요청을 정확히 충족 → agree
- 논리 오류·계산 실수·빠진 단계·근거 없는 단정 등 추론으로 고쳐질 결함을 "인용 가능" → too-aggressive
- 명백히 단순한 질문에 불필요한 과잉 분석 → too-conservative
★편향 금지: "추론을 더 하면 보통 낫다"는 추정으로 판정하지 마라.
   구체 결함을 인용하지 못하면 무조건 agree.
출력: {verdict, confidence, cited_flaw}
```

**Tier B (blind head-to-head):**

```text
역할: 같은 턴의 두 답을 비교한다. 어느 쪽이 thinking on/off 인지 모른 채 (블라인드).
질문: 한 답이 다른 답 대비 사용자에게 의미 있는 것을 잃었거나 얻었나?
- 인용 가능한 실질 차이(틀린 답·빠진 정보·잘못된 단계)가 있을 때만 우열
- 표현·길이·문체 차이뿐이면 tie
★기본값 tie. 인용 가능한 실질 차이 없으면 tie.
출력: {winner: A|B|tie, concrete_difference}
```

블라인드 비교가 "thinking 이 당연히 낫다" 사전확률을 제거하는 핵심 장치다. winner 를 라벨에 적을 때 게이트웨이가 A/B ↔ live/counterfactual 매핑을 복원한다.

## Judge 캘리브레이션

> 캘리브레이션 없이 나온 "정확도" 숫자는 그 자체로 편향된 계측기다.

집계 수치를 신뢰하기 전에 **judge 자체를 소규모 human-checked 셋으로 검증**한다:

1. puppet-seat 로 curated 30~50 턴을 thinking on/off 둘 다 리플레이 → 운영자가 "off 가 실제로 결함을 냈나"를 손으로 라벨.
2. 같은 셋에 Tier B judge 를 돌려 judge-vs-human **precision/recall** 측정.
3. judge 가 human 과 합의율 임계 미만이면 → 프롬프트 재설계, 집계 수치 사용 보류.

이 캘리브레이션 셋은 회귀 픽스처로 보관(모델·프롬프트 변경 시 judge 재검증).

## 샘플링·비용·가드레일

- **Tier A 샘플레이트:** config (`DENEB_EFFORT_JUDGE_RATE`, 기본 보수적 — 예 0.2). lightweight 모델은 공유 자원.
- **Tier B:** Tier A 샘플의 더 작은 부분 + **idle-gate**(라이브 트래픽 없을 때만 — `autonomous` idle 신호 재사용). dsv4 GPU 경합이라 희소 필수.
- **불변식:** judge 는 `effortDecision` 도 라이브 답도 **절대 변경 안 함**. 순수 append-only 관측.
- **동시성:** 단일 동시 judge + foreground 양보. 패닉 복구(`pkg/safego`), ctx 종료 경로(서버 shutdown 연동).
- **자동화 턴 제외 검토:** cron/heartbeat 는 thinking 항상 유지(automation 분기)라 판정 가치 낮음 → 초기엔 interactive 만 샘플.

## 측정 지표와 학습형 라우터로 가는 길

누적 로그에서 뽑는 것:

- **합의율 추이** (Tier A agree 비율) — 드리프트 알람.
- **추정 false-easy 율** (Tier B winner=counterfactual on routed-OFF) — 가장 중요한 비싼 에러.
- **추정 false-hard 율 + 회수 토큰** (Tier B tie on kept-ON) — 절감 여력.
- **reason-tag 별 정확도** — 어느 휴리스틱 분기가 가장 자주 틀리나 (다음 튜닝 타깃).

**학습형으로의 게이트 (deep-dive 5B):** grounded(Tier B) 라벨이 충분히 쌓이고 judge 가 캘리브레이션을 통과한 후에만, 15 피처 → **로지스틱 회귀** 섀도 라우터를 병행 실행(결정만 로그, 적용은 휴리스틱) → 주간 불일치 리뷰 → 합의율·예상 절감 확인 후 컷오버. **명시적 비목표:** 단일 머신 상주 1.7B 라우터(unified memory·latency 경합, deep-dive 에서 비채택).

## 단계와 미해결 질문

| Phase | 내용 | 게이트 |
|---|---|---|
| 0 | 이 RFC | — |
| 1 | Tier A 로깅 + 스키마 + kill switch + 샘플 게이트 | judge 프롬프트 리뷰 |
| 2 | judge 캘리브레이션 셋 + precision/recall | 합의율 임계 |
| 3 | Tier B idle 리플레이 + 반사실 델타 | dsv4 경합 측정 무영향 |
| 4 | 섀도 로지스틱 회귀 (deep-dive 5B) | grounded 라벨 충분 + judge 캘리브 통과 |

**미해결:**

- **잔여 judge 편향:** Tier B 블라인드 비교도 judge 자체 한계 내. 캘리브레이션이 유일한 방어 — 그 정확도의 천장은?
- **샘플 대표성:** 보수적 샘플레이트가 드문 실패 모드(롱테일)를 놓치지 않나 — reason-tag 별 stratified 샘플 필요할 수도.
- **라벨 드리프트:** 메인 모델 교체 시 과거 라벨 무효화 범위. model 필드로 분리 집계.
- **공유 모델 비용:** lightweight 가 gmailpoll/genesis/pilot 와 경합 — judge 가 그 작업을 지연시키지 않는지 라이브 관측 필요.

관련: `docs/research/agent-papers-2026-deep-dive.md` (정정 2, 5B 섀도 모드), `gateway-go/internal/pipeline/chat/effort_router.go`, `gateway-go/internal/ai/router/router.go`.
