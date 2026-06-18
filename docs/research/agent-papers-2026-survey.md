---
title: "Agent Papers 2026 Survey"
summary: "2025-2026 LLM agent research (31 papers, 155 claims) mapped to Deneb improvement opportunities, prioritized."
read_when:
  - "Looking for research-grounded improvement ideas for the chat pipeline, memory, skills, or proactive systems"
  - "Evaluating whether a proposed agent technique has empirical support or known refutations"
  - "Deciding priorities for the next round of gateway optimization work"
sidebarTitle: "Agent Papers 2026"
---

# Agent Papers 2026 Survey

> **세부 탐색 후속편**: 6개 핵심 발견의 논문 원문 재검증·코드 ground truth·구체 설계는 [Agent Papers 2026 Deep Dive](/research/agent-papers-2026-deep-dive) 참조. 이 서베이의 결론 중 3건(위키 supersession 부재, 역할 재라벨 적용처, dreaming collapse 위험)이 거기서 **정정**되었고 우선순위 표도 갱신되었다.

> 2026-06-13 조사. deep-research 하네스(6각도 분해 → 병렬 검색 → 31개 1차 소스 fetch → 155개 주장 추출 → 적대적 검증)로 수행. 검증 단계가 API 세션 한도로 조기 종단되어 **4건만 3-0/2-0 만장일치 확정, 2건 1-0, 나머지는 출처 직접 인용 기반(미검증)**. 수치는 대부분 논문 저자의 self-report이며 2026년 preprint(피어리뷰 전)가 다수 포함된다. 코드 매핑은 당일 코드베이스 탐사(챗 파이프라인 + 기억·진화·능동 서브시스템) 기준.

## 핵심 발견 (Deneb 관점 우선순위)

1. **추출-저장형 메모리는 정보를 잃을 수 있고, "사실 수정(망각)"은 전 시스템 공통 최약점.** MemoryAgentBench에서 상용 메모리 레이어(Mem0 ~32.6, Zep ~37.5)가 정확 검색에서 bare long-context 백본(~49.2)보다 낮았고, multi-hop 사실 수정은 전부 ≤7%. Deneb는 hindsight(추출) + wiki/diary/transcript(원문 검색)를 병행하므로 구조적 헷지가 이미 있으나, **decay/supersession 메커니즘 부재**는 동일한 약점.
2. **모놀리식 LLM 재요약은 "context collapse"를 실증적으로 일으킨다** (ACE: 1스텝에 18,282→122토큰, 정확도가 무적응 베이스라인 아래로). polaris의 append형 요약 DAG는 이 함정을 피하고 있고, 위험 지점은 dreaming의 위키 문서 병합 경로.
3. **자기생성 스킬은 평균적으로 성능을 깎는다** (SkillsBench: 인간 큐레이션 +16.2pp vs 자기생성 -1.3pp). genesis/evolve 루프는 검증 게이트 없이는 "skill debt"를 쌓는다.
4. **셀프 크리틱은 역할 재라벨만으로 크게 좋아진다.** 모델은 자기 출력 오류의 64.5%를 못 잡지만(blind spot), 같은 내용을 user/tool/memory 역할로 제시하면 수정률 +23~93pp. 진화 self-test 등 모든 자기검증 루프에 공짜로 적용 가능.
5. **effort 라우팅의 다음 단계는 학습형 per-step 라우터.** 동명 논문 Ares(arXiv:2603.07915)가 "스텝 성공에 필요한 최소 effort" 라벨을 자동 생성해 1.7B 라우터를 학습, reasoning 토큰 -52.7%에 성공률 무손실. Deneb의 o_t feed(#2274)가 이 라벨 마이닝의 원료.
6. **능동형의 지배적 실패는 과개입(미개입이 아님).** 최고 프런티어 모델도 오경보 ~50%. Deneb의 contentless 억제 방향이 맞고, 다음은 수락/거부 신호 수집과 과개입율(FTR) 계측.

---

## 축 1 — 장기기억

| 논문 | 핵심 결과 | 주의/한계 |
|---|---|---|
| **Zep/Graphiti** (arXiv:2501.13956, 2025-01) | DMR 94.8% vs MemGPT 93.4% [검증 2-0]. bi-temporal 모델(사건 타임라인 T + 인제스트 타임라인 T'), 모순은 삭제 대신 edge invalidation(t_invalid) [검증 3-0]. 3계층 서브그래프(raw episodes/entities/communities). LongMemEval +18.5% relative & latency -90% [1-0] | 저자 스스로 DMR 벤치 한계 인정(60메시지, 단일턴 사실검색) |
| **Mem0** (2504.19413, 2025-04) | LOCOMO LLM-judge에서 OpenAI 내장 메모리 대비 +26% [검증 3-0]. p95 latency -91%, 토큰 -90%+ | **그래프 변형(Mem0g)은 base 대비 겨우 +2%** [검증 3-0] — 그래프 기계장치의 한계효용 작음 |
| **MemoryAgentBench** (2507.05257, 2025-07) | 4역량 분해(정확 검색·test-time 학습·장거리 이해·선택적 망각). 어떤 시스템도 4역량 동시 마스터 못함. **사실 수정: Mem0/Cognee/Zep 전부 ≤7%**. 상용 메모리가 정확 검색에서 long-context 백본·단순 RAG보다 낮음 | 2,071문항 incremental injection 프로토콜은 회귀 하네스로 직접 재사용 가능 |
| **HippoRAG 2** (2502.14802, ICML 2025) | vector RAG를 3과제 전부에서 능가(+7% associative). 최대 기여 = passage를 KG 노드로 직접 통합(제거 시 Recall@5 74.7→63.7). query-to-triple 링킹이 NER 대비 +12.5% recall. 전부 로컬 가중치로 재현 가능 | 종전 graph-RAG(GraphRAG/LightRAG류)는 사실 회상이 vector RAG보다 한참 아래. 코퍼스 성장에 따른 검색 열화는 미해결 |
| **Engram** (2606.09900, 2026-06) | retain 핫패스 LLM 0회·<50ms, LLM 사실 추출은 비동기 consolidation으로. salience decay/reinforcement 망각. supersession 체인(provenance 보존). 검색 슬라이스 ~9.6k 토큰이 79k full-context보다 +10.4pp | 단일 벤치·단일 백본, 타 시스템 head-to-head 없음 — 설계 참조용 |

**Deneb 매핑**
- 다중 소스 recall(원문 검색 병행)은 유지 — MemoryAgentBench의 "추출 파이프라인 정보 손실" 경고에 대한 구조적 헷지. 이미 정합.
- **부재한 것은 망각/수정.** hindsight retain은 append-only. 현실적 1단계는 hindsight 서버 개조가 아니라 게이트웨이 측 규칙: dreaming이 이미 FactsExpired/FactsMerged를 추적하는 위키가 사실상의 consolidation 레이어이므로, recall evidence 머지에서 **충돌 시 위키(수정 반영) > hindsight(append-only)** 우선·최신성 규칙을 명시 (`recall_evidence.go`의 dedup/score 단계). 2단계로 hindsight 쪽 salience decay/supersession 기능 검토.
- wiki graph 추가 투자는 우선순위 낮음 — Mem0g +2%와 HippoRAG2의 "그래프가 사실 회상을 깎는다" 증거. Deneb hybrid(벡터 0.5 가중, 문서=노드+임베딩)는 HippoRAG2의 핵심 교훈(passage 통합)과 이미 같은 형태.
- `recall_bench_test.go`(현재 합성 코퍼스 hit rate)를 MemoryAgentBench식 **턴 단위 incremental injection + 사실 수정 케이스**로 확장하면 retain→recall 전 구간 회귀 하네스가 된다.

## 축 2 — 컨텍스트 관리

| 논문 | 핵심 결과 | 주의/한계 |
|---|---|---|
| **ACE** (2510.04618, 2025-10) | 컨텍스트=진화하는 플레이북: 증분 델타(불릿 ID + helpful/harmful 카운터, 비-LLM 머지). AppWorld +10.6%, 적응 latency -82~91%, 토큰 비용 -83.6%. **context collapse 실증**: 모놀리식 재작성 1스텝에 18,282→122토큰, 정확도 66.7→57.1(무적응 63.7 미만) | 실행 피드백 없으면 열화. 과제 따라 간결 지침이 더 나음(HotpotQA) |
| **Context-Folding** (2510.11967, 2025-10) | branch/return 2개 도구로 서브태스크 컨텍스트 분리·접기. KV-cache 친화(return 시 branch point로 롤백 — 캐시 재사용). 32K 활성 예산으로 327K ReAct 능가 | **이득 대부분이 RL 훈련에서 옴**(+20pp). 프롬프트-온리는 long-context ReAct에 못 미침 — 로컬 무훈련 도입의 핵심 캐비앗 |
| **AgentFold** (2510.24699, 2025-10) | per-step folding 지시(JSON). 100턴에 ~7k 토큰(ReAct ~91k). Qwen3-30B-A3B SFT만으로 BrowseComp 36.2%(671B·o4-mini 능가) | folding vs 고정 요약 통제 ablation 없음 |
| **FoldAct** (2512.22733, 2025-12) | folding RL 불안정의 원인 규명: 요약이 미래 관측에 들어가 관측 분포가 policy-dependent. full-context consistency loss로 안정화. 7B가 32B·GPT-4.1-mini 능가(WebWalker) | RL 훈련 전제 |
| **ACON** (2510.00615, ICML 2026) | **압축 가이드라인을 실패 trajectory 분석으로 자연어 공간에서 반복 정제 — 파인튜닝 불필요.** peak 토큰 -26~54% + 성공률 동시 개선. 작은 모델 +46%(context distraction 완화). 압축기를 소형 모델로 distill 가능 | — |
| **Prompt caching 실측** (2601.06007, 2026-01) | 멀티턴 에이전트 비용 -41~80%, TTFT -13~31%. **전략적 캐시 블록 제어(동적 콘텐츠는 끝에, 동적 tool 정의 회피, 동적 tool result 캐시 제외)가 naive full 캐싱보다 일관 우위** — naive는 latency 역효과 가능 | — |

**Deneb 매핑**
- prompt-cache 독트린은 2601.06007 권고와 사실상 동일 — 외부 실증 확보, 변경 불요.
- polaris는 append형 DAG + fence 보호로 ACE의 collapse 함정을 회피 중. **요약 품질 자체의 평가·개선 루프가 없는 것**이 갭: ACON 패턴 적용 — 압축 후 정보 누락으로 인한 재질문/오답 사례를 agentlog에서 수집 → summarizer 프롬프트(`compaction/llm.go`) 가이드라인 정제 → recall/quality metric으로 검증. lightweight role 모델로 압축기 distill까지 가능.
- ACE 델타 패턴의 적용처는 압축이 아니라 **누적 지식 문서**: topics/*.md, MEMORY.md, 위키 자동 병합. dreaming의 LLM 재작성 병합이 collapse 위험 지점 — "불릿 단위 append + ID + 유익/유해 카운터 + 주기 curator 압축" 구조 검토.
- branch/return folding은 컨텍스트 소진형 멀티스텝 미완료 문제의 직접 처방이지만 프롬프트-온리 한계 캐비앗이 큼 — 토큰 무거운 서브태스크(대량 검색·파일 스캔)에 한정한 실험 가치. KV 롤백 친화성은 trailing 마커 구조와 양립.

## 축 3 — Tool Use

| 논문 | 핵심 결과 | 주의/한계 |
|---|---|---|
| **RAG-MCP** (2505.03275, 2025-05) | 검색-우선 tool 선택 43.1% vs 전체 스키마 프롬프트 13.6%. 프롬프트 토큰 절반 | ~100개 초과 시 검색 정밀도 열화. 절대 정확도 <50% |
| **Tool-to-Agent Retrieval** (2511.01854, 2025-11) | tool+상위 agent 공동 임베딩, 라우팅 LLM 0회. Recall@5 0.83 (선행 +19.4%). MiniLM급 임베더로도 동작 | — |
| **MemTool** (2507.21428, 2025-07) | 턴 간 tool 정의 동적 추가/제거 프레임. **중형 모델은 자율 제거 효율 0-60%로 신뢰 불가** → 결정론적 제거 + 자율 추가 Hybrid 권장 | — |
| **ToolRet** (2503.01763, 2025-03) | 일반 IR 모델은 tool 검색에 약함(최강 nDCG@10 33.8). 전용 파인튜닝으로 개선 | 임베딩 기반 tool 검색 도입 시의 캐비앗 |
| **SoK: Agent Skills** (2602.20867, 2026-02) | metadata-driven progressive disclosure(메타데이터만 상주, 본문 lazy 로드)가 정석 패턴으로 공식화 | — |

**Deneb 매핑**
- 현행 deferral(이름 1줄 + fetch 지시, eager 14개)은 SoK의 progressive disclosure 그 자체 — 검증된 노선.
- **per-turn 임베딩 tool retrieval은 도입하지 않는 것이 결론.** 프롬프트 캐시 독트린 Rule B(대화 중 툴셋 불변, static 캐시 키 = 툴 목록)와 정면 충돌 — 논문들이 다루지 않는 우리 제약. 적용 가능한 형태는 (a) 세션 시작 시 1회 선택 고정, (b) agentlog 빈도 기반 deferral 심화(prompt diet R2 방법론 그대로), (c) MemTool Hybrid의 결정론적 제거를 세션 경계에서만.

## 축 4 — Adaptive Reasoning

| 논문 | 핵심 결과 | 주의/한계 |
|---|---|---|
| **Ares** (2603.07915, 2026-03) | per-step 학습 라우터(Qwen3-1.7B)가 히스토리에서 low/mid/high 예측. reasoning 토큰 -52.7%, 성공률 무손실(WebArena +1.5pp). 라벨 = "스텝 성공의 최소 effort"를 K=3 샘플링 + LLM 검증으로 자동 생성. static/random 베이스라인은 실패 | 라우터 latency 오버헤드 미보고 |
| **AutoThink** (HF blog, optillm) | 복잡도 분류 + 동적 thinking 예산. 1.5B에서 GPQA +9.3pp, 토큰 -15~25%, 분류 오버헤드 ~10ms | MMLU-Pro에선 +0.8pp(벤치 의존). steering vector는 activation 접근 필요(vLLM 표준 미노출) |
| **ASRR** (2505.15400, 2025-05) | "Internal Self-Recovery": thinking 억제해도 답변 생성 중 암묵 보완 — thinking-off가 쉬운 쿼리에서 안전한 기제적 근거. 토큰 -25~32% | 학습형(RL) — 로컬 적용은 파인튜닝 필요 |
| **Adaptive reasoning 서베이** (2511.10788, 2025-11) | **라우팅 신호는 가벼워야**(임베딩 수준 난이도 추정 권고 — LLM 자기평가는 비용이 이득 상쇄). 학습-프리 halting(엔트로피 임계·다수결 신뢰도)은 vLLM logprobs로 로컬 구현 가능 | — |
| **Test-time compute 서베이** (2507.02076, 2025-07) | L1 controllability / L2 adaptiveness 분류. reasoning 모델은 비추론 대비 최대 5x 길게 생성 | thinking budget 파라미터가 엄수되지 않는 long tail — budget API 신뢰 캐비앗 |

**Deneb 매핑**
- effort router 휴리스틱 v1의 다음 단계가 동명 논문에 그대로 있음: **agentlog o_t feed(#2274)에서 "최소 effort로 성공한 스텝" 라벨 마이닝 → 경량 분류기**(임베딩 + 로지스틱, 필요시 Qwen3-1.7B — DGX 여유). 라벨 생성 파이프라인(스텝 단위 effort 변주 재실행 비교)은 puppet seat로 재현 가능.
- 라우터 비용 원칙: 현행 O(1) 휴리스틱 → 임베딩 수준까지가 적정선. LLM 호출 라우터는 금지.
- ASRR의 self-recovery 기제는 dsv4 thinking:false 운용(쉬운 턴 비추론)의 사후 정당화.

## 축 5 — Propus / 자기개선 스킬

| 논문 | 핵심 결과 | 주의/한계 |
|---|---|---|
| **SoK: Agent Skills** (2602.20867, 2026-02) | SkillsBench: **인간 큐레이션 스킬 +16.2pp vs 자기생성 스킬 -1.3pp.** 도메인 의존(절차/규제 +40~50pp, SWE·수학 +4~6pp). 검증 없는 라이브러리 = "skill debt". 스킬 마켓 공급망 공격 사례(악성 스킬 ~1,200개) | — |
| **EvolveR** (2510.16079, 2025-10) | 경험→원리 distill + 검색이 RL 단독 대비 가산 이득(EM 0.325→0.382). self-distill > teacher distill(소형 모델). **store 45k에서 피크 후 하락 — 무한 축적은 열화.** 원리 품질 병목 = 모호함(저점수군의 50%) | 경험의 weight 내재화는 노이즈로 역효과 |
| **Self-Correction Blind Spot** (2507.02778, 2025-07) | 자기 출력 오류 64.5% 미수정(외부 제시 동일 오류는 수정). "Wait" 프롬프트로 blind spot -89.3% | 통제된 오류 주입 벤치 — 실전 일반화 캐비앗 |
| **역할 재라벨** (2606.05976, 2026-06) | byte-identical 오류를 own-thought → user/tool/memory 역할로 옮기면 명시 수정률 +23~93pp (7 모델 패밀리 × 3 도메인) | 최적 역할은 도메인 의존(수학=memory, 논리=user) |
| **AutoSkill** (2603.01145, 2026-03) | 스킬 lifecycle(추출→버전 병합→하이브리드 검색 주입) 설계 참조 | **정량 검증 전무** — 설계 참조로만 |

**Deneb 매핑**
- genesis 자동 생성 스킬은 SoK 데이터상 평균적으로 해로울 수 있음 → **검증 게이트 강화**가 핵심: 생성 시 구체성 기준(트리거·절차·반례) 체크, 모호 스킬 reject(EvolveR의 "모호함이 지배 실패모드"와 일치).
- **evolve self-test에 역할 재라벨 적용** (`genesis/evolver.go`): 검증 대상 스킬 본문·테스트 결과를 모델 자신의 출력이 아닌 user/tool 역할 콘텐츠로 제시 — 프롬프트 구성 변경만으로 수정률 대폭 상승 기대. 난이도 최하.
- 진화 후 실효 검증 부재 ↔ skill debt 경고 일치: 진화본 vs 이전본의 사용 성공률 비교(tracker 데이터 기존재)를 LogEvolve(#2271)에 추가 — A/B 폐쇄 루프.
- skillcurator(stale 30일/archive 90일)는 EvolveR의 무한 축적 열화 증거로 정당화됨 — 유지.

## 축 6 — 능동형 개입

| 논문 | 핵심 결과 | 주의/한계 |
|---|---|---|
| **ProactiveBench** (2410.12361, 2024-10) | **지배적 실패 = 과개입: 최고 모델도 오경보 ~50%** (GPT-4o 51.9%, Claude-3.5 54.6%). 파인튜닝 8B 판정기 91.8% F1 vs GPT-4o judge 67.0% — 로컬 판정기가 프런티어 judge 능가 | 시뮬레이션 환경 기반 |
| **ProAgentBench** (2602.04482, 2026-02) | 실사용 데이터 28k 이벤트. 타이밍 예측에 KG 장기기억이 최대 기여(+11.8%). **행동 컨텍스트는 ~5분에서 이득 평탄.** 실데이터 SFT 8B(74.0%)가 zero-shot 프런티어(64.4%) 능가. precision=interruption cost, recall=need coverage | — |
| **ProActor** (2605.24900, 2026-05) | 기회 시간창 자동 라벨링 + RL. PT/FTR/RAR 타이밍 메트릭 제안 | 훈련에 4xH200급 필요(DGX Spark 초과). 절대 PT 점수 낮음 |

**Deneb 매핑**
- 과개입이 지배 실패모드라는 결과는 contentless 억제 + noise floor 노선의 실증. 다음 단계는 **수락/거부 신호 수집**: workfeed 카드 dismiss/열람/액션, 푸시 무시를 agentlog 능동 퍼널(delivered/suppressed/dropped)에 이어 붙이면 판정기 학습 데이터가 자생. 단일 사용자라 표본은 작지만 페르소나 단일이라 적합도 높음.
- 능동 퍼널 리포트에 **FTR(과개입율) 채택** — 기존 agentlog 데이터로 계산 가능.
- 이벤트 인제스트(폰 알림 watcher) 판정 프롬프트의 행동 컨텍스트는 직전 ~5분 윈도우면 충분하다는 budget 가이드.
- 장기: 축적된 수락/거부 데이터로 로컬 8B 판정기 SFT(ProactiveBench 경로). RL(ProActor)은 인프라상 비현실.

## 축 7 — 에이전트 평가

| 논문 | 핵심 결과 | 주의/한계 |
|---|---|---|
| **LLM-judge 신뢰성 서베이** (2508.02994, 2025-08) | **artifact 검사형 에이전트 judge는 인간 다수결과 불일치 0.3% vs 단발 LLM judge 31%.** null-response 공격·길이·동족 편향 문서화. debate judging +10~16%(고비용) | judge-인간 일치율 자체가 순환적 — 인간 편향 복제 가능 |
| **Agent 평가 서베이** (2503.16416, 2025-03) | **백본 모델 vs 하네스의 회귀 귀속 분리**가 현재 벤치마크의 핵심 갭. 살아있는 벤치마크로 이동. 비용을 핵심 메트릭으로 통합해야 | — |
| **MemoryAgentBench** (재인용) | incremental injection 프로토콜 — retain/recall 회귀 하네스로 직접 재사용 가능 | — |

**Deneb 매핑**
- quality-metric.sh는 표면 점수(한국어 비율·길이·누출) — artifact-aware 채점(도구 결과 실재 확인, quality tools-deep의 확대)으로 신뢰도 상승 여지. 회귀 판정은 점수 절대값보다 fixed probe set의 pass/fail 천이로.
- 모델 교체가 잦은 운용(step3p7→dsv4)에서 "백본 vs 하네스" 분리 정책 부재 — modeltuner 캘리브레이션을 fixed probe suite로 확장해 모델 교체 시 자동 실행, 하네스 회귀와 모델 특성 변화를 분리.
- LLM-judge를 회귀 게이트로 쓸 경우 길이 정규화 + 타 패밀리 judge 등 편향 방어 명시.

---

## 우선순위 제안

| 순위 | 제안 | 근거 | 적용 지점 | 난이도 |
|---|---|---|---|---|
| P1-1 | evolve self-test 역할 재라벨 (자기 출력을 user/tool 역할로 제시) | 2606.05976, 2507.02778 | `genesis/evolver.go` 프롬프트 구성 | 하 |
| P1-2 | genesis 구체성 게이트 + 진화 전/후 성공률 A/B를 LogEvolve에 기록 | SoK 2602.20867, EvolveR | `genesis/genesis.go`, `evolver.go`, LogEvolve | 하~중 |
| P1-3 | 능동 퍼널에 FTR(과개입율) 메트릭 + workfeed 카드 수락/거부 신호 수집 | ProactiveBench, ProAgentBench | `proactive_relay.go`, workfeed, agentlog | 중 |
| P1-4 | recall 회귀 하네스를 incremental-injection + 사실수정 케이스로 확장 | MemoryAgentBench 프로토콜 | `recall_bench_test.go`, recall-metric.sh | 중 |
| P2-5 | ACON식 polaris summarizer 가이드라인 자기정제 루프 | ACON 2510.00615 | `compaction/llm.go` + agentlog 실패 사례 | 중 |
| P2-6 | effort router 학습형 1단계: o_t feed 라벨 마이닝 → 임베딩 분류기 | Ares 2603.07915, 2511.10788 | `effort_router.go`, agentlog | 중~상 |
| P2-7 | recall evidence 충돌 시 위키(수정 반영) 우선·최신성 규칙 | Zep supersession, MemoryAgentBench | `recall_evidence.go` 머지 정책 | 중 |
| P2-8 | 모델 교체 시 fixed probe suite 자동 실행(백본 vs 하네스 분리) | 2503.16416 | modeltuner 캘리브레이션 확장 | 중 |
| P3-9 | branch/return context folding 실험(무거운 서브태스크 한정) | 2510.11967 (프롬프트-온리 한계 유의) | agent loop + 신규 도구 2개 | 상 |
| P3-10 | hindsight salience decay/supersession | Engram, Zep | hindsight 서비스 측 기능 검토 | 상 |
| P3-11 | 로컬 능동 판정기 SFT (신호 데이터 축적 후) | ProactiveBench | 별도 사이드카 | 상 |
| — | per-turn tool retrieval은 **도입하지 않음** (캐시 독트린 충돌) | RAG-MCP/MemTool vs prompt-cache Rule B | 현행 deferral 노선 유지 | — |

## 이미 논문 방향과 정합인 Deneb 설계 (변경 불요, 근거 확보)

- **prompt cache 독트린** ↔ 2601.06007의 전략적 캐시 블록 제어 권고와 일치 (동적 콘텐츠 후미 배치, 동적 tool result 캐시 제외).
- **recall 다중 소스(원문 검색 병행)** ↔ MemoryAgentBench의 추출-저장 정보 손실 경고에 대한 헷지.
- **polaris append형 요약 DAG + fence 보호** ↔ ACE의 context collapse 회피 설계.
- **도구 deferral(이름 1줄 + fetch 지시)** ↔ SoK의 metadata-driven progressive disclosure 정석 패턴.
- **능동 noise floor(contentless 억제)** ↔ 과개입이 지배 실패모드라는 벤치 결과.
- **skillcurator(stale/archive)** ↔ EvolveR의 무한 축적 열화 증거.
- **effort router 자동화 가드(cron 등은 thinking 유지)** ↔ 라우터 오판 비용의 비대칭 원칙.
- **hindsight retain 비동기 fire-and-forget** ↔ Engram의 "핫패스에 LLM 금지" 원칙과 동형.

## 방법론·신뢰도

- deep-research 워크플로: 6각도 분해, 31개 1차 소스(arXiv 30 + HF blog 1), 155개 주장 추출, 상위 25개 적대 검증 시도. API 세션 한도로 검증 64건 실패 — 확정 4건(Zep DMR, Graphiti bi-temporal, Mem0 +26%, Mem0g +2%), 1-0 2건, 나머지는 출처 직접 인용 기반의 미검증 주장.
- 수치는 대부분 저자 self-report. 2026년 arXiv ID(26xx.xxxxx)는 프리프린트로 피어리뷰 전일 수 있음. 반박 증거가 있는 주장(프롬프트-온리 folding 한계, 그래프 메모리 한계효용, budget API 미엄수 등)은 본문에 캐비앗으로 병기.
- 코드 현황은 2026-06-13 main 기준 탐사 결과 (recall preflight 병렬 1.5s, polaris bounded digestion, effort router v1 휴리스틱, genesis/evolve/curator, contentless 억제, agentlog 능동 퍼널).
