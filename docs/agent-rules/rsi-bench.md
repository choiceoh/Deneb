---
description: "RSI Bench 점수·finding·baseline·과정/효용 도메인 ratchet 규약"
globs: ["scripts/audit/rsi-bench.py", "scripts/audit/rsi_bench_main.py", "scripts/audit/rsi_bench/**", "scripts/audit/rsi-bench-*.json"]
---

# RSI Bench

RSI Bench는 재귀적 자가개선 루프의 **과정(Process)** 과 **효용(Utility)** 을
하나의 벤치로 묶는다. 설계 SoT는 `docs/research/rsi-bench.md` 이다.
Health Bench 3.0 점수와 비교하거나 산술 변환하지 않는다.

## 평가 원칙

- Overall은 도메인 점수의 **가중 기하평균**이다 (Process 0.55 · Utility 0.45).
- LLM이 이 벤치를 채점하지 않는다. ledger / cache 집계만.
- `resolved < 3` 인 비율은 unmeasured (soft watch도 **soft_confirmed ≥ 3**).
- Soft 점수는 `SOFT_RESOLVE_SCORE_CAP` (55) 상한. Soft 표본은 **열린 워치** ∪
  (28d 안 evolved 후 real `postUses≥3` 재구성) 합집합.
- **1.2부터 Process와 Utility 모두 `--check` 래칫** (`ratcheted=true`).
- `--check`는 confidence < `MIN_CHECK_CONFIDENCE`(60)이면 실패 (증거 얇은 상태의 Utility 래칫 신뢰 금지).
- Health Fitness의 finding-land / feed-card는 RSI Utility를 **thin re-export**.
- **L4 `codebase-delta`는 health-v3 `structure` 도메인만 본다 — overall 금지.** overall에는 Fitness(w0.20)가 들어 있고 Fitness는 RSI `closure-land`·`operator-verdict`를 재수출하며 자기 `live-delta`로 **같은 live−baseline 비교를 한 번 더** 감점한다. overall을 읽던 시절엔 런타임 한 번의 하락이 스코어보드에 세 번 꽂혀 이 필러가 0.0으로 고정됐고, 정작 Structure는 −2.2였다(2026-07-30 실측). structure가 없으면 **bootstrap(unmeasured)** 로 두고 overall로 폴백하지 않는다. 런타임 퇴행은 health-v3의 자체 래칫 도메인 + `health_finding_miner.py` 런타임 레인으로 흐른다 — 여기서 "overall 복원"을 재제안하지 말 것.
- `operator-verdict`는 meta 레인(시스템 프롬프트 진화, 전체 29행·adopted 1)만 읽으면 영구히 `total<3` bootstrap 바닥(≤38)에 붙는다. **배달 레인의 `impactResult`(배포 후 선언 지표 실측 판정)를 함께 센다** — 수용(accept)보다 강한 증거라 후보를 더 승인해서 올릴 수 없다. 대칭 유지: `no_effect`는 adopt_rate를 내리고 `regressed`는 revert로 센다. 후보 id 단위 폴딩(append-only).
- `judge-fuel`의 `misses`는 **구별되는 결함 정체(skill|degradation) 수**다 — miss 이벤트 누적이 아니다. 판정 프로브는 케이던스로 재실행되는 상설 커리큘럼이라 안 고친 결함 하나가 매 런에 다시 잡힌다: 2026-07-30 실측 miss 이벤트 34건 = 구별 결함 8건, 그중 하나가 7회. 이벤트를 더하면 **프로브를 자주 돌릴수록 감점이 커져**(density는 자주 돌리라고 보상하는데) 필러가 포화된다 — 측정을 늘리면 점수가 떨어지는 rubric 버그다. 이벤트 수는 `miss_events`로 evidence에만 남기고, 만성 결함은 `chronic=` 으로 이름을 박아 압력을 유지한다.
- 미너의 런타임 레인은 **약한 dim 전부를 weakest-first로 넘기고** per-run cap은 `select_candidates`가 적용한다(reopen-blocked 행은 cap을 소비하지 않음). 생산 측에서 cap으로 자르면 최약 dim 2개가 14d 쿨다운에 들어간 동안 레인이 통째로 침묵한다(2026-07-30: `tool-reliability` 30.9가 한 번도 접수되지 않음).
- `swap-consistency` / `ability-transfer`는 전용 코퍼스 전까지 **proxy ceiling** (각각 ≤58, 포화 시 swap ≤52).
- L4 `closure-land`도 **착지(delivery)** 를 센다 — `accepted`는 "시도 승인"일 뿐 착지가 아니다. 착지 = dispatch 행의 `dispatchPhase=watch_passed`(롤백 감시 통과) ∪ review `status`∈{applied,landed}. accepted를 착지로 세던 시절엔 **배치 수용 스윕만으로 점수가 올랐고**(게이밍 벡터) 진짜 배달 16건은 세지 않았다(2026-07-25 실측: landed 배차 16건 전부 status=accepted). 행이 아니라 **후보 id 단위로 폴딩**한다(append-only 원장이라 리뷰가 많은 후보가 분모를 부풀렸다). `rejected`는 revert가 아니다 — 좋은 기각을 이중 처벌하지 않는다.
- L4 `dispatch-land`는 마커 **`outcome`**(landed/…)을 착지로 센다 — review `status`는 accepted로 남을 수 있음. 착지는 **RHAE식 효율 가중**(`ledgers.land_efficiency`): 1차 시도 착지=1.0, 재시도는 `(1/attempts)²`로 감쇠, 상한 `LAND_EFFICIENCY_CAP`(1.15). attempts는 attemptId 말미 서수, 파싱 불가=1(무벌점).
- Snapshot 케이던스: `make bench-refresh` / `scripts/systemd/setup-bench-refresh.sh` (일 04:30).
- **retention-proxy의 pathway 분해는 advisory** (`ledgers.load_pathway_window`, arXiv:2608.04003): confirm rate는 이득이 *있었다*만 말하고 그 이득이 **저장→회수→갱신 경로**를 탔는지는 말하지 않는다. 진화 후 real 사용 레코드의 `exercised` 귀속(스킬이 선언한 도구가 실제로 돌았는가)으로 `pathwaySupport`를 계산해 evidence 라인에 붙이고, confirm이 높은데 경로 근거가 없으면(attributed ≥3 · coverage ≥0.5 · support <0.5) finding을 낸다. **점수·ratchet 무접촉** — 프로덕션 이력이 없는 신호로 래칫 항을 재가중하지 않는다(게이팅 승격은 표본 축적 후). PAST-Bench의 **경험 on/off 대조군**은 하네스 작업이 필요해 여기 포함되지 않는다 — 지금 있는 것은 경로 귀속뿐이다.
- **Token-economics readout는 advisory** (`scripts/audit/rsi_bench/token_economics.py`, arXiv:2607.06906): agent-logs(`~/.deneb/agent-logs/`)에서 τ(완수 태스크당 토큰)·CPM(백만토큰당 완수)·cacheHit를 계산해 payload `token_economics` 사이드카 + `DENEB_RSI_TOKEN_ECONOMICS` 렌더 라인으로만 노출한다 — **Metric/도메인 점수·ratchet·confidence·baseline check 전부 무접촉** (self-improvement이 품질만 보고 token-max하는 걸 *가시화*할 뿐, 게이트 아님). 래칫 항 승격은 신뢰 baseline 확보 후 2단계.

## Rubric 1.2.0

| Domain | Weight | Metrics |
|---|---:|---|
| Process | 0.55 | acceptor-trust, confirm-honesty, judge-fuel, preference-collapse, swap-consistency, probe-coverage, timescale-turn, ability-transfer, anti-collapse |
| Utility | 0.45 | closure-land, operator-verdict, codebase-delta, retention-proxy, dispatch-land |

expect-band **25–50** (L4 outcome 정합 + soft resolve 표본 반영). 루브릭 승격 시 `--migrate-rubric`.

## Baseline과 ratchet

```bash
make rsi-bench
make rsi-bench-check
make bench-check
make rsi-bench-deep
make bench-refresh
make rsi-bench-test
python3 scripts/audit/rsi-bench.py --update-baseline --migrate-rubric --expect-band 25:50
# production host once: scripts/systemd/setup-bench-refresh.sh
```

## 변경 시 검증

- `make rsi-bench-test`
- 게이트웨이 변경 시 `go test ./internal/runtime/server/ -run MetaRSI`
- 한 도메인 상승이 다른 도메인 래칫 실패를 가리지 않음 (기하평균)
