# Code-as-Agent-Harness 서베이 × Deneb 검토

> **출처**: [Awesome-Code-as-Agent-Harness-Papers](https://github.com/YennNing/Awesome-Code-as-Agent-Harness-Papers) (YennNing, 457편). 명제 = *코드는 산출물이 아니라 에이전트가 추론·행동·환경모델링·피드백·협응을 하는 실행가능·검사가능·상태유지 인터페이스(harness)다.* 3계층: Interface(추론/행동/환경) → Mechanisms(계획·메모리·도구·디버깅) → Scaling(멀티에이전트) → Applications.
> **방법**: 병렬 리서치 에이전트가 실제 논문(arXiv)을 fetch → Deneb 코드와 대조 → 채택/스킵 판정. 3라운드.
> **일시**: 2026-06-26
> **한 줄 결론**: 큰 패러다임(CodeAct·OS-메모리·RL 거버넌스)은 Deneb 워크로드에 안 맞거나 범위 밖. **채택 3건(머지 완료)**, 나머지는 **이미 구현됨**이거나 범위 밖. 세 라운드 일관된 신호 — **Deneb 아키텍처가 이미 포괄적**이며, 표면 검토에서 "갭"으로 보인 것 다수가 코드 확인 시 이미 존재.

---

## 1. 판정 요약

| 클러스터 | 판정 | 근거 |
|---|---|---|
| exec 트랜잭션 롤백 (warn 티어) | ✅ **채택 (#2884)** | [Fault-Tolerant Sandboxing](https://arxiv.org/abs/2512.12806) — 복구가능성만, 자동롤백 제외 |
| autoresearch raw-trace + 반증가능 예측 | ✅ **채택 (#2884)** | [Meta-Harness](https://arxiv.org/abs/2603.28052), [AHE](https://arxiv.org/abs/2604.25850) |
| research_panel 종합 누락 방지 | ✅ **채택 (#2884)** | [MAST](https://arxiv.org/abs/2503.13657) — inter-agent misalignment 37% |
| CodeAct (코드를 액션으로) | 🟡 스킵 | 원자 툴은 무승부 + 보안·캐시·로컬모델 비용 |
| MemGPT 셀프 페이징 | ✅ 이미 구현 | 자동 압축 + 에이전트 호출 메모리 도구 |
| 구조화 반복 압축 | ✅ 이미 구현 | `compaction/llm.go` 4섹션 + 증분 갱신 |
| 개인화/유저 선호 학습 | ✅ 이미 구현 | 사용자 카테고리 + dream Phase 3e 절차기억 |
| 코드 에이전트 신뢰성 ("tests pass ≠ correct") | ✅ 이미 도그마 | live-test 하드게이트 + full-CI 머지게이트 |
| SWEzze 기능충분성 | 🟡 스킵 | 실행 오라클 필요, Deneb 도메인엔 없음 |
| 학습형 RL 거버넌스 | 🟡 스킵 | 단일 사용자엔 과잉 |
| 로보틱스·과학·GUI/OS·RL훈련·수학 | ⛔ 범위 밖 | §5 |

---

## 2. 채택 완료 (PR [#2884](https://github.com/choiceoh/Deneb/pull/2884) 머지)

세 건 모두 *이미 있는 메커니즘의 정제*이며, Deneb 워크로드에 맞는 것만 추렸다.

1. **exec의 in-place 파일 편집 체크포인트** — `sed -i`·`>` 리다이렉트로 파일을 고치는 exec 명령이 fs Write/Edit 도구의 롤백 안전망을 우회하던 것을, 실행 전 대상 파일(존재하는 정규파일)을 기존 `pkg/checkpoint`로 스냅샷해 `/rollback` 복구 가능하게. 추출은 과포함+존재성 필터라 안전. 논문의 *복구가능성*만 도입, 임의 bash에 fragile한 *자동 롤백-on-failure*는 의도적 제외. → `tools/exec_safety.go:InPlaceFileTargets`, `tools/exec.go`.

2. **autoresearch 최적화 규율** — `.claude/rules/optimization.md`에 ① 퇴보 시 스칼라 metric 말고 **raw 트랜스크립트로 인과 진단**, ② 변경마다 **반증가능한 예측 선언 후 대조**(운빨 keep 방지). 결과 테이블에 예측/적중 컬럼.

3. **research_panel 종합 누락 방지** — 종합 지침에 "모든 패널리스트 빠짐없이 검토, 배제 시 사유 명시" 추가. Deneb 경량 오케스트레이션(서브에이전트·패널 팬아웃·결정적 워크플로)의 **유일한 inter-agent 노출 이음매**가 패널 합성 단계라는 MAST 분석에 따른 것.

---

## 3. 검토했으나 스킵 (이유)

| 아이디어 | 논문 | 스킵 이유 |
|---|---|---|
| **CodeAct** (코드=액션) | [2402.01030](https://arxiv.org/abs/2402.01030) | +20pt는 *어려운 멀티툴 조합*에서만; **원자 단일툴은 무승부**(API-Bank). Deneb 비서실장 워크로드는 대부분 원자적(메일·캘린더). 비용 큼: 임의 코드 실행(cmdsafety와 역행), 프롬프트 캐시 파괴(150 스키마), 강한 모델 의존성(오픈 13% vs GPT-4 74% — 로컬 dsv4 위험). 조합 필요 시 이미 exec/code 도구 있음. 필드 합의도 *하이브리드*(구조화 JSON 래퍼) |
| **SWEzze 기능충분성** | [2603.28119](https://arxiv.org/abs/2603.28119) | "빼면 실패하나?"를 잴 **테스트 오라클 필요**. Deneb 메일/캘린더/위키 도메인엔 정답 오라클이 없어 녹아웃 실험 불가. 회상은 유사도/BM25로 |
| **Context-Folding (RL)** | [2510.11967](https://arxiv.org/abs/2510.11967) | 구조적 제거 자체는 Deneb의 Tier 2b(나이 기반 도구결과 stub) + 서브에이전트 위임이 근사. 신규는 RL 학습(FoldGRPO)인데 Deneb는 **모델 훈련 안 함** |
| **학습형 RL 역량 거버넌스** | [2604.11839](https://arxiv.org/abs/2604.11839) | 역량 과다provisioning은 멀티테넌트 위협; Deneb는 단일 사용자. RL 툴 스코핑 과잉 — regex 차단목록이 맞는 하드 플로어 (*명칭 불일치 플래그됨: Aethelgard/AgentWarden*) |

---

## 4. 이미 구현돼 있던 것 (코드 확인) — "갭"인 줄 알았으나 검증으로 확인

> 표면 검토(README 일러두기)에서 도입 후보로 보였으나, Deneb 코드를 읽어보니 *이미 (대개 더 정교하게) 구현*돼 있었다. MemGPT·개인화에서 반복된 패턴.

- **구조화 반복 압축** (MemGPT/Hermes/서베이의 핵심 기법) — [`compaction/llm.go:379`](../../gateway-go/internal/pipeline/compaction/llm.go) `compactionOutputFormat`: 4섹션 필수 템플릿(핵심사실`[확실]` / 열린루프`[진행중\|차단\|대기]` / 불확실메모`[추정\|충돌\|오래됨]` / 도구결과) + `recompactionSystemPrompt` 증분 갱신(주석이 "Hermes _previous_summary pattern" 명시) + anchor 보존 + bounded digestion. 서베이 권장보다 정교.
- **MemGPT 능동 회상** — 에이전트가 턴 중 `wiki`/`diary`/`polaris(recall)` 도구를 호출(MemGPT의 active retrieval). 유일 미보유 = *사전 압박 신호*(현재는 사후 `CompactionFired`만)인데, 압박 받는 긴 턴(크론 합성·딥리서치)은 **이미 설계상 결론을 wiki/diary에 영구 기록**하므로 실효 얇음.
- **개인화 / 유저 선호 학습** — [`dreamer.go:328`](../../gateway-go/internal/domain/wiki/dreamer.go) Phase 3e 절차기억: 드림 사이클이 일지에서 유저 선호·교정을 distill해 **사용자** 카테고리(인물/프로젝트/거래와 분리 — iAgent의 "유저 전용 레인")에 적재 → `USER.md` "행동 지침"으로 승격(시스템 프롬프트 로드). [Evo-Memory](https://arxiv.org/abs/2511.20857)가 짚은 *"적재·검색만으론 학습 아님, 능동 추상화 필요"* = 바로 이 dream-cycle. 관련: [A-MEM](https://arxiv.org/abs/2502.12110), [Mem0](https://arxiv.org/abs/2504.19413), [iAgent](https://arxiv.org/abs/2502.14662).
- **"tests pass ≠ correct"가 이미 Deneb 도그마** — [Are 'Solved Issues' Really Solved?](https://arxiv.org/abs/2503.15223)(테스트 통과 패치 7.8%가 전체 테스트선 실패, 의심 패치 82.7%가 기존 테스트 사각) + [Agentic Coding PR 실증](https://arxiv.org/abs/2509.14745)(PR 45.1% 인간 수정 필요)의 핵심 = CLAUDE.md의 *"단위 테스트 통과 ≠ 제품 품질"* live-testing 하드게이트. 이번 세션 매 머지를 full `make ci`에 게이트한 것이 논문 #1 권고(타깃 아닌 전체 회귀) 실천. 컨벤션도 — [TOSEM 논문이 *literally* "CLAUDE.md 스타일 가이드 파일"을 1순위 레버로 추천].

---

## 5. thin / 선택적 (코드리뷰 verify 보강 — 미채택)

기능이 아니라 `code-review` 스킬/워크플로 verify 단계에 얹을 체크 수준. ROI 낮아 보류.

- **intent-consistency**: diff가 커밋 제목·이슈 의도를 실제로 이행하는지 ([CodeAgent](https://arxiv.org/abs/2402.02172) consistency 차원).
- **범위초과 변경 에스컬레이션**: 이슈 범위 밖 "보조 시맨틱 변경"이 있으면 리뷰 플래그(오답 최고수율 신호).
- **artifact-sync**: 공개 표면 코드 변경 시 동거 문서/주석도 갱신했는지(PR 수정의 29%가 docs).
- **propose-then-implement** ([CodeTaste](https://arxiv.org/abs/2603.04177)): 서브에이전트가 컨벤션 plan을 먼저 제시→선택 후 구현.

---

## 6. 범위 밖 (이유)

| 클러스터 | 왜 무관 |
|---|---|
| 로보틱스/임바디드 (~60편) | Deneb 물리 구현 없음 |
| 월드모델/환경모델링 (~22) | 물리·게임 시뮬레이션 |
| 과학발견 (~27) | 연구자동화 에이전트 아님 |
| GUI/OS 자동화 (~40) | 표면=네이티브 클라, 외부앱 클릭 자동화는 비제품 |
| RL 훈련 (RLTF/CodeRL/Atropos) | Deneb는 모델 소비, 훈련 안 함 |
| Code-for-Reasoning (PoT/PAL, 수학) | 수학·기호추론 코어 아님; "계산을 코드로 위임"은 exec로 이미 |
| 벤치마크 (SWE-bench/τ-bench/Terminal-Bench) | 기법 아닌 평가셋; Deneb는 live-test로 평가 |
| 코드 플래닝 (repo그래프/MCTS) | 2차적 코딩 서브에이전트 경로에만 marginal |

---

## 7. 논문 레퍼런스 (arXiv)

- **도구/액션**: CodeAct [2402.01030](https://arxiv.org/abs/2402.01030)
- **메모리/압축**: Context-Folding [2510.11967](https://arxiv.org/abs/2510.11967) · SWEzze/OCD [2603.28119](https://arxiv.org/abs/2603.28119) · MemGPT [2310.08560](https://arxiv.org/abs/2310.08560) · A-MEM [2502.12110](https://arxiv.org/abs/2502.12110) · Mem0 [2504.19413](https://arxiv.org/abs/2504.19413) · Evo-Memory/ReMem [2511.20857](https://arxiv.org/abs/2511.20857) · iAgent [2502.14662](https://arxiv.org/abs/2502.14662)
- **harness 엔지니어링**: AutoHarness [2603.03329](https://arxiv.org/abs/2603.03329) · Meta-Harness [2603.28052](https://arxiv.org/abs/2603.28052) · AHE [2604.25850](https://arxiv.org/abs/2604.25850)
- **거버넌스/샌드박싱**: Learned Governance [2604.11839](https://arxiv.org/abs/2604.11839) · Fault-Tolerant Sandboxing [2512.12806](https://arxiv.org/abs/2512.12806)
- **멀티에이전트 실패**: MAST [2503.13657](https://arxiv.org/abs/2503.13657) · Which Agent/When [2505.00212](https://arxiv.org/abs/2505.00212) · AgenTracer [2509.03312](https://arxiv.org/abs/2509.03312) · AgentDebug [2509.25370](https://arxiv.org/abs/2509.25370)
- **코드 에이전트 신뢰성/컨벤션**: Are Solved Issues Really Solved [2503.15223](https://arxiv.org/abs/2503.15223) · Agentic Coding PR study [2509.14745](https://arxiv.org/abs/2509.14745) · CodeAgent [2402.02172](https://arxiv.org/abs/2402.02172) · CodeTaste [2603.04177](https://arxiv.org/abs/2603.04177)

---

## 8. 결론

세 라운드의 일관된 패턴: **Deneb는 이 분야의 검증된 좋은 아이디어들을 이미 (대개 더 정교하게) 구현**하고 있다 — 구조화 압축, 능동 회상, 유저 선호 학습, live-test 하드게이트. 채택한 3건은 모두 *기존 메커니즘의 정제*였고, "큰 패러다임"은 단일 사용자·비서실장 워크로드에 안 맞거나 범위 밖이었다. 표면 검토에서 갭으로 보인 것(MemGPT 페이징, 개인화 학습)이 코드 확인 시 이미 존재한 사례가 반복된 것은, **다음 세션이 도입을 검토할 때 반드시 코드를 먼저 읽어 검증해야 함**을 시사한다. 관련 선행 연구노트: `hermes-deneb-mapping.md`, `agent-papers-2026-survey.md`.
