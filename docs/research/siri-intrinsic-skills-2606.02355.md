# SIRI (Self-Internalizing RL with Intrinsic Skills) × Deneb 검토

> **출처**: Zhongyu He, Yuanfan Li, Fei Huang 외, *"SIRI: Self-Internalizing Reinforcement Learning with Intrinsic Skills for LLM Agent Training"* ([arXiv:2606.02355v1](https://arxiv.org/abs/2606.02355v1)). arxiv 직접 fetch 는 조직 egress 정책(403)으로 차단 → 공개 요약([HF/arXiv listing](https://arxiv.org/abs/2606.02355), [ResearchGate](https://www.researchgate.net/publication/405685374)) 기반 검토.
> **방법**: 논문 핵심 추출 → Deneb 스킬 시스템(인덱스 `prompt/`, 진화 `domain/skills/genesis/`)과 대조 → 채택/스킵 판정.
> **일시**: 2026-06-27
> **한 줄 결론**: SIRI 의 코어는 **RL 로 스킬을 *정책 가중치에 내재화*하는 학습 기법**이다. Deneb 는 **파인튜닝/RL 을 하지 않는** 단일 사용자 기성-모델 배포라 그 코어는 **범위 밖**. 다만 SIRI 가 RL credit 으로 푸는 두 하위 문제 — (1) "스킬이 *실제로* 도움 되는지 paired 검증", (2) "naive 정책에서 스킬 캐면 노이즈(-10.2%)" — 는 Deneb 의 genesis 진화 루프가 **이미 inference-time 으로 근사**한다(`validation_executor.go` original↔candidate 행동 델타 게이트 + usage-quality/daily-cap 게이팅). 그리고 SIRI 가 internalization 의 동기로 드는 "inference-time 스킬뱅크가 컨텍스트·레이턴시를 늘린다"는 비판에 Deneb 는 **가중치 내재화가 아니라 compact 스킬-인덱스+지연로드**로 답한다. 순수 신규 채택거리는 **없음**, 검증할 가치 있는 점검 1건(warmup 게이트 엄격성).

---

## 1. 논문 핵심

**문제**: 롱-호라이즌 LLM 에이전트는 재사용 스킬로 이득을 보지만, 기존 스킬 기법은 학습 중 **외부 스킬 생성기**나 추론 시 **상시 스킬 검색(skill bank)**에 의존 → 엔지니어링 복잡도·컨텍스트 길이·배포 레이턴시가 늘어난다.

**SIRI** = 외부 생성기·추론시 스킬뱅크 **없이** 에이전트가 스킬을 *발견·검증·내재화*하는 3단계 RL 프레임워크. 중심 명제: **RL credit 은 행동만 최적화하는 게 아니라 *어떤 스킬이 유용하고 어떤 스킬-유도 행동을 정책에 흡수할지*도 결정해야 한다.**

| Phase | 무엇 |
|---|---|
| **0. Policy Warmup** | GiGPO 로 정책을 워밍업해 기본 상호작용 능력 + 성공한 *skill-free* 궤적을 모은다. (스킵 시 **-10.2%** — naive 정책에서 캔 스킬은 스킬 저장소에 노이즈를 주입) |
| **1. Self-Skill Mining & Utilization** | 현재 정책이 *자기 자신의* 성공 plain 롤아웃에서 compact 스킬을 요약 → **paired 롤아웃(skill-augmented vs skill-free)**으로 유용성 검증 |
| **2. Skill Internalization & Distillation** | trajectory-level utility + action-level advantage 로 **유익한 스킬-유도 action token 만** plain 정책에 distill (= 가중치 내재화). 이후 추론엔 스킬뱅크 불필요 |

**결과** (Qwen2.5-7B-Instruct): ALFWorld 0.908→0.930, WebShop 0.728→0.813. prompt/RL/memory 베이스라인 상회. self-mining 이 **클로즈드 대형모델 distillation 에 필적**. ablation: warmup 제거 -10.2%; Phase-1 체크포인트를 스킬 없이 평가하면 0.719 로 급락, 스킬 검색만 켜면(내재화 전) 0.805 → 채굴된 스킬뱅크의 품질을 입증.

---

## 2. Deneb 현황 대조

Deneb 의 스킬 시스템은 **두 층**이다 — 추론시 *인덱스*와 백그라운드 *진화*:

| SIRI 요소 | Deneb 대응 | 위치 | 판정 |
|---|---|---|---|
| RL 로 스킬을 **정책 가중치에 distill**(Phase 2) | **없음** — Deneb 는 기성 로컬/클라우드 모델을 *호출만* 한다. 파인튜닝·RL·가중치 갱신 0 | — | ⛔ **범위 밖** |
| 추론시 **상시 스킬뱅크가 컨텍스트를 늘린다**는 비판 | **compact 스킬-인덱스**(name+description+location, ~5–7K tok) + 본문 on-demand(`skills(read)`/`read`) | `.claude/rules/prompt-cache.md` Semi-static, `prompt/system_prompt.go` | ✅ **다르게 해결** (내재화 대신 인덱스+지연로드) |
| **paired 롤아웃 검증**(skill vs skill-free 유용성)(Phase 1) | **original↔candidate 행동 델타 게이트** — 같은 입력을 원본/후보 SKILL.md 로 시뮬레이션해 도구호출 plan 회귀 채점 | `domain/skills/genesis/validation_executor.go:57-61` (`runReplayExecutorWith`), `validation_replay.go`, `DENEB_SKILL_EVOLVE_REPLAY` | ✅ **이미 구현(개념적)** |
| **self-skill mining**(자기 성공 롤아웃에서 스킬 요약)(Phase 1) | genesis Evolver 가 에이전트 *자기 행동*에서 SKILL.md 패치 채굴 → nudger fork 가 propose 결정 | `genesis/evolver.go`, `nudger.go`, `tracker_opportunities.go` | ✅ **이미 구현(개념적)** |
| **Phase-0 warmup**(naive 정책에서 캐지 마라, -10.2%) | usage-quality·validation-case 게이팅 + daily cap + liveness — 검증된 품질 사용에서만 채굴, 콜드스타트 폭주 방지 | `genesis/tracker_usage_quality.go`, `tracker_validation_cases.go`, `genesis_dailycap_test.go`, `liveness_test.go` | 🟡 **부분/점검 가치** |

**핵심 관찰**: SIRI 가 RL gradient 로 푸는 "스킬 유용성 판정"과 "naive 채굴 노이즈 방지"를, Deneb 는 **학습 없이** — LLM-기반 행동 시뮬레이션(replay 델타)과 휴리스틱 게이팅으로 — 이미 근사하고 있다. `validation_executor` 의 닥스트링이 정확히 SIRI 의 paired 명제다: *"executor 는 정답을 못 보고 스킬 텍스트만으로 plan 을 도출해야 하며, 그것이 original-vs-candidate 델타를 echo 가 아닌 진짜 행동 신호로 만든다."*

---

## 3. 적용성 — 왜 코어가 안 맞나, 무엇만 남나

**코어(가중치 내재화)가 범위 밖인 이유 (구조적)**

- **Deneb 는 학습 파이프라인이 없다.** 단일 사용자·단일 머신(DGX Spark)에서 기성 모델(dsv4/glm 등)을 *배포·호출*한다. RL rollout·GiGPO·gradient·distillation 은 모델 소유자(모델 제공자/SparkFleet)의 영역이지 게이트웨이의 영역이 아니다. SIRI 를 "도입"하려면 Deneb 가 본 적 없는 **RL 훈련 루프 + 라벨된 환경(ALFWorld/WebShop 류 success oracle)**을 세워야 하는데, 이는 제품 범위(비서/분석)와 무관하고 단일 사용자에 과잉이다(`code-as-agent-harness-review.md` 의 "학습형 RL 거버넌스 🟡 스킵 — 단일 사용자엔 과잉"과 동형).
- **내재화의 *목적*은 Deneb 에선 이미 충족.** SIRI 가 가중치 distill 로 없애려는 건 "추론시 스킬뱅크의 컨텍스트·레이턴시 비용"이다. Deneb 는 그 비용을 compact 인덱스(본문 미적재)+APC Semi-static 슬롯으로 이미 묶었다 → 내재화의 ROI 가 애초에 작다.

**남는 점검 1건 (actionable)**

- SIRI 의 가장 이전 가능한 *정량* 신호는 **"naive 단계에서 스킬 채굴 = -10.2% 노이즈"**다. Deneb genesis 가 *언제* 채굴/진화를 발화하는지(저품질·콜드스타트 행동에서 패치를 만들지 않는지)를 이 렌즈로 한 번 점검할 가치가 있다. 현재 `tracker_usage_quality`/`validation_cases`/daily-cap 가 그 게이트로 보이지만, **"성공/검증된 사용에서만 채굴" 불변식이 명시적으로 강제되는지**는 확인거리다. 강제가 약하면 warmup 게이트를 조이는 것이 SIRI 가 주는 유일한 직접 교훈.

---

## 4. 판정

| 항목 | 판정 | 근거 |
|---|---|---|
| RL 스킬 내재화(distillation) — SIRI 코어 | ⛔ **범위 밖** | Deneb 무학습 단일사용자 배포. 훈련루프·success-oracle 환경 부재 + 제품범위 무관 |
| 추론 스킬뱅크 컨텍스트 비용 (SIRI 의 internalization 동기) | ✅ **이미 다르게 해결** | compact 인덱스+지연로드(prompt-cache Semi-static) — 내재화 불필요 |
| paired 유용성 검증 (Phase 1) | ✅ **이미 구현(개념)** | `validation_executor.go` original↔candidate 행동 델타 replay |
| self-skill mining (Phase 1) | ✅ **이미 구현(개념)** | genesis Evolver/nudger/tracker 자기행동 채굴→propose |
| warmup-before-mining (Phase 0, -10.2%) | 🟡 **점검 가치** | usage-quality/daily-cap 게이트 존재 — "검증된 사용에서만 채굴" 불변식 엄격성 확인 |
| 신규 코드 채택 | ⛔ **없음** | 코어 범위밖 + 주변부 이미 존재. 새 메커니즘 추가 불필요 |

**한 줄**: SIRI 는 *학습으로* 푸는 문제를 Deneb 는 *무학습 인덱스+행동검증*으로 이미 우회한다. 채택거리는 없고, genesis warmup 게이트의 엄격성 점검만 남는다. (인접 EMPG `arXiv:2509.09265` 등 entropy-modulated credit assignment 도 같은 이유로 범위 밖 — 전부 훈련 시 gradient 기법.)

---

## 5. 관련 문서

- 스킬 인덱스/캐시: `.claude/rules/prompt-cache.md` (Semi-static = compact skill-index), `gateway-go/internal/pipeline/chat/prompt/system_prompt.go`
- 스킬 진화(genesis): `gateway-go/internal/domain/skills/genesis/` (`evolver.go` 채굴, `validation_executor.go`/`validation_replay.go` paired 검증, `nudger.go` propose, `tracker_*.go` 게이팅)
- 모델 역할(진화=coding, replay=lightweight): `.claude/rules/model-roles.md` (스킬 진화 패치 생성·behavioral replay 행)
- 선행 "범위 밖 RL 거버넌스" 판정: `docs/research/code-as-agent-harness-review.md`
- genesis 자체 리서치 노트: `gateway-go/internal/domain/skills/genesis/research/` (apex-self-evolution, self-harness 등)
- 선행 검토 포맷: `docs/research/self-compacting-agents-2606.23525.md`
