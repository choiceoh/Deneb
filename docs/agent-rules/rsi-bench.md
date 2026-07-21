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
- `swap-consistency` / `ability-transfer`는 전용 코퍼스 전까지 **proxy ceiling** (각각 ≤58, 포화 시 swap ≤52).
- L4 `dispatch-land`는 마커 **`outcome`**(landed/…)을 착지로 센다 — review `status`는 accepted로 남을 수 있음. 착지는 **RHAE식 효율 가중**(`ledgers.land_efficiency`): 1차 시도 착지=1.0, 재시도는 `(1/attempts)²`로 감쇠, 상한 `LAND_EFFICIENCY_CAP`(1.15). attempts는 attemptId 말미 서수, 파싱 불가=1(무벌점).
- Snapshot 케이던스: `make bench-refresh` / `scripts/systemd/setup-bench-refresh.sh` (일 04:30).
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
