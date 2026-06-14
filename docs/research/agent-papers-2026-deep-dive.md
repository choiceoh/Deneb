---
title: "Agent Papers 2026 Deep Dive"
summary: "Six core findings from the 2026 agent-papers survey, deep-dived: verified paper mechanics, ground-truth code state, and right-sized implementation designs."
read_when:
  - "Implementing any of the survey's P1-P3 proposals (recall, compaction, skills, effort router, proactive)"
  - "Checking which paper claims were verified against the original text vs. abstract-only"
  - "Understanding why several first-pass survey conclusions were corrected"
sidebarTitle: "Papers Deep Dive"
---

# Agent Papers 2026 Deep Dive

> 2026-06-13. [Agent Papers 2026 Survey](/research/agent-papers-2026-survey)의 6개 핵심 발견을 세부 탐색한 결과. 4개 병렬 조사(논문 원문 fetch + 코드 정밀 독해) 후 상충 보고는 직접 코드로 재검증했다. **1차 서베이의 결론 중 3건이 이 과정에서 정정**되었다 (아래 "정정 사항").

## 검증 상태 요약

| 논문 | 원문 세부 확인 | 비고 |
|---|---|---|
| ACE (2510.04618) | ✓ 본문 인용 확보 | collapse 사례·델타 스키마·grow-and-refine·적용 경계 모두 원문 확인 |
| ACON (2510.00615) | ✓ 본문 인용 확보 | 최적화 루프 절차·AppWorld 수치(Easy peak -55.6%, Hard 정확도 39.7→44.6)·distill 절차 확인 |
| 역할 재라벨 (2606.05976) | ✓ 실험 설계 확인 | 13 모델-도메인 셀, 역할 조건 4종, +23~93pp, 도메인별 최적 역할 |
| Zep·Mem0 핵심 4건 | ✓ 1차 적대검증 만장일치 | DMR 94.8%, bi-temporal 무효화, LOCOMO +26%, 그래프 +2% |
| **MemoryAgentBench·Engram·Zep·HippoRAG2** | **✓ PDF 정독 (2026-06-13)** | 4역량·FactCon 표·cheap-then-escalate 3단·edge invalidation·ablation 확인 — ↓ "PDF 정독 검증" 절 |
| **SoK·EvolveR·Blind Spot** | **✓ PDF 정독** | -1.3pp·45k 피크·vagueness 50%·"Wait" -89.3%·reasoning 모델 blind spot ~0 확인 |
| **Ares·adaptive 서베이** | **✓ PDF 정독** | ★라벨 파이프라인이 검증가능 성공+리플레이 전제 → **개인 비서 직접 적용 불가** 판명 |
| **ProactiveBench·ProAgentBench·ProActor** | **✓ PDF 정독** | ProAgentBench 5분 윈도·실데이터 SFT 확인. ★ProactiveBench의 "~50% 오경보" 구체수치는 **원문 미발견**(1차 추출 오류 의심) |

> **PDF 정독 결과 1차 추출 5건이 정정됨** — 아래 "PDF 정독 검증" 절. 가장 중요한 건 **Ares 직접 적용 불가**(발견 5의 제안을 하향), **MemoryAgentBench 상용메모리 수치가 1차보다 더 나쁨**, **reasoning 모델은 blind spot ~0**(발견 3·4의 근거를 역할→모델로 재귀속).

## 정정 사항 (1차 서베이 대비)

1. **위키 supersession은 이미 구현·배선되어 있다.** dreamer가 모순 감지 시 `Supersedes` 필드를 채우고 `dreamer_apply.go:325`가 `MarkSuperseded`를 호출 → frontmatter `SupersededBy` + 검색 0.5x 감가(`search.go:validityFactor`) + 나이 감가(1년 0.7x). 탐사 에이전트 간 상충 보고를 코드로 재확인했다. 남은 갭은 "recall evidence에 superseded 표식이 안 실리는 것"과 "hindsight로 무효화가 전파되지 않는 것"뿐.
2. **evolver self-test는 이미 역할 재라벨 논문의 '좋은 쪽' 설계다.** 후보 스킬을 별도 LLM 호출의 **user 역할 메시지**로 제시한다 (`evolver.go:442`). 블라인드스팟(자기 thought/assistant 역할 내 오류 미수정)은 같은 대화 안에서 자기 출력을 재검토시킬 때의 문제 — Deneb의 자기검증 루프(evolver judge·compaction summarizer·dreaming synthesis)는 모두 별도 호출 + 외부 역할 구조라 해당 없음. **진짜 갭은 판정 모델 = 생성 모델(자기-패밀리 편향, 2508.02994)**: lightweight가 만든 후보를 같은 lightweight가 판정하고(`evolver.go:387`), teacher 재작성본은 teacher 자신이 판정한다(`evolver.go:410`).
3. **dreaming의 위키 update는 append-only다** (`existing.Body += "\n\n" + content`) — ACE가 경고한 모놀리식 재작성형이 아니므로 collapse 위험 평가를 "안전"으로 정정. 남은 위험은 무한 append로 인한 페이지 비대 + 큐레이션 부재.
4. **recall evidence에는 이미 행마다 `age=Nd` 표기가 실린다** (`recall_evidence.go:formatRecallAge`) — "모델에 시간 정보 제공" 제안은 이미 충족. 좁혀진 제안: superseded 마커만 추가.

---

## 발견 1 — 장기기억: 정보 손실 헷지와 망각 공백

### 코드 ground truth

- recall 4소스(wiki/diary/session/hindsight) 병렬, 공유 1.5s 데드라인. 스코어: wiki `0.80+BM25`(최대 1.80), hindsight `0.92-0.05×rank`(0.60 바닥), diary 0.55, session 0.58 (`recall_preflight.go`, `recall_hindsight.go`).
- dedup은 정규화 내용 키(80 rune) 기준 점수 승자 — **같은 사실의 중복**만 잡는다. **다른 값의 충돌**(담당자 김민준 vs 박수진)은 내용 키가 달라 dedup을 통과 → 둘 다 노출 → 모델이 `age=` 표기로 판단. 이 구조는 유지가 맞다 (서버가 충돌을 자동 중재하면 위키가 stale한 케이스에서 오답을 강제한다).
- retain 페이로드: `User+Assistant 합본(6000자 캡) + metadata{source, retained_at=now}` (`hindsight_recorder.go`) — 원 발화 시각 메타 없음.
- 위키 supersession 파이프라인 존재 (정정 사항 1). 단 superseded 페이지가 recall에 잡혀도 evidence 노트에 그 사실이 표기되지 않는다.
- 회귀 하네스(`recall_bench_test.go`): 정적 코퍼스 8케이스, floor 80%. 사실 수정/망각 케이스 0.

### 설계

**1A. superseded 마커 (소, 즉효).** `recallWikiEvidence`가 페이지 메타의 `SupersededBy`를 보고 노트 앞에 `[대체됨 → <new path>]`를 붙인다. 모델이 구사실을 인용하는 사고를 recall 단에서 차단. 변경: recall_wiki 경로 1곳 + 단위 테스트.

**1B. retain 발생 시각 메타 (소).** retain metadata에 turn timestamp(`occurred_at`)를 추가해 hindsight의 시간 해석(임베딩 증강 `(happened in Month Year)`)이 retained_at이 아닌 실제 발화 시각을 쓰도록. hindsight 서버가 이 메타를 소비하는지 확인 필요 — 소비하지 않으면 retain content 말미에 `[발생: YYYY-MM-DD]` 주석으로 폴백.

**1C. 무효화 전파 (중).** dreamer가 `MarkSuperseded`를 수행한 사이클 종료 시 hindsight에 1건 retain: "사실 갱신: <old> 내용은 <new>로 대체됨 (YYYY-MM-DD)". hindsight를 고치지 않고 검색 공간 안에 정정 사실 자체를 심는 접근. 부작용 낮음, 효과는 hindsight 하이브리드 검색의 랭킹에 의존 — 1A보다 후순위.

**1D. 회귀 하네스 확장 (중).** `recall_bench_test.go`에 턴 단위 시나리오 추가: ①위키 주입 → 질의(구사실 적중) ②diary 주입(사실 변경) → 질의(신사실 적중) ③`MarkSuperseded` 시뮬레이션 → 질의(신사실 + `[대체됨]` 마커, 구사실 비인용) — `mustFind`/`mustNotFind` 어서션, floor 75%, `recall-metric.sh`에 두 번째 메트릭 라인 추가. MemoryAgentBench의 incremental-injection 정신의 단일 사용자 축소판.

## 발견 2 — 컨텍스트: collapse 회피 확정, ACON 경량 루프

### 논문 세부 (원문 확인 ✓)

- ACE: 델타 단위 = `bullet{id, helpful_count, harmful_count, content}`, 머지는 비-LLM 결정론, dedup은 임베딩 비교, pruning은 컨텍스트 초과 시 lazy. collapse 발생 조건 = **모놀리식 전체 재작성** (스텝 60에서 18,282→122토큰, 66.7→57.1). 적용 경계: 상세 도메인 지식·복잡 tool use 환경에 유익, HotpotQA류 single-hop엔 간결 지침이 우위.
- ACON: 루프 = 실패쌍(비압축 성공 vs 압축 실패) 비교 → LLM이 "무엇이 누락됐나" 피드백 → 가이드라인 후보 생성 → 검증 후 채택. AppWorld Hard에서 압축이 정확도를 **올림**(39.7→44.6, context distraction 완화). 압축기 distill로 비용 절감.

### 코드 ground truth

- polaris summarizer(`compaction/llm.go`)는 이미 정교함: 한국어 4섹션 고정 출력(Facts/Open Loops/Uncertain/Tool Outcomes) + **"이전 요약을 갱신하라" 증분 프롬프트**(재작성 아닌 갱신) + **anchor keywords 인프라**(`SetAnchorKeywords` — 위키 Tier1 제목의 사실 보존 강제) + fence 분리·budget 재배분(`polaris/engine.go:86-107`). ACE collapse 관점에서 안전.
- dreaming 위키 병합 = append-only + supersedes (정정 사항 1·3). 갭: 비대 페이지 큐레이션 부재 (30KB+ 페이지가 누적 가능; 중복 "페이지" 병합 패스 `MergePage`는 있으나 페이지 "내부" 정리는 없음).
- MEMORY.md(워크스페이스): 32K 캡, 자동 누적, 압축 메커니즘 없음 (`prompt/context_files.go`) — 비대화 1순위 문서.
- "압축 후 정보 부족" 신호: agentlog에 직접 필드는 없으나 프록시 가능 — 압축 발생 run(`Compacted` 기록됨) 직후 N턴 내 과거 조회성 도구 호출(transcript 검색, read_spillover) 발생 여부.

### 설계

**2A. anchor 자동 보강 (소 — ACON 루프의 1단계).** 주기 작업이 "압축 발생 후 과거 재조회" 프록시 신호를 모아, 부족했던 주제 키워드를 `SetAnchorKeywords` 목록에 추가/교체. 기존 인프라 재사용이라 코드 변경 최소 — ACON의 "실패에서 가이드라인 정제"를 가장 싼 형태로 구현.

**2B. 가이드라인 문서화 + 정제 루프 (중).** `compactionSystemPrompt`의 보존 규칙 부분을 별도 가이드라인 파일로 분리(프롬프트에 주입), 자율작업이 실패 사례 K건 누적 시 lightweight 모델로 개정안 생성 → recall-metric/quality 전후 비교 후 채택. 채택 게이트 없이 자동 적용 금지(ACE의 검증 게이트 정신). 주의: 가이드라인은 Static 프롬프트가 아닌 압축 호출 전용이라 프롬프트 캐시 무영향.

**2C. 비대 문서 큐레이션 (중).** 대상: MEMORY.md(32K 근접 시), 위키 30KB+ 페이지. dreaming 사이클에 "통합 정리" 단계 추가 — 원본은 wiki gitsnap이 이미 커밋하므로 롤백 무료. 출력 검증: 정리 후 본문이 원본 대비 일정 비율 미만으로 줄면(예: 5% 미만 — collapse 신호) 적용 거부. ACE의 helpful/harmful 카운터 풀 구현은 과설계로 판단 — 보류.

## 발견 3·4 — 자기생성 스킬 게이트와 셀프 크리틱 (통합)

### 코드 ground truth

- genesis 게이트 = dedup(Jaccard 0.82) + 쿨다운 24h + 일일 캡 10 — **품질(구체성) 게이트 없음** (`genesis.go`).
- evolver self-test: fail-closed 판정 프롬프트(5기준, 불확실 시 reject)는 우수. 구조적 갭 = 판정자·생성자 동일 모델 (정정 사항 2). 진화 기록(`LogEvolve`/`LogEvolveRejected`)은 있으나 **전후 성공률 delta 추적·롤백 없음**, 스킬 파일 버전 백업 없음 (위키와 달리 git 스냅샷 부재).
- tracker가 스킬별 success/failure를 이미 기록 → A/B 원료는 존재.

### 설계

**3A. judge 교차 분리 (최소, 즉효).** ①lightweight 후보의 1차 판정을 teacher(main)로 교체 — 6시간 주기 작업이라 토큰 비용 무시 가능 ②teacher 재작성본(`evolver.go:410`)은 lightweight 또는 main-이외 모델이 판정 (자기 재작성 자기 판정 제거). 변경: `selfTestAndMaybeEscalate`의 client/model 인자 2곳.

**3B. 구체성 게이트 (소).** 생성 직후 휴리스틱 검사(필수 섹션 4종 존재·단계 명시·도구 명시·500자+) → 실패 시에만 LLM 재심(하이브리드). genesis 프롬프트에 reject 기준 명문화(모호 지시 예시 ❌/✓). EvolveR의 "모호함이 지배 실패모드" 직접 대응.

**3C. 진화 A/B + 롤백 (중).** 커밋 시 `EvolutionRecord{skill, fromVer, toVer, preRate}` 기록 + 이전 본문을 `.backups/`에 보관. 이후 사용 기록마다 검사 — 단일 사용자라 표본 희소하므로 통계 기준 대신 단순 룰: **진화 후 신규 사용 3회 연속 실패 시 자동 롤백** + LogEvolve에 rollback 사유 기록. (20pp 하락 기준은 표본 모이는 데 수 주 걸려 비현실적.)

**3D. 원칙 명문화 (문서).** "자기검증은 반드시 별도 호출 + 외부 역할(user/tool) 제시 + 가능하면 타 모델"을 collaboration 규칙에 1줄 추가 — 향후 배달 전 self-check 류 기능이 같은 대화 안 자기 재검토로 구현되는 것을 예방. 역할 재라벨 논문의 도메인별 최적 역할(수학=memory, 논리=user)은 효과 대비 복잡도가 높아 도입 보류.

## 발견 5 — 학습형 effort 라우터

### 코드 ground truth

- 휴리스틱 v1 전모: 자동화/첨부/60자+/하드신호 26종 → 유지, pure-ack 예외; 스텝 단위는 `turn 0 무조건 off → turn 1-2 o_t 크기(단건 2000/누적 8000 rune)·에러로 복귀 → turn 3+ 강제 복귀` (`effort_router.go`).
- o_t feed: `ToolActivity{Name, Turn, OutputRunes, IsError}` run-scoped (#2274). agentlog `RunEnd`에 `EffortDecision`("routed:short-conversational"/"kept:...") + `EffortEscalated` 기록 — **라벨 마이닝 원료는 이미 쌓이는 중**.
- 성공 판정 프록시: `StopReason==end_turn ∧ 무에러 ∧ output>0`로 구성 가능. "다음 턴 사용자 정정 없음" 신호는 미기록.

### 설계 (단일 사용자 보정)

**5A. 재캘리브레이션 (소, 1순위).** 분석 스크립트 1개: 최근 30일 agentlog에서 2×2(routed/kept × 성공/실패) + o_t 크기 구간별·턴별 성공률 분포 → 상수 4종(2000/8000/1500/턴 한도 3) 데이터 근거로 조정. 학습 없음, 코드 변경은 상수뿐.

**5B. 학습형 전환 — 섀도 모드 (중).** 단일 사용자 현실: 라벨 ~1-2K/월 → 구조화 피처 15종(턴 번호·메시지 길이·o_t max/sum·에러 수·하드신호·자동화·첨부 등) + 로지스틱 회귀부터 (임베딩 피처는 2차). 멀티유저 카나리 불가 → **섀도 모드**: learned 라우터를 병행 실행하되 결정만 로그(적용은 휴리스틱), 불일치 사례를 주간 리뷰 → 합의율·예상 토큰 절감 확인 후 컷오버. `effort_router.go`에 결정 인터페이스 추상화 + `DENEB_ADAPTIVE_EFFORT=shadow|learned`. 라우터 추론은 임베딩 없이 <1ms (서베이 2511.10788의 "가벼운 신호" 원칙 준수).
- 검증: puppet seat로 동일 턴 thinking on/off 재실행 비교 가능 — Ares의 라벨 생성 파이프라인(K샘플링)의 수동 축소판.

## 발견 6 — 능동형: FTR 계측과 수락/거부 신호

### 코드 ground truth

- 능동 퍼널 기록은 이미 충실: `ProactiveRelayData{Decision: delivered|suppressed|dropped|error, Reason, ContentLen, Preview}` (`proactive_relay.go:290-297`).
- 결손: 배달 **이후** 사용자 반응. workfeed RPC는 `list/ack/action.run(open|followup|snooze|ack)`뿐 — 열람 타임스탬프 없음, dismiss 별도 메서드 없음, nativesync는 서버→클라 단방향(`TypeWorkFeedUpdated`만).

### 설계

**6A. 반응 신호 배선 (중).** ①`miniapp.workfeed.read`(열람)·`miniapp.workfeed.dismiss`(reason: later|not_relevant|done) RPC 신설 ②`workfeed.Item`에 `ReadAtMs/DismissedAtMs/DismissReason` 필드 (+`//deneb:wire` → `make kotlin-models`) ③agentlog `ftr.signal{deliveredRunId, itemId, decision: read|dismissed|timeout, timeSinceDeliveryMs}` ④무반응 폴백: 배달 24h 후 timeout 신호 자동 기록.

**6B. FTR 집계 (소, 6A 의존).** FTR = (dismissed+timeout)/delivered, 주간·잡별 집계 → 능동 퍼널 리포트에 추가. ProAgentBench 해석(precision=interruption cost) 그대로: FTR 높은 능동 잡이 contentless 임계 강화 대상.

**6C. 임계 자동 조정 → 로컬 판정기 (장기).** FTR 데이터가 수개월 쌓인 후: not_relevant 사유 dismiss가 많은 카드 패턴을 contentless 사전에 반영(수동) → 충분하면 로컬 8B 판정기 SFT는 그때 재평가. ProactiveBench의 91.8% F1은 1,640 라벨 기반 — 단일 사용자로는 1년치 신호에 해당하므로 조급히 가지 않는다.

---

## 갱신된 우선순위 (세부 탐색 반영)

| 순위 | 제안 | 변경 사유 | 난이도 |
|---|---|---|---|
| P1-1 | **5A 라우터 재캘리브레이션** (agentlog 마이닝 → 상수 조정) | 데이터 이미 존재, 스크립트 1개 | 하 |
| P1-2 | **1A superseded 마커** + 1D 회귀 하네스(사실수정 케이스) | age 표기는 기존재로 확인 — 마커만 남음 | 하 / 중 |
| P1-3 | **3A judge 교차 분리** + 3B 구체성 게이트 | 역할 재라벨은 기충족(정정) — 모델 분리가 진짜 갭 | 하 |
| P1-4 | **6A 반응 신호 RPC + ftr.signal** | FTR의 선행 조건, 네이티브 변경 포함 | 중 |
| P2-5 | 2A anchor 자동 보강 (ACON 1단계) | 기존 인프라(SetAnchorKeywords) 재사용 | 중 |
| P2-6 | 3C 진화 A/B+롤백 (3회 연속 실패 룰) | 통계 기준 → 단순 룰로 현실화 | 중 |
| P2-7 | 5B 학습형 라우터 섀도 모드 | 카나리 → 섀도로 단일 사용자 보정 | 중~상 |
| P2-8 | 2C 비대 문서 큐레이션 (MEMORY.md·위키 30KB+) | collapse 가드(축소율 하한) 필수 | 중 |
| P3 | 1C hindsight 무효화 전파, 2B 가이드라인 정제 루프, 6C 로컬 판정기 | 효과가 선행 데이터에 의존 | 상 |

---

## PDF 정독 검증 (2026-06-13, 12편 원문)

> abstract만 봤던 논문들의 PDF를 받아 표·인용까지 정독한 결과. 메인이 직접 읽은 4편(Ares 2603.07915 15쪽, adaptive 서베이 2511.10788, MemoryAgentBench 2507.05257 17쪽 — 정독 에이전트와 교차) + 정독 에이전트 8편. **1차 추출 5건 정정.**

### 정정 1 — MemoryAgentBench 상용메모리 수치가 1차보다 더 나쁘다

1차 보고는 "Mem0 ~32.6, Zep ~37.5"였으나 **Table 3 원문은 Accurate Retrieval(SH-DocQA)에서 Mem0 15.0 / Cognee 5.0 / Zep 2.5 vs GPT-4o-mini long-context 60.0**. 추출-저장 메모리가 단순 long-context 대비 사실 회상에서 **훨씬 더** 떨어진다 — Deneb의 "원문 검색 병행" 헷지 정당성은 1차보다 강해졌다. 4역량 명칭 확정: Accurate Retrieval / Test-Time Learning / Long-Range Understanding / **Selective Forgetting**(모순 시 수정·삭제). FactConsolidation: single-hop은 GPT-4o-mini 100%지만 **multi-hop이 32K 컨텍스트에서 80%→14%로 붕괴** — "≤7%"는 일부 상용 시스템 셀이고, 핵심 메시지는 "장문맥에서 사실수정이 무너진다". 프로토콜: 2,071문항, 512/4096토큰 incremental injection, GPT-4o-mini 채점 — 회귀 하네스(1D) 복제 사양 확정.

### 정정 2 — Ares 라벨 파이프라인은 개인 비서에 직접 적용 불가 (발견 5 하향)

Ares 원문 정독으로 라벨 생성의 전제가 드러났다: **①검증 가능한 task 성공**(TAU-Bench DB 상태, WebArena 완료) **②동일 스텝을 effort별 K=3회 리플레이** **③가장 간결한 성공 trajectory를 ground-truth 액션 기준으로** 삼아 행동 등가(tool명+핵심 인자 일치, web click[id] 일치) 판정. 라벨 = "M/K 다수결로 정답 재현하는 최소 effort", 재현 실패 스텝은 **폐기**. 라우터 자체는 **Qwen3-1.7B LLM**(rationale 3-5문장 + 라벨 생성, SFT lr 5e-6/3ep) — 경량 분류기가 아니라 **두 번째 모델 호출**.

→ **개인 비서는 ①②③ 셋 다 없다**: 성공이 퍼지(사용자 무불평≠성공), 실제 사용자 턴을 effort별 리플레이+행동등가 판정 불가(최종 액션이 자연어 메시지), task pool 부재(개방형 스트림). 따라서 **Ares의 라벨 파이프라인은 라이브 트래픽에 이식 불가**. 이식 가능한 것은 (a) 5A 재캘리브레이션(프록시 성공 분포로 휴리스틱 상수 튜닝 — 유효) (b) **puppet seat로 curated eval set의 특정 턴을 thinking on/off 리플레이**해 소규모 검증 라벨 생성(Ares 라벨링의 수동 축소판) — 단 라이브 자동 마이닝이 아니라 큐레이션 셋 한정. 5B "라이브 agentlog 라벨 마이닝→분류기"는 **프록시 라벨이 noisy**(검증된 Ares 라벨과 달리)임을 명시하고 "휴리스틱의 점진 정제"로 격하. 단일 머신에 1.7B 라우터 상주는 unified memory·latency 경합이라 비채택 — 서베이(2511.10788)의 "라우팅 신호는 가벼워야" 원칙과도 합치.

**확정 수치**: 토큰 절감 body 기준 TAU-Retail -35.2% / BrowseComp-Plus -41.8% / WebArena -45.3%(T_total), 성공률 High 베이스라인 동급(Retail 54.8=54.8, WebArena +1.5, BrowseComp -1.4). abstract의 "최대 52.7%"는 단일 구성 최대치(평균 아님). RL 변형은 Retail 58.5%(+3.7). static-low·random 베이스라인 실패 확인. ★**Figure 1이 intra-model effort의 이점으로 "KV 캐시 재사용"을 명시** — Deneb의 thinking on/off(모델 스왑 아님)가 프롬프트 캐시 보존하는 올바른 선택임을 외부 검증.

### 정정 3 — reasoning 모델은 self-correction blind spot이 ~0 (발견 3·4 재귀속)

Blind Spot 원문(2507.02778): 정의 = `1 − P(정정|사용자제시오류)/P(정정|자기오류)`, 비추론 14모델 평균 64.5%. **"Wait"는 모델이 오류를 낸 직후 덧붙이는 토큰**(강제 디코딩 아님)이고 -89.3%는 상대 감소율. ★**reasoning 모델은 blind spot이 거의 0 또는 음수**(self-correction 능력 강함). → Deneb 메인(dsv4-thinking)은 reasoning 모델이라 **자기검증 blind spot 무관**.

3A(judge 분리)의 **주근거는 동족 자기선호 편향**(LLM-judge 서베이 2508.02994, 크기·reasoning 무관): evolver에서 lightweight가 후보를 **생성**(`evolver.go:133`)하고 **같은 모델이 판정**(`evolver.go:387` = `resolveModel()`) → 수용 편향. blind spot은 **부근거(조건부)**: lightweight(Qwen3.6-35B-A3B)는 thinking 가능 모델이라 evolve 경로에서 **thinking을 끄고 돌 때만** 적용(현 코드 Thinking 미설정 → 모델 기본값 의존, 미검증). 역할 재라벨(2606.05976)은 보조(우리 self-test는 이미 별도 호출+user 역할이라 1차 적용분 충족). **핵심: 생성자가 자기 출력을 판정하지 않도록 분리**하면 되고, teacher(main) 승격 vs 단순 다른 모델/세션 분리는 별개 선택지 — 동족 편향만 깨면 후자로도 충분.

### 정정 4 — EvolveR 스케일 결과는 3A를 정당화하지 못한다 (자기수정: lightweight는 소형이 아님)

EvolveR Table 7: principle store 성능 **45k에서 피크 0.410 → 49.9k에서 0.387 하락**(무한 축적 열화 확인, skillcurator 정당). vagueness = 저점수군의 50%, 정의 "맞지만 실행불가(예: '맥락을 주의 깊게 읽어라')" — **구체성 게이트(3B) reject 기준 직수입**: 모호 명령문·실행단계 부재·도메인 예제 부재. (이 두 발견은 유효.)

★**자기수정 (2026-06-13)**: 초판은 "self-distill(0.382)>teacher(0.370)은 3B+에서만이므로 Deneb lightweight는 소형이라 teacher 판정이 정당"이라 썼는데 **틀렸다**. 실제 `lightweightModel = custom/qwen3.6-35b-a3b` (Qwen3.6-**35B**-A3B, 활성 3B MoE) — 소형이 아니라 능력상 dense ~14–32B급. EvolveR의 교차점 기제는 param 수가 아니라 **capability("cognitive alignment")**를 따르므로 lightweight는 3B 교차점 **위** → EvolveR를 제대로 적용하면 오히려 **self-distill로 충분**하다는 쪽이다(정반대 인용이었음). 게다가 EvolveR는 *distillation* 결과인데 evolve self-test는 *판정* 작업이라 애초에 느슨한 유비. **결론: EvolveR 스케일 결과를 3A 근거에서 폐기.** 3A(판정자≠생성자)는 크기-무관한 근거 위에 선다 — ↓ 정정 반영 참조.

### 정정 5 — ProactiveBench "~50% 오경보" 구체수치는 원문 미발견

정독 에이전트가 ProactiveBench(2410.12361) 본문에서 "GPT-4o 51.85% false alarm" 등 **구체 수치를 찾지 못함** — 1차 추출이 타 출처를 섞었거나 환각. **검증된 것**: 보상모델(LLaMA-3.1-8B) 인간일치 91.80% F1, 학습 6,790 이벤트(coding 46/writing 46/daily 44 시나리오), 라벨=제안 **수락/거부**. "과개입이 지배 실패"라는 프레이밍은 ProAgentBench의 precision=interruption-cost 해석에 더 단단히 기댄다. **ProAgentBench(2602.04482) 확정**: 28,528 이벤트·B=0.787, 행동 컨텍스트 **5분에서 효과 균형**(10초~10분 테스트, 6A 윈도 사양 확정), 실데이터 SFT 8B 74.0% vs 합성 62.1%, KG 메모리가 타이밍 예측 기여. **ProActor FTR 정의**: `|reference range 밖 ready 액션| / |예측 ready 액션 총|` — Deneb FTR(배달 후 dismiss/무반응 / 배달 총)과 **분모가 다르다**(예측 ready vs 실제 배달). 의미는 정합하나 약어 충돌 방지 위해 본문/주석에 "(배달 기준)" 명기 권장. Engram/Zep supersession: cheap-then-escalate 3단(슬롯 정확매칭 → 임베딩 유사도 → LLM 판정)·`old.invalid_at ← new.valid_at` + `supersedes` 포인터, 모순 감지는 **LLM이 의미적으로 관련된 기존 엣지와 비교**(순수 룰 아님) 확정 — 1C 무효화 전파 설계의 모델.

### 정정 반영 — 갱신 P1/P2 미세조정

- **5A 재캘리브레이션**: 유효 유지 (1순위). **5B 학습형**: "라이브 라벨 마이닝→분류기"에서 "**큐레이션 eval set + puppet 리플레이 라벨 + 섀도 모드**"로 변경, 프록시 라벨 noisy 경고 명시, 1.7B LLM 라우터 비채택.
- **3A judge 분리**: 근거를 "역할 재라벨"·"EvolveR 스케일"에서 **"동족 자기선호 편향(생성자≠판정자)"**으로 교체 — 크기-무관. lightweight=Qwen3.6-35B-A3B(소형 아님)라 EvolveR 3B 교차점 논리는 폐기. 구현은 teacher 승격 또는 판정만 다른 모델/세션 분리 중 택1.
- **3B 구체성 게이트**: reject 기준에 EvolveR vagueness 정의("맞지만 실행불가") 직수입.
- **1D 회귀 하네스**: MemoryAgentBench 프로토콜(incremental injection, single-hop/multi-hop 분리, 32K에서 사실수정 붕괴 케이스) 사양 확정.
- **6A FTR**: ProActor와 분모가 달라 "(배달 기준) FTR"로 명명, 5분 행동 윈도 확정.

---

## 구현 상태 (2026-06-13, 브랜치 claude/cool-kirch-8dc190)

| 항목 | 상태 | 커밋 |
|---|---|---|
| 1A recall superseded 마커 | ✅ 구현 | `feat(recall): mark superseded/archived wiki pages` |
| 1D 사실수정 회귀 케이스 | ✅ 구현 | `test(recall): fact-revision supersession` |
| 3B genesis 구체성 게이트 | ✅ 구현 | `feat(genesis): reject vague self-generated skills` |
| 3A evolver judge 분리 | ✅ 구현 | `fix(genesis): judge with a different model` |
| P2-6 진화 자동 롤백 | ✅ 구현 | `feat(genesis): auto-rollback evolutions that regress` |
| P1-1 라우터 effort 스코어카드 | ✅ 구현 (observe action=effort) | `feat(observe): effort-router scorecard` — prod 진단: escalation 0%, ~4.5x 절감, 표본 작아 상수변경 미정당 |
| P1-4 능동 FTR 스코어카드 | ✅ 구현 (observe action=proactive) | `feat(observe): proactive-card FTR scorecard` — prod 진단: FTR 0%(166카드 중 162 engaged), 노이즈플로어 효과 실증 |
| **P2-5 anchor 자동 보강** | ⏸️ **보류(근거 있음)** | — |
| 1B retain 시각 메타, 1C hindsight 무효화 전파 | ⏸️ 미착수 | hindsight 서버 측, 효과 데이터 의존 |
| 2A/2C ACON 가이드라인·문서 큐레이션 | ⏸️ 미착수 | 상위 복잡도 |
| 5B 학습형 라우터 섀도 모드 | ⏸️ 미착수 | effort 스코어카드(P1-1)가 선행 데이터 축적 중 |

**P2-5 보류 근거**: anchor 시스템(`anchor_keywords.go` + `polaris/engine.go` SetAnchorKeywords)은 이미 완전히 배선·작동 중 — 위키 Tier1 중요도 ≥0.95 페이지 제목을 **최대 5개**(많으면 압축 무력화) 압축 summarizer에 보존. ACON의 "중요한 것 보존" 메커니즘 그 자체다. "압축 후 재조회" 프록시로 자동 보강하면 ①신호가 모호하고 ②cap-5 anchor 세트를 노이즈로 오염시켜 잘 튜닝된 메커니즘을 **악화**시킬 위험이 더 크다. 위키 중요도 기반 선택이 이미 옳은 소스라, 순효과 음(-)으로 판단해 보류. (프로젝트 철학 "narrow scope, deep quality" 일치.)

**P1-1/P1-4 패턴**: 둘 다 per-step/per-event 신규 로깅 없이 **기존 데이터(agentlog run.end · workfeed 카드 상태)로 진단을 산출**하고 observe 도구로 노출 — 네이티브 변경 0, 즉시 prod 데이터로 검증. 둘 다 "현 증거상 건강, 데이터 누적 시 추적 가능"이 결론(상수/임계 변경은 표본이 더 모인 뒤). 5B 학습형 라우터·로컬 능동 판정기는 이 스코어카드들이 쌓는 데이터가 선행 조건.

## 탐사 방법 주의

- 4개 병렬 탐사 에이전트 중 2개가 dreaming `Supersedes` 배선 여부에서 상충 → 직접 grep으로 확정 (배선됨). **다중 에이전트 보고는 상충 시 반드시 1차 소스(코드)로 재검증**할 것.
- 한 에이전트는 역할 재라벨 논문의 방향을 역으로 해석("user 역할 제시가 블라인드스팟을 유발")했다 — 원문 기준 user/tool/memory 역할 제시는 수정률을 **올리는** 쪽이다. 논문 매핑 보고서는 결론 채택 전 원문 주장과 대조할 것.
- 일부 에이전트 설계는 멀티유저 전제(카나리 롤아웃, 월 3-5K 라벨)를 포함 — 단일 사용자 환경으로 보정한 본 문서가 우선한다.
- abstract만으로 추출한 수치는 PDF에서 정정될 수 있다(이번에 5건). **헤드라인 수치 인용 전 원문 표 확인**이 원칙 — 특히 ProactiveBench류 "구체 %"는 1차 추출이 타 출처를 섞을 수 있다.
- PDF는 `/tmp/papers/`에 캐시(2507.05257, 2603.07915, 2511.10788 등). arXiv `curl -sL https://arxiv.org/pdf/<ID>` 정상 작동, 26xx ID도 실재.
