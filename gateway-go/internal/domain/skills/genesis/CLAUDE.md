# genesis — recursive self-improvement subsystem

Deneb의 자가개선 루프. 스킬을 진화시키고(L1), 진화시키는 *절차 자체*를 진화시키며(L2), 판정자를 공진화시킨다(L3). 정본 설계는 `docs/research/recursive-self-improvement-roadmap.md`.

## 불가침 원칙 (먼저 읽을 것)

- **LLM은 생산만, 판정은 결정적 Go.** LLM은 후보 body·검증 케이스·exhibit를 만들 뿐, accept/reject/adopt/rollback 결정은 전부 결정적 Go다. 수용 메커니즘은 그것이 수용하는 것에 의해 최적화되면 안 된다 (2026H1 스윕 만장일치).
- **수용 게이트 회로 = forbidden self-edit surface.** `surfaces.go`의 `acceptance-machinery` 목록(validation_engine·eprocess·meta 벤치·judge_accuracy·tracker_usage·surfaces·meta_evolution)은 자가개선 큐가 record-time 기각. 사람 PR로만 변경.
- **자기 브레이크.** `evolution_drift.go`가 원장을 읽어 reward-hacking 드리프트(judge 물러짐·채택 다양성 붕괴·롤백 급증·verifier 파손)를 감지하면 자동 채택을 동결(propose-only 복귀)한다.

## 층 지도

| 층 | 하는 일 | 주 파일 |
|---|---|---|
| **L1 스킬 진화** | 저성과 SKILL.md body 재작성 (bounded patch + judge + held-out replay + 롤백 워치) | `evolver*.go`, `tracker*.go` |
| **L2 메타 진화 (slow loop)** | evolve/judge 프롬프트 자체를 주간 개정 → epoch별 벤치 게이트 → 자동 채택 + 롤백 워치 | `meta_evolution.go`, `meta_judge_bench.go`(evaluator epoch=열화 골드페어), `meta_producer_bench.go`(producer epoch=shadow-replay flip) |
| **L3 verifier 공진화 (준비 중)** | judge가 라벨된 오판으로 학습; 라벨은 판정 정확도 레인이 결정적 생산 | `judge_accuracy.go`(심은결함 재생 + false-reject 채굴), charter 동결 = `tracker_validation_cases.go` `IsCharterCase` |

## 게이트 스택 (evolve 1건이 통과하는 순서)

1. behavioral replay (`validation_replay.go`) — 실행기로 도구 호출 재생, 회귀 시 기각
2. 결정적 selection preflight (bounded edit + self-harness audit)
3. held-out 검증 (`validation_engine.go`) — visible/blind 풀 분리, **케이스 단위 flip gate**(옛 케이스 깨면 집계 이득 무관 기각)
4. LLM self-test judge (+ teacher 에스컬레이션)
5. K-후보 중 held-out margin 최고 커밋
6. post-evolve 롤백 워치 (`tracker_usage.go`) — N일 미해소 시 시간 기반 confirm/expire

## 영속 (전부 append-only JSONL, `~/.deneb/data/`)

`skill_usage.jsonl`(사용) · `skill_genesis_log.jsonl`(lifecycle: evolved/confirmed/rolled_back/…) · `skill_validation_cases.jsonl` · `skill_rejected_edits.jsonl` · `meta_evolution_log.jsonl`(메타 경험 원장 — 다음 사이클이 읽음) · `judge_accuracy_log.jsonl`(L3 라벨) · `auto_adopt_freeze.json`(자기 브레이크 마커). `Tracker`가 이 파일들과 기동 시 재구축하는 인메모리 집계를 소유.

## 함정

- **Tracker는 DENEB_STATE_DIR 무관하게 `~/.deneb`에 쓴다** — dev/live-test 인스턴스가 프로덕션 skill_usage.jsonl을 공유(읽기 위주 위험 수용). workout·judge-accuracy 등 라이브 모델 호출+합성 쓰기 레인은 프로덕션-state 게이트로 격리.
- **합성 소스(workout)는 real과 격리** — `UsageSourceWorkout`은 evidence-only, 실사용 통계 오염 금지. 자기오염 방지의 핵심.
- **EvolutionHealth는 60s 캐시** — 안전 감시자(drift audit)는 캐시 우회 fresh compute를 쓴다.
- **가속 노브** (캘리브레이션): `DENEB_META_EVOLUTION_INTERVAL_DAYS`·`DENEB_SKILL_WATCH_MAX_AGE_DAYS`·`DENEB_META_BENCH_SCALE`·`DENEB_JUDGE_ACCURACY_INTERVAL_HOURS`. 킬 스위치 `DENEB_META_AUTO_ADOPT=0`.
