# RSI Research 2026H1: Papers to Code Mapping

> 2026-07-11 rsi-papers-to-code 워크플로 산출 (검색 7각도 → 실재확인 118편 → 상위 14편을
> 실제 genesis 소스와 대조해 코드 매핑). 이 문서는 조사 원본 기록이며, 로드맵 반영분은
> `recursive-self-improvement-roadmap.md`가 정본이다. 문서 규칙상 예외적으로 한국어
> (조사 산출물 원문 보존; docs/research/ 내부 노트는 내비게이션 비포함).

**작성일**: 2026-07-10 · **대상**: `gateway-go/internal/domain/skills/genesis/` 자가개선 루프 (P1 메타 아티팩트 외부화 랜딩 직후) · **입력**: 실재 확인 논문 118편 + 상위 11건 코드 매핑

---

## 1. 연구 지형 요약 (2026 상반기)

**하네스 진화(harness evolution)가 지배 패러다임으로 정착했다.** 2025년 말 Darwin Gödel Machine(2505.22954)·Live-SWE-agent(2511.13646)가 연 "동결 모델 + 진화하는 스캐폴드" 노선이 2026 상반기에 명명법 자체를 장악했다 — Self-Harness(2606.09498), Meta-Harness(2603.28052), Adaptive Auto-Harness(2606.01770), SIA(2605.27276), DemoEvolve(2605.24539), EvoTrainer(2606.03108), Agentic Harness Engineering(2604.25850). 가중치 업데이트 없이 프롬프트·제어 로직·스킬·도구 배선을 진화 대상으로 삼는 이 체제는 Deneb genesis의 설계(소스코드 forbidden self-edit + SKILL.md/메타 아티팩트만 editable surface)와 정확히 같은 좌표에 있다. 즉 Deneb은 유행을 좇을 필요 없이 이미 주류 체제 위에 서 있으며, 논문들은 "무엇을 진화시키나"보다 "어떻게 안전하게 수용하나"로 초점이 이동했음을 보여준다.

**무게중심이 proposer에서 acceptor로 이동했다 — 상반기 최대 단일 교훈.** PACE(2606.08106)는 그리디 점수 기반 수용이 개선 여지가 없을 때 72–100% 가짜 커밋(false commit)을 낳음을 실증하고 anytime-valid 순차검정(sequential hypothesis testing / e-process)을 커밋 게이트로 제안했다. SEA(2607.00871)는 모든 자기수정에 감사 가능한 인증서(certificate)와 오류 예산(error budget)을 요구하고, AgentDevel(2601.04620)은 자가진화를 아예 릴리스 엔지니어링(release engineering)으로 재구성해 flip 중심 게이팅(P→F 회귀 차단)의 효과를 ablation으로 입증했다(flip 게이트 제거 시 회귀율 3.1%→14.8%). Regimes(2606.10241)도 이벤트 소싱 기반 감사 가능성을 신뢰의 전제로 놓는다. 공통 결론: **자가진화의 성패는 후보 생성 품질이 아니라 수용 메커니즘의 통계적 신뢰성이 결정한다.**

**LLM-judge 신뢰성 위기가 문헌으로 폭발했고, 응답은 generator-verifier 공진화다.** Reliability without Validity(2606.19544, 21개 judge 모델 대규모 평가), BabelJudge(2606.22329), One Token to Fool(2507.08794), Judging Against the Reference(2601.07506) 등이 프로덕션 judge의 position bias·verbosity bias·언어 편향·parametric knowledge 충돌을 체계적으로 고발했다. reward hacking 계열(2604.13602, 2604.15149, 2606.15385)과 RLVR 검증기 퍼징(2606.01066)은 "검증기 버그는 우연히 밟히는 게 아니라 최적화가 선택한다"는 위협 모델을 확립했다. 그 응답으로 CoVerRL(2603.17775, consensus trap 차단), Red Queen Gödel Machine(2606.26294, epoch 규율 하의 평가자 공진화), CoEvoSkills(2604.01687), Sci-CoE(2602.12164)가 verifier를 고정물이 아닌 공진화 대상으로 재정의했다 — Deneb 로드맵 P3의 방향을 검증하면서 그 전제조건(라벨 품질·퍼징 선행·epoch 분리)을 구체화한다.

**스킬 라이브러리 연구가 "생성"에서 "수명주기 거버넌스"로 성숙했다.** Library Drift(2605.19576), SkillOps(2605.13716), SkillBrew(2605.29440), GRASP(2605.29668), Skill Drift Is Contract Violation(2605.10990), CPE(2605.09315, 능력 침식 실증)가 무한 축적·무감시 진화의 침묵 열화(silent degradation)를 공통 진단하고 앵커 보존·다목적 큐레이션·계약 기반 유지보수를 처방했다. SkillFlow(2604.17308)·SkillEvolBench(2605.24117)·EvoAgentBench(2607.05202) 등 벤치마크도 등장했다. Deneb의 curator·staleness·thrash 브레이커는 이 흐름의 초기형이며, 부족한 것은 "획득 게이트와 분리된 보존 게이트(anchor no-regression)"다.

**마지막으로 회의론·안전 하강 기류가 뚜렷하다.** Introspection Threshold(2607.04277)·Singularity Is Not Near(2601.05280)·Generalization Gap(2606.01075)·Simple Baselines(2602.16805)·Mind the Gap(2412.02674)은 무제한 재귀 자기개선의 이론적 한계(불완전 자기접근·entropy decay·generation-verification gap)를 지적하고, Misevolution(2509.26354)·Alignment Tipping(2510.04860)·Zombie Agents(2602.15654)·OEP poisoning(2605.18930)은 자기진화 특유의 신종 공격면을 연다. 실무적 종합(2607.07663 서베이의 프레임)은 명확하다: **open-ended RSI가 아니라 bounded·auditable·statistically-gated self-refinement가 2026년의 정답**이며, 이는 Deneb의 불가침 원칙(결정적 게이트 Go 잔류·소스코드 self-edit 금지·propose-only)과 완전히 정합한다. 필요한 것은 방향 전환이 아니라 acceptor·verifier 층의 통계적 하드닝이다.

---

## 2. 논문 카탈로그 (전체 118편)

| arXiv | 제목 | 날짜 | 한줄 |
|---|---|---|---|
| 2603.23420 | Bilevel Autoresearch | 2026-03-24 | 외부 autoresearch 루프가 내부 루프의 코드·트레이스를 분석해 탐색 메커니즘 자체를 개선, ~5x 이득 |
| 2605.23019 | PACE (Two-Timescale) | 2026-05-21 | 동결 sLM이 프롬프트·제어로직을 이중 시간척도로 자율 개선, 가중치 변경 없이 +9.2% |
| 2602.05848 | DARWIN | 2026-02-05 | 다중 GPT 에이전트가 유전 선택 하에 자기 훈련 코드를 재작성 — 완만한 효율 이득 |
| 2604.23472 | Escher-Loop | 2026-04-25 | 솔버·옵티마이저 두 집단이 승패 신호로 폐루프 상호 정제, 외부 평가 제거 |
| 2605.13874 | GEAR | 2026-05-08 | 집단 기반 유전 탐색으로 연구 방향 병렬 탐색, 단일 경로 대비 국소최적 탈출 |
| 2603.28052 | Meta-Harness | 2026-03-30 | LLM 주변 코드 인프라(하네스)를 end-to-end 자동 최적화 |
| 2605.09998 | Continual Harness | 2026-05-11 | 환경 리셋 없이 온라인으로 프롬프트·전략을 자가 정제 (Pokemon 실증) |
| 2606.01770 | Adaptive Auto-Harness | 2026-06-01 | 성능 갭을 진화 손실·적응 손실로 분해, 태스크별 하네스 라우팅으로 지속 개선 |
| 2605.27276 | SIA | 2026-05-26 | 하네스와 모델 가중치를 동시 자율 개선하는 프레임워크 |
| 2605.24539 | DemoEvolve | 2026-05-23 | 희소 보상 장기 환경에서 전문가 시연으로 하네스 진화를 보완 |
| 2602.23413 | EvoX | 2026-02-26 | 후보 해와 탐색 전략을 공진화 — 고정 전략 대비 우위 |
| 2607.04277 | Self-Reference / Introspection Threshold | 2026-07-05 | 지속 RSI에는 내성(introspection)이 필요하나 현 LLM은 구조적으로 결여 |
| 2605.20086 | What Do Evolutionary Coding Agents Evolve? | 2026-05-19 | 개선 대부분이 소수 편집 유형·삭제 코드 재활용 — 진짜 신규 알고리즘은 드묾 |
| 2603.17187 | MetaClaw | 2026-03-17 | 다운타임 없는 스킬 합성·정책 최적화 연속 메타학습 |
| 2606.03108 | EvoTrainer | 2026-06-02 | LLM 정책과 훈련 하네스를 경험 피드백으로 공동 진화 |
| 2606.09498 | Self-Harness | 2026-06-08 | 실패 분석→수정→검증 사이클로 하네스가 스스로를 개선하는 패러다임 |
| 2604.25850 | Agentic Harness Engineering | 2026-04-28 | 관측성 3축 기반 폐루프 하네스 자동 엔지니어링, Terminal-Bench 69.7→77.0% |
| 2606.08106 | PACE (Anytime-Valid Acceptance) | 2026-06-06 | 순차검정 기반 training-free 커밋 게이트 — 그리디 수용의 false-accept 통제 |
| 2604.17308 | SkillFlow | 2026-04-19 | 166-task 평생 스킬 발견·패치·라이브러리 유지 벤치마크 |
| 2607.05202 | EvoAgentBench | 2026-07-06 | 자기진화를 ability graph 통한 도메인 간 능력 전이로 측정 |
| 2605.22794 | MOSS | 2026-05-21 | 텍스트 아티팩트가 아닌 소스코드 직접 재작성 자기진화 |
| 2602.10233 | ImprovEvolve | 2026-02-10 | basin-hopping + LLM 진화 서브루틴으로 수학적 구성 문제 SOTA |
| 2606.06473 | MLEvolve | 2026-06-04 | Progressive MCGS·회고 메모리·적응 코딩 모드로 ML 알고리즘 발견 |
| 2601.04620 | AgentDevel | 2026-01-08 | 자가진화 = 릴리스 엔지니어링 — 비회귀 보장·감사 가능 아티팩트 우선 |
| 2511.13646 | Live-SWE-agent | 2025-11-17 | 런타임 on-the-fly 연속 자기진화 SWE 에이전트 |
| 2509.19349 | ShinkaEvolve | 2025-09-17 | 부모 샘플링·novelty rejection·bandit LLM 앙상블의 sample-efficient 프로그램 진화 |
| 2510.14150 | CodeEvolve | 2025-10-15 | island 기반 진화 탐색 + LLM의 오픈소스 알고리즘 발견 프레임워크 |
| 2604.20714 | TPGO | 2026-04-22 | 멀티에이전트 설정을 텍스트 파라미터 그래프로 최적화 + 경험 메모리 메타학습 |
| 2601.21064 | TEP | 2026-01-28 | 국소 학습으로 compound AI 시스템의 depth-scaling 실패 해결 |
| 2511.20693 | A²Flow | 2025-11-23 | 자기적응 워크플로 추상화 연산자 자동 추출 |
| 2602.01664 | FlowSteer | 2026-02-02 | RL 에이전트가 실행 캔버스를 점진 편집해 워크플로 그래프 설계 |
| 2606.11290 | FlowBank | 2026-06-09 | 상보적 워크플로 포트폴리오 구축 + 쿼리별 최적 라우팅 |
| 2601.07477 | JudgeFlow | 2026-01-12 | 워크플로를 로직 블록으로 분해, Judge의 책임 점수로 결함 블록 표적 최적화 |
| 2601.22305 | BayesFlow | 2026-01-29 | 베이지안 추론 기반 메타에이전트 워크플로 생성 (+9%p) |
| 2605.08769 | EvoMAS | 2026-05-09 | 실행 중 태스크 상태에 맞춰 멀티에이전트 워크플로 동적 구성 |
| 2603.22791 | ABSTRAL | 2026-03-24 | MAS 아키텍처를 진화하는 자연어 문서로 자동 설계 |
| 2602.14697 | E-SPL | 2026-02-16 | 가중치는 RL, 시스템 프롬프트는 진화 알고리즘으로 공동 최적화 |
| 2601.04055 | MPO | 2026-01-07 | 구조화 프롬프트를 섹션-국소 textual gradient로 독립 최적화 |
| 2606.11459 | APEX | 2026-06-09 | 동적 데이터 선택으로 프롬프트 최적화 효율 개선 |
| 2601.21557 | Meta Context Engineering | 2026-01-29 | 문맥 최적화 스킬 자체를 agentic crossover로 진화시키는 이중 레벨 |
| 2606.26669 | SKILL-DISCO | 2026-06-25 | 실행 트레이스를 파라미터화 FSM 서브그래프 스킬로 증류 |
| 2606.00619 | MemPro | 2026-05-30 | 메모리 시스템 파이프라인 전체를 진화 가능 프로그램으로 취급 |
| 2606.19544 | Reliability without Validity | 2026-06-17 | 21개 judge 대규모 평가 — exact-match 합의가 judge 품질을 과대평가, kappa 붕괴·position bias |
| 2602.07594 | Learning to Self-Verify | 2026-02-07 | 자기검증 훈련이 생성 추론까지 개선 |
| 2603.17775 | CoVerRL | 2026-03-18 | 생성기-검증기 역할 교대 공진화로 label-free RL의 consensus trap 탈출 |
| 2606.26300 | The Verification Horizon | 2026-06-24 | 코딩 에이전트 검증이 생성보다 어려워짐 — 보상 신호는 정책과 공진화해야 |
| 2606.04923 | CHERRL | 2026-06-03 | 루브릭 judge 편향의 reward hacking 재현·탐지 통제 환경 |
| 2605.27564 | The Future of Facts | 2026-05-26 | 생성-검증 갭의 훈련 단계별 발생 추적 |
| 2603.02218 | Self-Play Learnable Information Gain | 2026-02-10 | 자기합성 파이프라인이 학습 가능 정보 이득을 보장할 때만 자기진화 지속 |
| 2602.12164 | Sci-CoE | 2026-02-12 | 희소 주석 + 기하 합의 보상으로 솔버·검증기 겸업 자기개선 |
| 2606.22329 | BabelJudge | 2026-06-21 | 통제 열화 골드라벨로 judge의 positional·length 편향과 저자원 언어 열화 정량화 |
| 2601.13649 | Fairness or Fluency? | 2026-01-20 | 페어와이즈 judge의 언어 편향 — 유창성만으로 설명 안 됨 |
| 2601.07506 | Judging Against the Reference | 2026-01-12 | 참조 답과 judge의 parametric 지식 충돌 시 평가 신뢰도 붕괴 |
| 2603.06594 | A Coin Flip for Safety | 2026-02-04 | 적대 강건성 채점 LLM judge가 사실상 동전 던지기 수준 |
| 2603.05485 | Bias-Bounded Evaluation (A-BB) | 2026-03-05 | judge 편향을 증명 가능하게 상한·저감하는 알고리즘 프레임워크 |
| 2605.06161 | Policy Invariance | 2026-05-07 | 안전 judge가 정책 변경과 무의미한 구조 개서에 동일 반응 — 신뢰도 테스트 제안 |
| 2605.26046 | When Gradients Collide | 2026-05-25 | judge용 다목적 프롬프트 최적화의 gradient dilution·instruction interference |
| 2605.29668 | GRASP | 2026-05-28 | 스킬 개발 = 유계 라이브러리에 대한 통제 편집, 순이득+비회귀일 때만 수용 |
| 2605.09315 | Do Self-Evolving Agents Forget? (CPE) | 2026-05-10 | 평생 적응 중 능력 침식 실증, Capability-Preserving Evolution 제안 |
| 2607.00871 | SEA (Anytime-Valid Certificates) | 2026-07-01 | 동결 베이스 주변 adapter로 수정 제한 + anytime-valid 검증·감사 가능 오류 예산 |
| 2605.10990 | Skill Drift Is Contract Violation | 2026-05-09 | 스킬 열화 = 실행 가능 환경 계약 위반 — 선제적 정밀 유지보수 |
| 2606.01139 | SkillRevise | 2026-05-31 | 실행 트레이스에서 결함 진단→표적 수리로 스킬 반복 정제 |
| 2605.23904 | SkillOpt | 2026-05-22 | 스킬 = 학습 가능한 외부 파라미터, held-out 검증 하 통제 텍스트 편집 |
| 2606.13317 | SkillCAT | 2026-06-11 | 대조 인과 추출·평가 증강 진화·토폴로지 인지 실행 3단계 (최대 +40.4%) |
| 2603.01145 | AutoSkill | 2026-03-01 | 상호작용 이력에서 개인화 스킬을 자동 추출·진화·재사용하는 플러그인 |
| 2605.06614 | SkillOS | 2026-05-07 | RL로 스킬 큐레이션 자체를 학습 |
| 2605.10500 | SkillEvolver | 2026-05-11 | 스킬 학습 자체를 메타스킬로 — 배포 실패에서 도메인 스킬 정제 |
| 2606.13174 | TRACE | 2026-06-11 | 사용자 교정을 런타임 강제 규칙으로 컴파일 — 메모리 대비 압도적 준수율 (70.1% vs 42.5%) |
| 2605.23899 | From Raw Experience to Skill Consumption | 2026-05-22 | 모델 생성 스킬의 비일관 전이·음의 전이 실증 + 메타스킬 완화 |
| 2606.11435 | Agent Skill Evaluation and Evolution (서베이) | 2026-06-09 | 스킬 개발이 고립 생성→평가 주도 자동 개선으로 이동, 4개 패러다임 분류 |
| 2606.10241 | Regimes | 2026-06-08 | 이벤트 소싱 런타임이 개선 루프를 감사·재현 가능하게 — LongMemEval 실증 |
| 2606.04703 | Rethinking Continual Experience Internalization | 2026-06-03 | 표준 경험 내재화는 반복 열화 — 원리 수준 경험·단계 주입·오프폴리시 증류 처방 |
| 2604.16968 | Safety Risks in Experience-Driven Agents | 2026-04-18 | 무해한 경험 축적만으로도 실행 편향에 의한 안전 취약 누적 |
| 2602.09877 | The Devil Behind Moltbook | 2026-02-10 | 자기개선·완전 고립·안전 정렬 동시 유지 불가 트릴레마 |
| 2602.15654 | Zombie Agents | 2026-02-17 | 장기 메모리에 자기강화 악성 페이로드 주입 — 세션 초월 은밀 제어 |
| 2605.18930 | OEP | 2026-05-18 | 국소 정답·비전이 경험 주입으로 메모리 통합 중 유해 규칙 일반화 유도 |
| 2606.15385 | Reward Hacking Gridworlds | 2026-06-13 | LLM 에이전트가 프록시 보상을 자연스럽게 게이밍, 표준 완화책 무력 |
| 2606.01066 | Fuzzing RLVR Verifiers | 2026-05-31 | 모델이 검증기 버그를 학습하기 전에 퍼징으로 선제 탐지 |
| 2604.13602 | Reward Hacking in the Era of Large Models | 2026-04-15 | Proxy Compression Hypothesis — 스케일에 따라 보상 해킹이 체계적으로 창발 |
| 2602.10531 | From Collapse to Improvement | 2026-02-11 | 실데이터 충분하면 오염 소스 반복 학습도 붕괴 회피 가능 (통계 분석) |
| 2509.26354 | Your Agent May Misevolve | 2025-09-30 | 모델·메모리·도구·워크플로 4경로의 의도치 않은 유해 진화 (misevolution) |
| 2607.07663 | Recursive Self-Improvement in AI (서베이) | 2026-07-08 | bounded self-refinement vs open-ended RSI 구분 — 모든 루프는 "이 신호가 인간 판단을 대체할 수 있다"는 주장 |
| 2603.25681 | LLM Self-Improvement Technical Overview | 2026-03-26 | 데이터 획득·선택·최적화·추론 정제 4과정 + 자율 평가층 통합 프레임 |
| 2601.05280 | The Singularity Is Not Near | 2026-01-05 | 외부 grounding 없는 자기학습은 entropy decay·분산 증폭으로 지속 불가 |
| 2606.01075 | Generalization Gap in Self-Evolving Reasoning | 2026-05-31 | 자기생성 신호는 개선하되 oracle 감독에 일관되게 미달 |
| 2606.05976 | The Self-Correction Illusion | 2026-06-04 | 같은 오류도 외부 입력으로 제시하면 훨씬 잘 고침 — 챗 템플릿 라벨링 효과 |
| 2602.04288 | Contextual Drag | 2026-02-04 | 문맥 내 과거 오류가 구조 유사 실수 반복 유발 (10–20% 하락) |
| 2602.16805 | Simple Baselines are Competitive | 2026-02-18 | 단순 베이스라인이 정교한 코드 진화와 대등 — 단순해의 과소평가 경고 |
| 2603.08640 | PostTrainBench | 2026-03-09 | 프론티어 에이전트의 자율 포스트트레이닝은 아직 미달 + reward hacking 관찰 |
| 2602.14029 | Why Self-Training Helps and Hurts | 2026-02-15 | denoising vs signal forgetting 트레이드오프 — U자 리스크와 최적 조기 종료 |
| 2603.03338 | AI Researchers' Views on Automating AI R&D | 2026-02-13 | 25인 인터뷰 — 20/25가 AI R&D 자동화를 최상급 리스크로 |
| 2509.08713 | The More You Automate, the Less You See | 2025-09-10 | AI 과학자 시스템의 4대 실패 모드 — 워크플로 아티팩트 검사 필수 |
| 2505.22954 | Darwin Gödel Machine | 2025-05-29 | 자기 코드 수정 + 경험적 벤치마크 검증의 개방형 진화 (계보의 기점) |
| 2606.14239 | SkillAudit | 2026-06-12 | 스킬 유/무 페어드 트래젝토리 감사로 ground-truth 없이 스킬 기여 격리 |
| 2605.29440 | SkillBrew | 2026-05-28 | 유용성·다양성·커버리지 다목적 스킬 뱅크 큐레이션 (무한 append 반대) |
| 2605.19576 | Library Drift | 2026-05-19 | 무한 스킬 축적의 침묵 열화 진단 + 거버넌스 처방 |
| 2605.13716 | SkillOps | 2026-05-13 | 스킬 라이브러리를 자가 유지 소프트웨어 생태계로 — 조합·의존성 결함 진단 |
| 2605.27366 | MUSE-Autoskill | 2026-05-26 | 스킬 생성·저장·검색·정제 라이프사이클 관리 자가진화 |
| 2605.24117 | SkillEvolBench | 2026-05-22 | 에피소드→절차 스킬 변환 벤치 — raw trajectory 재사용이 증류 스킬을 자주 능가 |
| 2412.02674 | Mind the Gap | 2024-12-03 | 자가개선의 지배 요인으로 generation-verification gap 정식화 |
| 2510.04860 | Alignment Tipping Process | 2025-10-06 | 배포 후 미스얼라인 보상 반복으로 안전 정렬 점진 이탈 |
| 2605.19196 | Time to REFLECT | 2026-05-18 | 리서치 에이전트용 LLM 심판의 실패 탐지 능력 검증 벤치 |
| 2507.08794 | One Token to Fool LLM-as-a-Judge | 2025-07-11 | 단순 토큰(master key)만으로 judge 기만 가능 |
| 2601.15808 | Inference-Time Scaling of Verification | 2026-01-22 | 추론 시점 루브릭 검증 피드백으로 학습 없는 자가진화 |
| 2605.28010 | COSE | 2026-05-27 | 내재 confidence로 노이즈 자기 피드백 가중·리플레이 우선순위화 |
| 2604.22937 | AutoPyVerifier | 2026-04-24 | LLM 출력용 컴팩트 실행형 Python 검증기 자동 합성 |
| 2601.05111 | Agent-as-a-Judge (서베이) | 2026-01-08 | 계획·도구·협업·메모리를 갖춘 agentic judge가 전통 LLM-judge 편향 극복 |
| 2603.19461 | Hyperagents | 2026-03-19 | 태스크 에이전트+메타 에이전트를 단일 편집 가능 프로그램으로 (DGM 확장) |
| 2606.26294 | Red Queen Gödel Machine | 2026-06-24 | 에이전트와 평가자의 공진화 — epoch 규율 하에 고정 평가자 방식 능가 |
| 2510.21614 | Huxley-Gödel Machine | 2025-10-24 | CMP 지표로 고잠재 자기수정 식별, SWE 벤치 인간 수준 |
| 2603.22386 | Static Templates to Dynamic Runtime Graphs (서베이) | 2026-03-23 | 워크플로 구조의 고정/실행 중 결정 기준 분류 |
| 2602.04837 | Group-Evolving Agents | 2026-02-04 | 경험 공유 집단 자기개선 — 기존 자가진화 방법 능가 |
| 2606.23075 | Safety in Self-Evolving LLM Agent Systems | 2026-06-22 | 자율 진화가 적대 위협을 세대 간 증폭·기존 방어 무력화 |
| 2604.15149 | LLMs Gaming Verifiers | 2026-04-16 | RLVR이 불완전 검증기를 악용해 라벨 암기 — 동형 섭동으로 탐지 |
| 2605.22217 | Survive or Collapse | 2026-05-21 | 셀프플레이 안정성의 핵심은 보상 설계가 아닌 데이터 게이팅 |
| 2607.05297 | MetaSkill-Evolve | 2026-07-06 | 메타스킬·태스크스킬 이중 시간척도 진화 — 개선 과정 자체의 재귀 개선 |
| 2604.01687 | CoEvoSkills | 2026-04-02 | 정답 데이터 없이 스킬 생성기와 대리 검증기(surrogate verifier) 공진화 |
| 2606.01314 | SkillSmith | 2026-05-31 | 스킬·도구 공동 진화 + 생태 동역학 모델링 + 안티패턴 추적 |

---

## 3. 상위 매핑 상세

전 11건이 priority 4. phase별 그룹: **NEW**(fast loop 즉시 삽입 가능) 8건 → **P3**(verifier 공진화 구체화) 2건 → **P2**(slow loop 전제) 1건.

### Phase: NEW: fast loop에 지금 삽입 가능한 축

#### 3.1 PACE: 수용 게이트를 anytime-valid 순차검정으로 (2606.08106)

**핵심 진단**: Deneb의 프로덕션 현실(validation case 0개 → 전 스킬 uncovered)에서 유일한 수용 게이트가 judge 1회 호출의 score delta ≥ 3.0 — 단일 노이즈 표본 점추정이다. PACE 실험 기준 이 그리디 수용은 개선 여지가 없을 때 72–100% 가짜 커밋을 낳는다. K=5 max-margin 선택(winner's curse) + burst 3라운드 + 4개 트리거 레인이 무통제 적응적 다중검정(adaptive multiple testing)을 가중 — topsolar-db 4일 18회 진화 thrash가 교과서 사례. 해법: e-process(testing-by-betting) 기반 페어드 순차검정을 **확률적 레이어에만**(judge·behavioral replay·포스트커밋 실사용) 적용. e-process는 결정적 산술이라 "결정적 게이트 Go 잔류" 불가침과 정합.

| 코드 변경 | Effort | Risk |
|---|---|---|
| `genesis/eprocess.go` 신규 — 페어드 베팅 마팅게일 순수 프리미티브, E≥1/α 수용, 배선 전 무동작 | S | 낮음 |
| `evolver_skill_validation.go`+`evolver_judge_teacher.go` — 챔피언에만 judge 페어드 재투표 2–4회(순서 랜덤화) e-process 확인 게이트 (select-then-verify) | M | judge 호출 +1–3회, α 과엄격 시 uncovered 진화 고사 |
| `tracker_usage.go` — 롤백 워치를 baseline-aware 순차검정으로 (pre-evolve 실패율 스냅숏 대비 베팅) — 현행 3-in-6 규칙은 개선-but-불완전 스킬을 오롤백 | M | 롤백 타이밍 변화, 로그 리플레이 검증 필요 |
| `validation_engine.go` — behavioral replay를 페어드 e-테스트로 (예산 캡 내 재실행) | M | executor 호출 증가 (상한 있음) |
| `tracker.go` — false-commit율·e-process 상태 /health 노출 | S | 낮음 |
| `evolver.go` — 스킬별 alpha-investing 오류 예산 원장 | M | 예산 회복 정책 설계 필요 |
| `docs/agent-rules/self-improvement.md` — P2 수용 기준을 인터리브 페어드 검정으로 재스펙 | L | 관측 기간 연장 |

#### 3.2 TRACE: 사용자 교정을 런타임 강제 규칙으로 컴파일 (2606.13174)

**핵심 진단**: 07-10 모닝레터 회귀(위키 규칙 미로드 → 양식 위반 발송)가 정확히 TRACE의 실패 모드 — 선호가 회상(recall) 의존이라 로드 실패 시 무방비. TRACE ablation: 컴파일 강제 70.1% vs advisory 텍스트 55.0% vs Mem0 42.5%. Deneb은 강제 프리미티브(FinalizeGate·OnBeforeToolCall·PostProcessRegistry)를 이미 전부 보유 — 필요한 건 새 인프라가 아니라 "교정 유래 규칙 데이터 레인". 교정 마이닝 최고 정밀 포인트가 이미 코드에 존재: steer 큐(`trySteerIntoActiveRun`)가 미드런 교정을 폴딩 후 버림. 강제 어휘는 검증케이스 어휘(`SkillReplayCaseRecord`) 재사용, block 티어 활성화는 오퍼레이터 승인(propose-only).

| 코드 변경 | Effort | Risk |
|---|---|---|
| `tracker_user_corrections.go` 신규 — UserCorrectionRecord JSONL 사이드카 | M | 낮음 |
| `pipeline/chat/steer.go` — 폴딩된 교정 텍스트 결정적 티잉 (fail-open) | S | 낮음 |
| `genesis/rules.go` 신규 + `editable_surfaces.go` — RuleRecord(remind/verify/block 티어) + 5-액션 resolver | L | 규칙 스키마가 계약이 됨 |
| nudger 리뷰 프리셋 — 교정 마이닝 액션 (tiny/lightweight 역할) | M | 과잉 마이닝은 dedup+캡 흡수 |
| agentsys OnBeforeToolCall — block 티어 규칙 게이트 (자동 suspend + 킬스위치 3중 방어) | M | 중상 — 오발동 시 정상 툴콜 차단 |
| FinalizeGate 조합 — verify 티어 출력 assertion (모닝레터 '유로 금지'가 여기서 잡힘) | M | false-positive 비용 1턴 한정 |
| steer-note 주입 — remind 티어 (prompt-cache 불가침 준수, 시스템 프롬프트 불변) | S | 낮음 |
| lifecycle 규칙 통계 + rule→skill 마이그레이션 승격 레인 | M | 낮음 |
| 마이너·컴파일러 프롬프트 메타 아티팩트 외부화 (P1 패턴) | S | 낮음 |

#### 3.3 SkillAudit: 페어드 트래젝토리 감사 (2606.14239)

**핵심 진단**: Deneb 최대 갭 = 스킬 유/무 비교 부재. 현행 게이트는 원본-vs-후보만 페어링하므로 "이 스킬이 애초에 도움이 되는가(한계효용)"를 측정 못 함. 무스킬 베이스라인 암(arm)을 워크아웃 레인(12h)에 추가하면(케이스ID+모델 키 캐시로 한계비용 ~0) 케이스 판별 라벨이 공짜로 나옴: **noise**(베이스라인도 통과)·**earning**(스킬이 밥값)·**harmful**(스킬이 적극 해악). 이 라벨이 P3가 필요로 하는 학습 데이터(롤백 라벨보다 싸고 연속적). 추가로 genesis 생성 시점 **동결 차터 케이스**(compile-once-then-fixed) = 공진화가 오염 못 하는 고정 앵커.

| 코드 변경 | Effort | Risk |
|---|---|---|
| `workout.go`+`validation_engine.go` — runBaselineReplay + classifyCaseDiscrimination 순수 함수 | M | 워크아웃 캡으로 유계 |
| `tracker_case_audit.go` 신규 — 감사 레코드 JSONL + /health 요약 | S | 관찰 전용 |
| `evolver.go`+`evolver_prompt_format.go` — PACE-lite 용의 섹션 랭킹 프롬프트 주입 | M | 프롬프트 전용 |
| `evolver.go` — Refine/Repair 증거 기반 모드 라우터 (K 후보 분할) | S | 최악은 무변화 |
| `genesis.go` 등 — 동결 차터 케이스 증류 (source=charter, 프루닝·공진화 제외) | M | 틀린 불변식 동결 가능 — weak-case 게이트 완화 |
| `evolver_skill_validation.go` — noise-only 스킬의 covered 완화 티어 차단 (커버리지 정직화) | S | 보수 방향이라 안전 |
| `curator.go` — zero-marginal-utility advisory | S | advisory 전용 |

#### 3.4 SEA: 인증서 원장 + anytime-valid 승격/롤백 (2607.00871)

**핵심 진단**: genesis는 SEA의 구조적 전제(frozen base=소스 forbidden self-edit, versioned harness=SKILL.md+메타 아티팩트)를 이미 갖춤 — 빠진 것은 판정 통계와 감사 원장. 현행 "확정=6회 실사용 생존"은 최대 ~33% 실패율도 확정하는 셈. 부수 발견: **postEvolve 워치가 메모리 전용이라 SIGUSR1 재시작마다 활성 워치가 조용히 증발**. 3대 적용: (1) certificate 원장 — lifecycle에 게이트 점수·margin·e-value·소모 δ·버전 구조화 기록(P3 라벨 추출의 선행 의존성이자 가장 싼 항목), (2) reproduction oracle(SEA Alg 8) — evolve 시점에 producer가 "원본 FAIL·후보 PASS" 검증 케이스를 저작, 결정적 게이트로 확인된 것만 채택 → '검증 케이스 0개' 상태를 evolve가 일어나는 그 지점에서 해소, (3) CTHS식 감쇠 예산 — k번째 재진화일수록 강한 증거 요구.

| 코드 변경 | Effort | Risk |
|---|---|---|
| `tracker_usage.go`+`eprocess.go` — e-process 롤백/확정 워치 + 워치 영속화(재시작 생존) | M | 희소 트래픽 판정 지연 (고정 백스톱 병행) |
| `tracker_lifecycle.go`+`evolver_candidate_eval.go` — Certificate 구조 (점수·margin·e-value·δ·버전) | S | 낮음 — additive 필드 |
| `prompts.go`+`validation_engine.go` — reproduction_case 필드 + fails-on-original 결정 게이트 | M | 과적합 케이스 — weak-case 필터 방어 |
| `evolver.go` — CTHS식 스킬별 감쇠 예산 (k 단조 증가 임계) | S | 과도 감쇠 시 상한 캡 필요 |
| `tracker_usage.go` — confirm-rate Hoeffding 드리프트 알람 (P3 착수 트리거) | S | advisory 전용 |
| 로드맵 문서 — P2 판정을 SGM-CS식 per-version 누적 LCB로 명세 | M | 주 1회 표본 수렴 느림 |

#### 3.5 AgentDevel: 자가진화 = 릴리스 엔지니어링 (2601.04620)

**핵심 진단**: Deneb은 이미 ~70% 정렬 (단일 정본 라인·promote-or-discard·롤백 워치·lifecycle JSONL). 3대 갭: **(1) flip 마스킹** — 집계 비교(cand.Passed < orig.Passed)라서 통과하던 assertion N개를 깨며 실패하던 N개를 고친 후보가 통과 (P→F가 F→P로 상쇄; 논문 ablation: flip 게이트 제거 시 회귀율 3.1%→14.8%), **(2) held-out 격리 위반** — 프롬프트에 노출되는 케이스 5개가 게이트 채점 20개에 포함 → producer가 보여준 required substring을 베끼면 자기충족 통과, **(3) 정지 기준 부재** — 반려만 반복되는 스킬이 6h마다 K=5 비용을 영원히 태움.

| 코드 변경 | Effort | Risk |
|---|---|---|
| `validation_engine.go` — assertion 단위 flip 집합 계산, P→F 발생 시 차단 + lifecycle 영속화 | M | accept율 하락 (의도) |
| `evolver_candidate_eval.go` — K-선택을 (P2F==0, F2P수, margin) 사전식 랭킹으로 | S | 미미 |
| `evolver_skill_validation.go` — 케이스 해시 분할: train(노출)/gate(격리), 개선 마진은 gate에서만 | M | 희소 코퍼스 스킬은 gate 파티션 공백 |
| `evolver.go` — 반려-스트릭 서킷브레이커 (동일 게이트 클래스 연속 N회 → 쿨다운) | S | 지연 비용뿐 |
| `evolver.go` — 진단 스냅샷(FailureEvidenceClusters 서브셋) lifecycle 첨부 | S | 로그 성장 유계 |
| F→P intent-alignment 검사 (advisory 선행) | M | 매핑 느슨 — advisory 필수 |
| `RunSkillReview` — 구현-블라인드 critic 분리 (증상 라벨만, 수리 제안 금지) | L | evolve 품질 저하 가능 — confirm-rate 비교 도입 |
| P2를 shadow-replay flip 게이트 RC discipline으로 | L | 코퍼스 얇으면 fail-closed 스킵 |

#### 3.6 TPGO: 경험 메모리 메타학습 (GRAO) (2604.20714)

**핵심 진단**: 최대 갭 = 양성 exemplar 회수 부재. 새 실패 시그니처 S로 스킬 A를 진화시킬 때 "같은 시그니처를 스킬 B에서 성공적으로(confirmed) 고친 audit"을 교차-스킬 few-shot으로 주입하는 경로가 없음 — lifecycle log에 데이터는 전부 있고 회수기만 없다(최소 비용·최대 가치). GRAO ablation의 결정적 경고: **경험 메모리 없는 반복 메타 최적화는 붕괴한다(30.0%→14.5%)** — 현 P2 설계에 사이클 간 메모리가 없어 직격.

| 코드 변경 | Effort | Risk |
|---|---|---|
| `tracker_lever_yield.go` — HighYieldLevers + ConfirmedEvolveExemplars(signature, limit) 교차-스킬 회수기 | S | 읽기 전용 |
| `evolver.go` — evolve 프롬프트에 고수율 exemplar few-shot 섹션 (최대 3건) | S | 나쁜 exemplar는 게이트가 기각 |
| `tracker_recurrence_promotion.go` — support≥2 클러스터에서 weak validation case 자동 pin (targeted validation) | M | 저품질 케이스 — weak 티어·캡·fail-open 완화 |
| `tracker_optimizer_memory.go` — StableDirections 승격을 confirm 시점으로 이연 | S | 테스트 갱신 |
| `evolver.go` — K-선택 탈락 후보 margin 기록 (그룹-상대 감사) | S | 없음 |
| 로드맵 — P2에 메타-경험 메모리 필수 컴포넌트 명시 | S | 미반영 시 GRAO 붕괴 리스크 |
| 섹션 단위 패치 op (TPG 노드 편집 완전 채택) — **후순위, P4 이후** | L | 재조립 버그 — 성급 진입 금지 |

#### 3.7 CPE: anchor 보존 게이트 (2605.09315)

**핵심 진단**: 자가진화는 비단조 — 획득 게이트와 분리된 **보존 게이트**(anchor no-regression)가 없으면 침식된다(논문: 보존 적용 시 기존 과제 41.8→52.8%p 회복). Deneb 최대 침식 경로: held-out 코퍼스가 최신 20케이스 윈도우라 "이미 고친 실패" 앵커가 밀려나 재발 가능 — FrontierTier easy 스키마는 이미 있는데 케이스 선택에 미배선. 2026-07-09 aggressive 모드 전환(편집비율 0.85·judge delta 6→3·K=5·burst 3)으로 시급성 상승 — **burst가 1-레벨 백업(.prev)을 라운드마다 덮어써 라운드1 침식은 복원 불가**.

| 코드 변경 | Effort | Risk |
|---|---|---|
| `tracker_validation_cases.go` — AnchorAwareValidationCases (easy 티어 상시 N개 포함) | S | 대형 코퍼스 스킬만 엄격화 |
| `validation_engine.go` — 보존/획득 채점 분리 (앵커=즉시 reject, 개선 delta=frontier만) | M | fail-closed 방향이라 안전 |
| `tracker_lifecycle.go` — confirmed evolve → 앵커 케이스 자동 증류 | M | 저품질 앵커 — guard+cap |
| `evolver_regression_rollback.go` — 롤백 body를 rejected-edit + 실패 앵커로 기록 (P3 결정적 절반 선행) | S | 낮음 — 기록만 |
| `workout.go` — post-evolve retention probe + capability_erosion 이벤트 (advisory) | M | 결정적 스코어러면 비용 0 |
| /health — retention 차원 (CapabilityErosion7d 등) | S | 표면 추가만 |
| 백업 lineage — 버전 히스토리 5개 + last-confirmed-good 포인터 (burst 구멍 봉합) | M | 마이그레이션 — .prev 폴백 유지 |
| P2 frozen anchor bench (과거 확정 판정 리플레이) | L | LLM 의존 flaky — 결정적 서브셋부터 |

#### 3.8 Verifier Fuzzing: 루프가 버그를 학습하기 전에 (2606.01066)

**핵심 진단**: genesis evolve 루프는 미니 RLVR — K=5 셀렉터가 held-out margin을 직접 최적화하므로 **검증기 버그는 우연히 밟히는 게 아니라 선택된다**. 코드로 확인된 실제 악용면 4종: (1) zero-width/전각 문자로 forbidden substring 우회, (2) assertion echoing — required 문구를 아무 데나 베껴 넣으면 margin 상승 (uncovered 스킬에서 사실상 유일 하드 게이트), (3) 정규화 후 빈 forbidden 단언 → min-delta 영구 기각 웨지(해당 스킬의 모든 후보가 영원히 기각), (4) countRetainedLines가 줄 재배열을 '유지'로 계산. 게이트가 이미 순수 함수라 `go test -fuzz` 배선 비용이 거의 없음.

| 코드 변경 | Effort | Risk |
|---|---|---|
| `gate_fuzz_test.go` 신규 — 퍼즈 타깃 4종 + 메타모픽 불변식 + 익스플로잇 시드 체크인(래칫) | M | 테스트 전용 무위험 |
| `validation_replay.go` — normalizedValidationTextStrict (NFKC+zero-width 제거) 섀도 채점기 | S | 승격 시 경계 이동 — 섀도 계량 후 |
| `validation_engine.go` — 비판별(uncertainty) 케이스 격리 (min-delta 웨지 해소) | S | 과발동 시 코퍼스 약화 — 전량 로그 |
| `verifier_fuzz.go` 신규 — 결정적 적대 뮤턴트 배터리 (5-분류 findings, advisory) | M | 뮤테이터 품질 의존 |
| heartbeat 스윕 배선 + /health VerifierFindings7d | M | 로그 노이즈 — 캡+캐시 |
| `heldOutSelectionMargin` anti-echo 할인 (랭킹만 영향, 기각 아님) | M | fail-safe |
| `scripts/audit/verifier-fuzz.py` — wormhole LLM 적대 생성 (오프라인) | L | 진실원은 결정적 Go 리플레이 |

### Phase: P3: verifier 공진화 구체화

#### 3.9 Red Queen Gödel Machine: epoch 규율·judge 패널·보정 벤치 (2606.26294)

**핵심**: 평가 기준을 개선 루프의 일부로 만들되 **epoch 안에서는 동결, 갱신은 epoch 경계에서만** — Deneb P2/P3와 정확히 동형이며 로드맵의 이론적 근거를 보강. 3대 이식: (1) **평가자 버전 귀속** — 모든 lifecycle 엔트리에 활성 메타 아티팩트 해시 + judge 점수쌍 스탬프 (P1 방금 랜딩한 지금이 최적 시점 — 지금 안 하면 P3가 라벨 없이 시작), (2) uncovered 스킬 한정 **2-judge 패널**(만장일치 accept, fail-closed) — 논문 1.78–1.86x accept 품질, (3) **judge 보정 벤치** — confirmed/rolled_back에서 should-accept/should-reject 본문쌍 수확, judge 프롬프트 개정은 "신 judge false-accept ≤ 구 judge" Go 게이트 통과 시에만 승격. Red Queen 안티게이밍: producer-epoch와 evaluator-epoch 교대 강제(같은 epoch에 생성기·평가자 동시 진화 금지).

| 코드 변경 | Effort | Risk |
|---|---|---|
| `meta_artifacts.go` — Version()(SHA-256 12hex) + ActiveVersions() | S | 사실상 무위험 |
| `tracker_lifecycle.go` 등 — judge/evolve 아티팩트 버전·judge 점수쌍 additive 스탬프 | M | genesis 내부 국한 확인 필요 |
| `evolver_judge_teacher.go` — uncovered 한정 2-judge 패널 | M | evolve 처리량 감소 모니터 필요 |
| `judge_calibration.go` 신규 — 보정 코퍼스 수확 + ReplayJudgeCalibration 승격 게이트 | L | 엑시빗 축적 느림 — 수확 배관 선랜딩이 핵심 |
| `tracker_usage.go` — EvolutionHealthForEpoch (epoch-절단 뷰) | M | 스캔 비용 — 캐시 재사용 |
| 로드맵 — epoch 교대 ground rule 명문화 | S | meta 반복 2배 감속 (의도) |

#### 3.10 CoVerRL: consensus trap 차단 (2603.17775)

**핵심**: 다수결 pseudo-label을 그대로 보상으로 쓰면 다양성 붕괴 + 시스템적 오류의 자신감 있는 강화. Deneb의 트랩 표면(코드 확인): held-out 코퍼스가 스킬 자신의 성공 사용에서 자동 증류 → 잘못된 행동이 필수 assertion으로 고착되면 게이트가 그걸 고치는 후보를 오히려 기각. K=5가 같은 producer의 variation note 하나로만 분화 → 다양성 붕괴 시 margin 랭킹이 과적합을 증폭. 메커니즘: (a) verifier 정확도 스코어보드(judgeAccuracy=confirmed/(confirmed+rolledBack)), (b) **pseudo-label 위생** — rollback 발화 시 그 창의 auto-* 케이스 tombstone 격리, (c) K-후보 다수결 합의는 confirm-gated 승격만 (합의≠진실).

| 코드 변경 | Effort | Risk |
|---|---|---|
| `tracker.go` — judgeAccuracy·falseAcceptRate 스코어보드 (/health) | S | 초기 소표본 과신 — n 병기 |
| `evolver.go` — K 후보 pairwise 유사도 계측 + variation 결정적 에스컬레이션 | S | 관측 위주 |
| `tracker_validation_cases.go` — rollback 창 auto 케이스 tombstone 격리 (오퍼레이터 케이스 불가침) | M | 과도 격리 시 커버리지 축소 |
| `evolver_judge_teacher.go` — false-accept/false-reject exhibit judge 프롬프트 주입 (아티팩트 불변, 데이터만 신선) | M | 프롬프트 비대 — N≤3 캡 |
| `tracker_rejected_edits.go` — judge_false_reject_suspect 라벨 (advisory, 자동 재수용 절대 없음) | M | 기각이 옳았을 가능성 |
| 다수결 합의 증류 (auto-consensus, confirm-gated) | L | 스타일 고착 — diversity 계측 후 판단 |

### Phase: P2: slow loop 전제

#### 3.11 BabelJudge식 judge 신뢰도 감사 (2606.22329)

**핵심**: gold-labelling by degradation을 Go로 이식 — 검증된 스킬 본문에 결정적 열화 연산자(섹션 삭제·실존하지 않는 툴명 스왑·forbidden 행동 주입·순서 파괴·verbosity 패딩)를 적용해 라벨 확정된 (원본, 열화본) 페어를 사람 라벨 없이 생성하고, **judge가 원본을 이기게 판정하는지로 judge 자체를 채점**한다. 프로덕션에서 judge가 유일한 행동 게이트인데 그 정확도·일관성이 무측정 상태라는 점이 이 감사를 급하게 만든다. 스왑 일관성 감사(order-inconsistency): 현행 judgeCandidate는 항상 "원본 먼저" 고정 제시 — `.backups/*.prev` + lifecycle의 역사적 페어로 라벨 비용 0의 스왑 감사 즉시 가능. borderline 판정에만 역할 스왑 이중 판정(불일치 시 fail-closed). KO/EN 프레이밍 벤치로 judge 언어를 "측정된 결정"으로. **P2의 필수 전제**: 메타 judge 아티팩트를 진화시킬 때 열화 골드페어 벤치가 유일한 결정적 fitness 게이트 — 없으면 judge가 자기 프롬프트 개선을 자평하는 순환.

| 코드 변경 | Effort | Risk |
|---|---|---|
| `judge_audit.go` 신규 — 결정적 열화 연산자 세트 + 골드페어 생성기 (각 연산자가 실제로 채점기를 낙제시키는지 단위테스트) | M | 낮음 — 신규 파일, LLM 호출 없음 |
| (매핑 데이터 일부 절단 — ideas 기준 잔여 항목) 역사적 스왑 감사 · borderline 이중 판정 fail-closed · EvaluateBehavior 민감도 측정 · KO/EN 캘리브레이션 · EvolutionHealthSummary judge 정확도·일관성 필드 (insufficient-corpus 처리, 일관성 바닥 미달 시 uncovered 마진 결정적 상향) | M–L 추정 | borderline 이중 판정만 런타임 비용, 나머지 감사·관측 전용 |

---

## 4. 로드맵 수정 플래그 모음

**11건 전부 `roadmap_change_flag=true`.** 개별 요지와 수렴 구조:

| 매핑 | 수정 요지 |
|---|---|
| PACE | P1–P2 사이 "acceptor 하드닝" 삽입. P2 fitness(주간 health 델타 점추정+자동 revert)가 PACE가 경고하는 무통제 노이즈 수용 그 자체. P3 라벨은 baseline 무시 3-in-6 롤백의 오롤백에 오염 — baseline-aware 검정을 P3 라벨 수집 전에 |
| TRACE | "교정→강제 컴파일" 레인 신설 (P2–P3 사이 또는 병행). P2 비블로킹, P3에 false-block/false-accept 라벨 선공급, P4에서 스킬+도구+규칙 3요소 번들로 확장 |
| SkillAudit | P2.5 페어드 감사 레인 삽입. 케이스 판별 라벨(noise/earning/harmful)을 P3의 제2 라벨원으로 명시 편입. **동결 차터를 P3 공진화 대상에서 명시 제외** — false-accept 드리프트의 구조적 헤지 |
| SEA | P1.5 삽입: certificate 원장 + anytime-valid 워치 + reproduction oracle. 원장은 P3 라벨의 선행 의존성. P2 판정을 SGM-CS식 per-version 누적 LCB로 교체. reproduction oracle은 "검증 케이스 0" 해소 — 일찍 넣을수록 복리 |
| AgentDevel | P1.5 신설 (flip 게이트·held-out 격리·반려-스트릭 브레이커·진단 스냅샷 — P2가 재사용하므로 선행). P2 fitness를 "shadow-replay flip 게이트 주(主) + health 델타 advisory"로. P3는 flip 기록 영속화를 학습 기질로 명시 |
| TPGO | P1.5 GRAO 경험 회수 레인 삽입 (지금 착수 가능, P2 재사용). **P2에 메타-경험 메모리 필수 명시** — 무기억이면 GRAO 실증 붕괴(30.0→14.5%) 그대로 |
| CPE | P2 fitness를 "frozen anchor bench 보존 게이트 통과 + health 델타 보조"로. P3의 결정적 절반(롤백 body 기록+실패 앵커 증류)은 지금 구현 가능 → fast loop로 앞당김. "anchor 보존 게이트"는 P1–P4 어디에도 없는 횡단 축 — aggressive 모드로 시급성 상승 |
| RQGM | P2 전제로 평가자 버전 귀속 (지금 안 하면 P3가 라벨 없이 시작). P3에 결정적 judge 보정 벤치 명시 ("주입+게이트"로 강화). ground rule: producer/evaluator epoch 교대. uncovered 2-judge 패널은 독립 선행 배달물 |
| CoVerRL | 앵커 표에 CoVerRL 추가, P3 확장: 다수결 합의(confirm-gated) 라벨 보강 + consensus-trap 가드(격리·다양성 모니터) 명시 + verifier 정확도를 P2 fitness 차원에 포함. 스코어보드·다양성 계측(S 2건)은 P2 전 선행 |
| Verifier Fuzzing | **P2.5 "공진화 전에 퍼징"을 P3의 명시적 전제조건으로** — 게이트가 악용 가능하면 P3 라벨 자체가 오염(악용 통과→롤백→false-accept 오라벨). P2 fitness 가드 메트릭에 VerifierDisagreements 추가 |
| BabelJudge | 열화 골드페어 벤치를 P2 메타 judge 진화의 유일한 결정적 fitness 게이트로 — 없으면 judge 자평 순환. dense한 열화 라벨로 P3의 희소 rollback 라벨 보강 |

**수렴 구조 (11건이 독립적으로 같은 결론에 도달):**

1. **P1.5 "acceptor 하드닝" 단계 신설이 사실상 만장일치** — 구성 요소: certificate/lifecycle 원장 + 평가자 버전 귀속 (SEA·RQGM), e-process 수용/롤백 검정 (PACE·SEA), flip 게이트 + held-out 격리 (AgentDevel), GRAO exemplar 회수 (TPGO), anchor 보존 (CPE), reproduction oracle (SEA). 공통 논리: *P2/P3가 이 산출물을 기질로 소비하므로 순서상 선행이 옳다.*
2. **P2 fitness "주간 health 델타 + 자동 revert"는 6개 매핑이 각자 다른 논거로 기각** — 노이즈 점추정(PACE), 무기억 반복 붕괴(TPGO), 침식 미탐(CPE), flip 없는 회귀 허용(AgentDevel), judge 자평 순환(BabelJudge), reward 정확도 미관측(CoVerRL). 대체 합의: **페어드/앵커드/flip-게이트 결정적 벤치 주(主) + health 델타 advisory 강등**.
3. **P3 라벨 품질이 3중으로 위협받고 있음** — 오롤백 오염(PACE), 게이트 악용 오염(Fuzzing), 코퍼스 자기오염(CoVerRL·SkillAudit). 처방도 3중: baseline-aware 롤백 선행 + 퍼징 선행 + 차터 케이스 공진화 제외.
4. **불가침 원칙은 11건 전부에서 무손상** — 모든 통계·격리·라우팅·귀속 로직은 결정적 Go, LLM은 후보·케이스·exhibit 생산만, 소스코드 self-edit 표면 확장 제로.

---

## 5. 다음 커밋 후보: 지금 바로 착수 가능한 상위 5개

선정 기준: effort S~M · 게이트 동작 무변경(또는 순수 신규) · P1.5/P2/P3의 기질을 지금부터 축적 · 독립 랜딩 가능.

### ① Lifecycle certificate 원장 + 평가자 버전 귀속 (SEA + RQGM 합류): **최우선**

- **파일**: `gateway-go/internal/domain/skills/genesis/meta_artifacts.go` (Version()·ActiveVersions() 헬퍼, SHA-256 12hex), `tracker_lifecycle.go` + `evolver_candidate_eval.go` + `evolver_judge_teacher.go` (additive 필드: judgeArtifactVersion·evolveArtifactVersion·judgeModel·judge 점수쌍·held-out orig/cand·margin·behavioral counts)
- **왜 지금**: P1이 방금 랜딩해 아티팩트 파일이 존재하는 첫 시점. **하루 늦을수록 P3의 false-accept 라벨이 하루치 소실된다** — 시간에 비례해 가치가 쌓이는 유일한 후보. JSONL additive라 하위호환, 게이트 무변경. Effort S+M / Risk 낮음.

### ② `eprocess.go` 순수 프리미티브 + 롤백 워치 영속화 (PACE + SEA)

- **파일**: `genesis/eprocess.go` 신규 (페어드 베팅 마팅게일, α·베팅 파라미터 Go 상수, 테이블 주도 anytime-validity 테스트), `tracker_usage.go` (evolveWatch에 pre-evolve 베이스라인 스냅숏 필드 + liveness JSON 영속화)
- **왜 지금**: 프리미티브는 배선 전까지 동작 무변경(Risk 최저)인데 후속 게이트 3곳(챔피언 확인·롤백 워치·behavioral replay)이 전부 이걸 소비. 워치 영속화는 **SIGUSR1 재시작마다 활성 롤백 워치가 증발하는 확인된 버그**의 수정을 겸함. Effort S+M.

### ③ 롤백 증거 영속화 + confirmed→앵커 증류 (CPE: "P3의 결정적 절반")

- **파일**: `evolver_regression_rollback.go` (RollbackSkill에서 recordRejectedSkillEdit(reason="post-evolve rollback") + 롤백 유발 실패 트레이스의 validation case 증류), `tracker_lifecycle.go` (confirmEvolve에서 audit 기반 FrontierTier=easy 앵커 케이스 자동 기록, weak-case 가드+캡 통과)
- **왜 지금**: 현재 롤백은 lifecycle 로그만 남겨 **같은 나쁜 편집이 재제안될 수 있다**. 순수 기록 추가(게이트 불변)로 LLM 공진화 없이 P3 라벨의 결정적 전단계를 확보하고, 2026-07-09 aggressive 모드 전환으로 올라간 침식 리스크에 첫 앵커를 심는다. Effort S+M / Risk 낮음.

### ④ 게이트 퍼즈 하니스 + min-delta 웨지 해소 (Verifier Fuzzing)

- **파일**: `genesis/gate_fuzz_test.go` 신규 (FuzzScoreSkillValidationCases 등 4타깃 + 메타모픽 불변식 + 확인된 익스플로잇 4종 시드 체크인), `validation_engine.go` (정규화 후 빈 forbidden 단언 등 비판별 케이스를 Total에서 격리 + 로그)
- **왜 지금**: **min-delta 영구 기각 웨지는 확인된 라이브 버그** — 고칠 수 없는 단언 하나가 해당 스킬의 모든 후보를 영원히 기각한다. 퍼즈는 테스트 전용(런타임 무위험)이고, K=5 셀렉터의 최적화 압력이 P2/P3에서 더 커지기 전에 래칫을 걸어야 한다. Effort M+S.

### ⑤ GRAO exemplar 회수기 + verifier 정확도 스코어보드 (TPGO + CoVerRL)

- **파일**: `tracker_lever_yield.go` (ConfirmedEvolveExemplars — normalizedSelfHarnessSignature 일치하는 교차-스킬 confirmed evolve를 ConfirmRate 순 반환 + HighYieldLevers), `evolver.go`/`evolver_prompt_format.go` (exemplar few-shot 섹션 ≤3건), `tracker.go` (judgeAccuracy=confirmed/(confirmed+rolledBack)·falseAcceptRate·K-후보 pairwise 유사도를 EvolutionHealthSummary→/health 노출, n 병기)
- **왜 지금**: lifecycle log에 데이터가 이미 전부 있고 회수기만 없는 **최소 비용·최대 가치** 지점(전부 읽기 전용 결정적 Go). 스코어보드는 P2 fitness의 필수 차원이자 "judge가 조용히 물러지는" 드리프트의 첫 관측창 — ①의 버전 귀속과 결합하면 버전별 judge 정확도까지 즉시 나온다. Effort S×3 / Risk 낮음.

**공통 검증 경로**: 전부 gateway-go 단일 레인 — `make check` → `make ci/fast` → 게이트웨이 동작 변경분(①②③⑤)은 `scripts/dev/live-test.sh restart && smoke`로 라이브 검증. 커밋은 `scripts/committer`, Conventional Commit (`feat(genesis): …` / `fix(genesis): …`).

**후순위 보류 명시**: TPG 섹션 단위 패치 op(TPGO, L — 재조립 버그가 스킬 본문을 훼손할 수 있는 유일한 구조적 변경), 구현-블라인드 critic 분리(AgentDevel, L — uncovered seeding 품질 저하 리스크), wormhole 적대 생성 스크립트(L — 결정적 배터리 성숙 후), TRACE block-티어 런타임 게이트(오퍼레이터 승인 체계 선행 필요 — remind/verify 티어와 데이터 레인부터).
