# SkillHone (Persistent Decision History 스킬 진화 하네스) × Deneb 검토

> **출처**: Zhiwei Li, Yong Hu, *"SkillHone: A Harness for Continual Agent Skill Evolution Through Persistent Decision History"* ([arXiv:2606.08671](https://arxiv.org/abs/2606.08671), [HF papers](https://huggingface.co/papers/2606.08671)). arxiv HTML 직접 fetch 성공 — 본문 기반 검토.
> **방법**: 논문 핵심 추출 → Deneb genesis 진화 루프(`gateway-go/internal/domain/skills/genesis/`)와 대조 → 채택/스킵 판정. 코드 대조는 탐색 에이전트 + 핵심 경로 직접 재확인.
> **일시**: 2026-07-11
> **한 줄 결론**: SkillHone 의 코어 — **결정 히스토리 지속화** h_t=(진단, 수정, 증거, 결과)를 남기고 차기 최적화 세션이 이를 조회해 중복/재실패 편집을 회피 — 는 Deneb genesis 가 **이미 세 겹으로 구현**하고 있다(lifecycle log+`HarnessEditAudit` · rejected-edit 버퍼 · optimizer memory → 전부 다음 evolve 프롬프트에 재주입). 논문의 ablation(히스토리 제거 -13.4pt, 역할분리 제거의 ~2배)은 이 기존 투자의 정량 뒷받침이다. 남는 실질 검토거리는 **redaction 비대칭 1건**: SkillHone 은 최적화자가 probe 정답·validator 를 *못 보게* 가리는데, Deneb 는 producer 프롬프트에 "held-out" 케이스의 oracle(요구 substring·tool-call)을 최신 5건까지 그대로 노출한다 — 그 5건에 한해 선택 게이트가 game 가능. 케이스 코퍼스가 자랄 P3 진입 시 가시/블라인드 분할을 함께 설계할 것. 신규 대규모 채택은 없음.

---

## 1. 논문 핵심

**문제**: 기존 스킬 개선 기법은 bounded run 안에서 스킬을 최적화하고 **최종 아티팩트만 남긴다** — 왜 그 수정이 채택/기각됐는지의 결정 맥락이 버려져, 차기 개선 세션이 같은 진단을 재발견하고 이미 실패한 편집을 반복한다.

**SkillHone** = 스킬을 "고립된 아티팩트"가 아니라 **감사 가능한 개발 히스토리를 가진 진화 시스템**으로 유지하는 하네스. 세 요소:

1. **결정 레코드**: 매 이터레이션 t 마다 h_t = (q_t 진단, r_t 수정 제안, e_t redacted 평가 증거, o_t 결과 accept/revise/reject/defer)를 축적. 차기 최적화 에이전트는 히스토리를 조회해 "이 실패가 새 것인지, 비슷한 수정이 이미 시도됐는지, 왜 기각됐는지"를 판단. 구현은 GitHub-스타일 워크플로(이슈=진단, PR=수정안, 머지/기각=결과) — 저장 형식보다 **감사 가능한 원장**이라는 성질이 본질.
2. **역할 분리 + redaction**: 최적화 서브에이전트는 스킬 레포+과거 결정만 보고 **unredacted probe 정답·validator 를 못 본다**; 평가 서브에이전트는 probe 를 실행하되 **스킬을 수정할 수 없고** redacted 문제 리포트(실패 모드·집계 결과·진단 가설만, 정답 제외)만 돌려준다; dispatcher 는 결정권 없는 메시지 라우터. 프롬프트 지시가 아니라 **권한 경계**로 강제.
3. **스킬-평가 레포 분리**: practice probe·oracle 정답·validator·트레이스를 스킬 레포와 **별도 레포**에 격리 — 평가 자산이 직접 암기(memorization) 대상이 되는 것을 방지.

**결과**: GAIA(open-web) 64.6% (상용 deep-research 대비 +15.8pt), WebWalkerQA-EN 66.4% (+3.2pt). 내부 도구-매개 시나리오 7종 평균 +18.8pt. practice probe **20개**만으로 5 이터레이션 만에 practice 30%→70%. Qwen3.6-35B-A3B 로 개발한 스킬이 Claude Sonnet 4.6 에 무수정 이전되어 GAIA 72.4% — 이득이 모델 피팅이 아닌 **절차 개선**임을 시사. 개발 컨트롤러는 Claude Opus 4.6, 실행 백본은 Qwen3.6-35B-A3B (개발/실행 모델 분리).

**Ablation (핵심 정량)**: 히스토리 지속화 제거 **-13.4/-10.9pt** (GAIA/WebWalkerQA-EN), 역할분리 제거 -6.4/-5.3pt — **결정 히스토리의 기여가 역할분리의 약 2배**. 회귀를 유발한 수정에서 "문제 부분만 타깃하고 유용한 편집은 보존"하는 부분-롤백형 이터레이션이 scalar 신호로 후보 전체를 수락/기각하는 베이스라인 대비 우위.

**한계 (저자 명시+암묵)**: 단일 스킬 고립 진화만 다룸(스킬 간 상호의존 미해결); 히스토리 비대화·검색 효율 저하 미분석; 비용/토큰 오버헤드 미보고; 내부 시나리오 1종은 +0.0pt 인데 원인 미조사; 과거 결정 사슬을 무효화하는 환경 변화(API deprecation 등) 처리 부재.

---

## 2. Deneb 현황 대조

Deneb genesis 는 SkillHone 의 (1) 결정 히스토리를 이미 갖추고 있고, (2) 역할분리는 절반(모델 분리는 있으나 redaction 없음), (3) 평가자산 격리는 파일 분리만 있다:

| SkillHone 요소 | Deneb 대응 | 위치 | 판정 |
|---|---|---|---|
| **h_t 결정 레코드** (진단·수정·증거·결과) 축적 | lifecycle log 의 typed 엔트리(`evolved`/`evolve_rejected`/`evolve_rolled_back`/`evolve_confirmed`/`cross_skill_regression`) + `HarnessEditAudit`(TargetSignature=진단, EditedSurface, ExpectedBehaviorChange, RegressionRisk) — 구조화된 "왜" | `genesis/tracker_lifecycle.go`, `evolver.go` → `skill_genesis_log.jsonl` | ✅ **이미 구현** |
| **기각안도 보존** ("왜 기각됐나" 조회 가능) | rejected-edit 버퍼: Reason + 후보 본문 발췌 + 기각한 게이트(Source) + audit. 주석이 명제 그대로: *"A failed candidate is not just discarded; the next optimizer pass can read why it failed and avoid repeating the same mutation"* | `genesis/tracker_rejected_edits.go` → `skill_rejected_edits.jsonl` (+lifecycle 폴백 재구성) | ✅ **이미 구현** |
| **히스토리 조회로 중복/재실패 편집 회피** | evolve 착수 시 `RecentRejectedSkillEdits`+`OptimizerMemory`(Stable/AvoidDirections)+저수율 lever(반복 ship 됐지만 confirm 안 된 (signature×surface) 전략)를 producer 프롬프트에 재주입 — "같은 방향 반복 금지, 반려 사유 우회하는 더 작은 패치만". 추가로 thrash 쿨다운·recency 게이트가 LLM 호출 전 중복 작업 자체를 차단 | `genesis/evolver.go` (히스토리 로드→프롬프트 조립, `evolutionSuppressed`), `evolver_prompt_format.go`, `tracker_optimizer_memory.go` | ✅ **이미 구현** |
| **부분-롤백형 이터레이션** (문제 부분만 타깃, 유용 편집 보존) | K-후보 생성+judge 선택, post-evolve 롤백 워치(`evolve_confirmed`/`evolve_rolled_back`), 롤백 사유가 다음 라운드 AvoidDirections 로 환류 | `genesis/evolver_judge_teacher.go`, `tracker_lifecycle.go` | ✅ **개념적 등가** |
| **GitHub-스타일 이슈/PR 원장** | JSONL append-only 로그 + `/health` `EvolutionHealthSummary` — 형식은 다르나 "감사 가능한 원장" 성질 동일 | `genesis/tracker.go` (데이터 디렉토리의 `*.jsonl`) | ✅ **다르게 해결** |
| **역할 분리: 평가자는 스킬 수정 불가** | `SkillValidationEngine` 은 채점만 함(구조적으로 수정 경로 없음); producer≠judge 모델 강제(자기선호 편향 회피, arXiv:2508.02994 인용) + teacher/judge/replay-executor 역할별 모델 | `genesis/validation_engine.go`, `evolver_judge_teacher.go` (`pickCandidateJudge`) | ✅ **이미 구현** |
| **역할 분리: 최적화자가 probe 정답을 못 봄 (redaction)** | **없음** — producer 프롬프트가 "Held-out validation/replay cases" 섹션에 최신 5케이스의 required/forbidden substring·heading·tool-call(=oracle 전체)을 노출하고 "검증 계약이니 충족하라"고 지시. judge·teacher 도 동일 노출. 유일한 블라인드는 replay executor(*"executor 는 정답을 못 보고 스킬 텍스트만으로 plan 도출"*) | `genesis/evolver_prompt_format.go` (`formatValidationCasesForPrompt`), `evolver_skill_validation.go` (`validationCasesForPrompt`, limit 5) vs `validation_executor.go` (블라인드) | 🟡 **격차** (아래 §3) |
| **평가자산 별도 레포 격리** (암기 방지) | 파일은 분리(`skill_validation_cases.jsonl` vs 스킬 디렉토리의 SKILL.md)돼 있으나 위 행처럼 내용이 producer 에 재노출 → 물리 분리 ≠ 정보 격리 | `genesis/tracker.go`, `tracker_validation_cases.go` | 🟡 **부분** |
| **소수 practice probe 로 큰 이득** (probe 20개→+40pt) | 케이스 채굴 기계는 존재(리뷰 세션·세션 backfill·per-use 자동 캡처+약케이스 가드)하나 **프로덕션 커버리지가 사실상 0** — 코드 주석이 자인: behavioral held-out 게이트가 대부분 스킬에서 inert, "NO skill has validation cases" 경로가 상시 | `genesis/tracker_validation_cases.go`, `gateway-go/internal/runtime/server/validation_backfill_task.go`, `genesis/evolver_candidate_eval.go` | 🟡 **알려진 약점 재확인** |
| **개발 컨트롤러/실행 백본 모델 분리** + 소형→대형 스킬 이전 | producer=코딩 모델(glm 계열)·judge=main·replay=lightweight 로 이미 분리 배치 | `genesis/evolver.go`, `docs/agent-rules/model-roles.md` | ✅ **이미 구현** |
| 단일 스킬 고립 진화 (논문의 한계) | Deneb 는 **cross-skill regression sweep** 이 이미 있음 (`cross_skill_regression` lifecycle 엔트리) | `genesis/tracker_lifecycle.go`, `validation_engine.go` (이웃 채점) | ✅ **Deneb 가 앞섬** |

**핵심 관찰**: SkillHone 이 "패러다임 전환"으로 내세우는 것(아티팩트가 아니라 결정 히스토리를 유지하라)을 Deneb 는 이미 기본 설계로 갖고 있다 — `skill_genesis_log.jsonl` + rejected 버퍼 + optimizer memory 가 h_t 튜플의 분산 구현이고, 프롬프트 재주입이 히스토리 조회다. 이 논문의 가치는 새 메커니즘이 아니라 **그 투자에 대한 외부 정량 근거**(-13.4pt ablation, 역할분리의 2배)다.

---

## 3. 적용성 — 격차 1건과 우선순위 근거 1건

### 3-1. Redaction 비대칭 (실질 검토거리)

현재 구조: 선택 게이트(`ValidateCandidate`)는 **최신 20케이스**로 original vs candidate 본문을 채점하는데, producer 프롬프트에는 그중 **최신 5케이스의 oracle 전체**(required substring·heading·expected tool-call)가 "충족해야 할 검증 계약"으로 노출된다. 즉 게이트가 실제로 물어볼 채점 기준의 최신·최다-바인딩 부분을 후보 생성자가 미리 본다 — required substring 을 SKILL.md 에 그대로 심으면 그 5건은 사실상 자동 통과다. SkillHone 의 redaction 이 정확히 이걸 막는 장치다(평가 자산이 암기 대상이 되는 것).

**완화 요인 (지금 당장 사고가 아닌 이유)**:

- 노출은 의도된 설계다 — 케이스가 "보존해야 할 행동 계약"을 후보에게 알려주는 순기능(계약 명시)이 있고, 프롬프트도 "실행 지시로 취급 말라"는 주의를 단다.
- **replay executor 는 블라인드**라 행동 게이트(`EvaluateBehavior`)는 echo 로 못 뚫는다 — 스킬 텍스트가 실제 행동을 바꿔야 통과.
- post-evolve 롤백 워치가 실사용 회귀를 잡는 최종 백스톱.
- 무엇보다 프로덕션 케이스 커버리지가 ~0 이라 이 격차는 **현재 비활성**이다.

**판정**: 지금 코드를 고칠 사안은 아니고, **P3(verifier 공진화)로 케이스 코퍼스가 실제로 자라기 시작할 때 함께 설계할 항목**. 그 시점의 설계 옵션: (a) 케이스를 **가시 계약 서브셋**(producer 에 보여주는 소수)과 **블라인드 held-out 서브셋**(게이트 전용)으로 분할 — SkillHone 의 레포 분리를 파일 하나 안의 필드/샘플링 분할로 축소 구현; (b) producer 에는 oracle 대신 **redacted 실패 요약**(어떤 실패 모드가 몇 건, 어느 섹션이 문제)만 주기 — SkillHone 의 redacted report 와 동형. (a)가 기존 구조 변경이 작다. 이 항목을 RSI 로드맵 P3 의 설계 체크리스트에 얹는 것이 이 논문의 유일한 직접 액션이다.

### 3-2. Probe 코퍼스 우선순위 근거 강화

논문의 가장 실용적인 정량 신호: **practice probe 단 20개**로 5 이터레이션 만에 30%→70%. 즉 진화 루프의 병목은 정교한 최적화가 아니라 **소수라도 실재하는 평가 자산**이다. Deneb 의 자인된 약점이 정확히 이것(backfill lane 이 있어도 behavioral 게이트가 대부분 스킬에서 inert)이므로, "스킬당 케이스 5–20개 확보"가 진화 품질에 지렛대라는 논문 수치는 **backfill/캡처 레인의 커버리지 확대를 P3 이전에라도 밀 근거**가 된다. 새 메커니즘 불요 — 있는 기계의 가동률 문제.

### 3-3. RSI 로드맵과의 관계

`docs/research/recursive-self-improvement-roadmap.md` 의 앵커 3편에 대한 **인접 보강 논문**으로 읽는 것이 맞다:

- **P2 (slow loop)**: SkillHone 의 h_t 원장은 meta-artifact 수정의 fitness 판정에도 같은 구조가 필요함을 시사 — 이미 계획된 "meta-change 마다 fitness baseline+auto-revert 기록"과 동형. 추가 작업 없음.
- **P3 (verifier 공진화, CoEvoSkills)**: SkillHone 의 평가-레포 격리/redaction 은 P3 가 케이스 코퍼스를 키울 때의 **오염 방지 설계 제약**으로 편입 (§3-1).
- SkillHone 이 안 다루는 것(스킬 간 상호의존, 히스토리 비대화)은 Deneb 가 각각 cross-skill sweep 과 recent-N 윈도우(rejected 3·케이스 5/20·lever 스캔 300)로 이미 실용적으로 절단하고 있다.

---

## 4. 판정

| 항목 | 판정 | 근거 |
|---|---|---|
| 결정 히스토리 지속화 (h_t 축적+차기 조회) — SkillHone 코어 | ✅ **이미 구현** | lifecycle log+audit · rejected 버퍼 · optimizer memory · 저수율 lever → evolve 프롬프트 재주입. ablation -13.4pt 는 기존 설계의 외부 정량 근거 |
| GitHub-스타일 원장 | ✅ **다르게 해결** | JSONL append-only + `/health` 요약 — 원장 성질 동일, 형식 전환 무익 |
| 역할분리: 평가자 수정불가·producer≠judge·개발/실행 모델 분리 | ✅ **이미 구현** | `pickCandidateJudge` 자기선호 회피, 역할별 모델, 채점 전용 엔진 |
| Redaction: 최적화자에게 oracle 은닉 | 🟡 **P3 설계 체크리스트에 편입** | producer 가 게이트 채점 기준(최신 5케이스 oracle)을 봄 — 커버리지 0 이라 현재 비활성, 코퍼스 성장 시 가시/블라인드 분할 설계 |
| 평가자산 격리 저장소 | 🟡 **위와 동일 사안** | 파일 분리는 있음 — 정보 격리만 P3 에서 |
| probe 소수 확보의 지렛대 (20개→+40pt) | 🟡 **우선순위 근거 강화** | 알려진 커버리지 약점의 해소 가치를 정량 뒷받침 — 기존 backfill/캡처 레인 가동률 문제 |
| 단일-스킬 고립 (논문 한계) | ✅ **Deneb 가 앞섬** | cross-skill regression sweep 기존 |
| 신규 코드 채택 | ⛔ **지금은 없음** | 코어 기구현 + 격차는 현재 비활성. P3 착수 시 §3-1 반영 |

**한 줄**: SkillHone 은 Deneb genesis 가 이미 내린 설계 결정(결정 히스토리를 남기고 재주입하라)에 정량 근거를 주는 논문이다. 채택거리는 새 기계가 아니라 **P3 때 케이스 코퍼스의 가시/블라인드 분할 1건**과 **케이스 커버리지 확대의 우선순위 상향 근거**뿐이다.

---

## 5. 관련 문서

- RSI 로드맵 (P1–P4): `docs/research/recursive-self-improvement-roadmap.md` — 본 논문은 P3 설계 제약 + P2 원장 구조의 인접 보강
- genesis 규칙: `docs/agent-rules/self-improvement.md`
- 결정 히스토리 구현: `gateway-go/internal/domain/skills/genesis/` — `tracker_lifecycle.go`(원장), `tracker_rejected_edits.go`(기각 보존), `tracker_optimizer_memory.go`(방향 메모리), `evolver.go`(재주입), `evolver_prompt_format.go`(프롬프트 섹션)
- 검증 스택: `genesis/validation_engine.go`(선택/행동 게이트, 케이스 20), `validation_executor.go`(블라인드 실행자), `tracker_validation_cases.go`(케이스 저장/backfill), `gateway-go/internal/runtime/server/validation_backfill_task.go`
- 선행 검토 포맷: `docs/research/siri-intrinsic-skills-2606.02355.md`, `docs/research/self-compacting-agents-2606.23525.md`
- 모델 역할 배치: `docs/agent-rules/model-roles.md`
