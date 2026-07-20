# genesis — recursive self-improvement subsystem

Deneb의 자가개선 루프. 스킬을 진화시키고(L1), 진화시키는 절차 자체를
진화시키며(L2), 판정자를 공진화시키고(L3), 소스를 자가편집한다(L4).
L5 거버넌스는 검증기 보정이 입증될 때까지 동결한다. 정본 설계는
`docs/research/recursive-self-improvement-roadmap.md`다.

## 불가침 원칙

- **LLM은 생산만, 판정은 결정적 Go.** LLM은 후보 body·검증 케이스·exhibit를
  만들 뿐 accept/reject/adopt/rollback 결정은 결정적 Go가 내린다.
- **수용 게이트 회로는 forbidden self-edit surface다.** `surfaces.go`의
  `acceptance-machinery` 목록(validation_engine·eprocess·meta 벤치·
  judge_accuracy·tracker_usage·surfaces·meta_evolution)은 자가개선 큐가
  record-time에 기각하며 사람 PR로만 변경한다.
- **자기 브레이크를 보존한다.** `evolution_drift.go`가 reward-hacking 드리프트
  (judge 완화·채택 다양성 붕괴·롤백 급증·verifier 파손)를 감지하면 자동 채택을
  동결하고 propose-only로 복귀한다.

## 층 지도

| 층 | 하는 일 | 주 파일 |
|---|---|---|
| **L1 스킬 진화** | 저성과 SKILL.md body 재작성 | `evolver*.go`, `tracker*.go` |
| **L2 메타 진화** | evolve/judge/genesis 프롬프트를 epoch 회전으로 개정하고 벤치로 채택·롤백 | `meta_evolution.go`, `meta_judge_bench.go`, `meta_producer_bench.go`, `meta_genesis_bench.go` |
| **L3 verifier 공진화** | 라벨된 judge 오판과 심은 결함을 재생 | `judge_accuracy.go`, `tracker_validation_cases.go`의 `IsCharterCase` |
| **L4 소스 자가편집** | 근거 있는 코드 후보를 코딩 레인에 제안(propose-only) | `runtime_error_mining.go`, `evolver_tool_gap.go`, `surfaces/surfaces.go` |
| **L5 메타 거버너** | 수용 정책과 안전 경계 변경(현재 동결, 실행기 없음) | 거버넌스 경계 문서 |

모든 층은 개념적으로 관찰 → 평가 → 검증의 흐름을 따르지만, 하나의 범용 실행
오케스트레이터를 공유하지 않는다. `lifecycle/` identity는 현재 실행되는 L1-L4의
UI용 정체성만 담고,
각 층의 생산자·결정적 게이트·벤치·롤백 정책은 해당 구현이 소유한다. L4는
review와 delivery를 분리하고, `watch_passed` 이후에만 `applied`를 파생한다.
후보 선택과 RSI 상태 판정은 Go가 소유하며 sh/py는 실행과 표시만 담당한다.

## L2 자문 증거 (ADVISORY — 게이트 불가침)

메타 진화의 `assembleEvidence()`는 운영자가 체감하는 효용을 세 자문 신호로
받는다 (RSI P5-5). 이들은 생성자 산문을 접지할 뿐 **어떤 게이트도 읽지 않는다**
— subtle-vs-blatant judge-degradation 분리와 같은 "informs vs decides" 패턴.

| 신호 | 소스 | 주입 |
|---|---|---|
| feed-card 수락/기각 | `MetaRevisionRecord.Action` 원장 | `assembleOperatorUtilityEvidence()` |
| 런타임 건강 (latency#1) | `agentlog.Writer.AggregateByModel` | `RuntimeHealth` 클로저 (서버 주입) |
| 코드베이스 건강 | `health-v2-baseline.json` | `QualityBench` 클로저 (서버 주입) |
| 개정 구조성 균형 (L1.5 함정 계측) | `MetaRevisionRecord.RevisionClass` 원장 (`meta_revision_class.go` 결정적 분류) | `assembleRevisionClassEvidence()` (producer 에폭; 파라미터형 연속 채택 ≥3이면 구조 개정 넛지) |

세 신호 모두 기존 데이터 소스의 읽기 전용 뷰이며, genesis는 리프 패키지로
유지된다 (agentlog/baseline 지식은 서버 클로저가 소유).

## 진입점과 책임

- `tracker.go`의 `Tracker`와 `NewTracker`가 사용·실패·후보 상태의 append-only
  기록과 재구축을 소유한다. `tracker_*.go`는 기록 종류별 조회와 상태 전이를
  나눈다.
- `evolver.go`의 `Evolver`와 `NewEvolver`가 후보 평가, 적용, 회귀 롤백을
  오케스트레이션한다. 단계별 결정은 `evolver_*.go`에 둔다.
- `curator.go`의 `SkillCuratorTask`는 반복 신호를 큐레이션하고,
  `workout.go`의 `SkillWorkoutTask`는 기존 스킬을 실행 증거로 점검한다.
- `meta_evolution.go`와 `meta_*_bench.go`는 evolution 자체의 품질을 평가하며
  제품 런타임과 분리된 평가 계약으로 유지한다.

## 게이트 순서

1. `validation_replay.go`의 behavioral replay
2. bounded edit와 self-harness audit의 결정적 selection preflight
3. `validation_engine.go`의 visible/blind held-out 및 케이스 단위 flip gate
4. LLM self-test judge와 teacher escalation (승인 verdict는 order-swap 일관성
   프로브 통과 필수 — 역방향 쌍을 기각해야 하며, 양방향 승인/프로브 오류는
   fail-closed. 킬스위치 `DENEB_JUDGE_SWAP_CHECK=0`)
5. K개 후보 중 held-out margin이 가장 높은 후보 커밋
6. `tracker_usage.go`의 post-evolve rollback watch

후보는 검증 증거 없이 적용 상태로 건너뛸 수 없다. 채택·거절·롤백 순서와
감사 기록을 바꾸거나 집계 이득으로 기존 케이스 회귀를 상쇄하면 안 된다.

## 의존 방향과 데이터

- `Tracker`가 수명주기 상태의 단일 소스다. 런타임이나 도구가 tracker 파일을
  직접 수정하거나 별도 상태 enum을 만들지 않는다.
- LLM 응답은 제안 입력일 뿐 계약이 아니다. 파싱·검증된 구조만 `Tracker`에
  전달하고 자유 형식 값을 공개 API로 확산하지 않는다.
- `runtime/server` 배선이나 chat tool을 이 도메인으로 역수입하지 않는다. 필요한
  실행 능력은 좁은 함수 또는 인터페이스로 주입한다.
- 영속 파일은 `~/.deneb/data/` 아래 append-only JSONL이다. 주요 원장은
  `skill_usage.jsonl`, `skill_genesis_log.jsonl`, `skill_validation_cases.jsonl`,
  `skill_rejected_edits.jsonl`, `meta_evolution_log.jsonl`,
  `judge_accuracy_log.jsonl`이며 자기 브레이크 마커는 `auto_adopt_freeze.json`이다. <!-- docref:ignore -->

## 함정

- `Tracker`는 `DENEB_STATE_DIR`와 무관하게 `~/.deneb`에 쓴다. workout과
  judge-accuracy처럼 합성 쓰기와 라이브 모델 호출을 하는 lane은 production-state
  gate로 격리한다.
- `UsageSourceWorkout`은 evidence-only다. 합성 사용량을 실제 사용 통계에 섞지
  않는다.
- `EvolutionHealth`는 60초 캐시지만 drift audit은 fresh compute를 사용한다.
- 캘리브레이션 노브는 `DENEB_META_EVOLUTION_INTERVAL_DAYS`,
  `DENEB_SKILL_WATCH_MAX_AGE_DAYS`, `DENEB_META_BENCH_SCALE`,
  `DENEB_JUDGE_ACCURACY_INTERVAL_HOURS`이며 킬 스위치는
  `DENEB_META_AUTO_ADOPT=0`이다.

## Local change scope

genesis는 리프 도메인이다. 루프 로직을 바꿀 때 서버/도구로 역수입하지 않는다.

- 함께 바꿔도 되는 이웃: `genesis/generation`, `genesis/guardrails`,
  `genesis/review`, `runtime/skilllifecycle`(도구 표면), `runtime/server`의
  `init_genesis.go`(배선만). `Tracker`/`NewTracker`/`Evolver`/`NewEvolver` 계약이
  바뀌면 `evolver_test.go`와 `meta_revision_class_test.go`를 먼저 본다.
- 건드리지 말 것: acceptance machinery(`validation_engine.go`, `eprocess`,
  `surfaces.go`의 forbidden 목록), `pipeline/chat` 프롬프트 캐시 경로,
  client wire 생성물. LLM 출력을 accept/reject 결정으로 승격하지 않는다.
- 집중 검증: `cd gateway-go && go test -count=1 ./internal/domain/skills/genesis`

## 변경과 검증

새 전이나 평가 신호를 추가할 때 정상, 거절, 재시작 복원, 중복 실행 멱등성을
해당 `*_test.go`에 함께 추가한다. 테스트는 실제 `Tracker` 또는 `Evolver` 심볼을
통해 행동을 관찰해야 한다.

집중 검증:

`cd gateway-go && go test ./internal/domain/skills/genesis/...`

루트 패키지만 반복할 때:

`cd gateway-go && go test -count=1 ./internal/domain/skills/genesis`

evolution 적용 로직은 rollback, self-correction funnel, held-out flip gate까지 같은
명령에서 통과시킨다.
