# 라이브테스트 속도 향상

## Context

라이브테스트에서 불필요한 대기 시간이 누적됨. 순차 실행, 고정 간격 폴링(150~200ms), 과도한 타임아웃(120s)이 주 원인. 병렬화 + 파이프라이닝 + 타임아웃 조정 + 백오프로 개선.

## 변경 사항

### 1. 품질 테스트 Semaphore(2) + 파이프라이닝 (가장 큰 효과)

`scripts/dev-quality-test.py:1010-1023` — 현재 300개 테스트가 순차 실행.

**전략**: 두 가지를 결합

**A) Semaphore(2)**: LLM 동시 호출 2개. GPU 경합을 피하면서도 한 테스트가 응답 대기하는 동안 다른 테스트가 inference 시작.

**B) 파이프라이닝**: 테스트 응답 수신 완료 → 점수 계산(CPU) + 다음 테스트 WebSocket 연결/세션 생성(network)을 동시 진행. 현재는 점수계산 → 출력 → 다음 연결 → 세션 생성 → RPC 전송이 직렬인데, 셋업 비용(connect + handshake + session.create)이 ~200-500ms.

```python
# 현재 흐름 (직렬):
# [test1: connect→chat→wait→score→print] → [test2: connect→chat→wait→score→print] → ...
#
# 변경 후 (semaphore 2 + pipeline):
# [test1: connect→chat→wait─────────────→score→print]
#                  [test2: connect→chat→wait────────→score→print]
#                                  [test3: connect→chat→wait...] (sem blocks until slot opens)

sem = asyncio.Semaphore(args.concurrency)  # default 2
done_count = 0
lock = asyncio.Lock()

async def run_one(idx, tdef):
    nonlocal done_count
    async with sem:
        c = GatewayClient(host, port)
        await c.connect()
        try:
            r = await run_test(c, tdef, profiles, cat_defaults)
        finally:
            await c.close()
    # scoring + printing outside semaphore (frees slot for next test)
    async with lock:
        done_count += 1
        status = "PASS" if r.passed else "FAIL"
        print(f"[{done_count}/{total}] {tdef['name']}... {status} ({r.latency_ms:.0f}ms)")
    return r

tasks = [asyncio.create_task(run_one(i, t)) for i, t in enumerate(tests)]
results = await asyncio.gather(*tasks, return_exceptions=True)
```

핵심:
- `await c.close()` + scoring이 **sem 바깥**에서 실행 → 슬롯 반납 즉시 다음 테스트 시작
- `--concurrency N` 플래그 추가 (기본 2, `--concurrency 1`이면 기존 순차 동작)
- 테스트마다 독립 WebSocket + 세션 → 상태 간섭 없음
- **예상 효과: ~1.8배** (sem=2, 파이프라인 오버랩 포함)

### 2. Smoke 체크 3개 병렬 실행

`scripts/dev-iterate.sh:371-426`, `scripts/dev-live-test.sh:133-172`

현재: health → ready → websocket RPC 순차 (~200-500ms)
변경: 3개를 병렬로 실행, 결과 수집 후 판정

```bash
# 각 체크를 서브셸에서 실행, temp file로 결과 전달
C_START=$(date +%s%N)
(curl -sf "http://$HOST:$PORT/health" 2>/dev/null \
  | python3 -c "import sys,json;print(json.load(sys.stdin).get('status',''))" 2>/dev/null \
  || echo "") > /tmp/deneb-smoke-h.$$ &
PID_H=$!
(curl -sf -o /dev/null -w "%{http_code}" "http://$HOST:$PORT/ready" 2>/dev/null \
  || echo "000") > /tmp/deneb-smoke-r.$$ &
PID_R=$!
# WS check는 Python으로 별도 실행
python3 -c "..." > /tmp/deneb-smoke-w.$$ 2>&1 &
PID_W=$!
wait $PID_H $PID_R $PID_W
C_MS=$(( ($(date +%s%N) - C_START) / 1000000 ))
# parse each temp file
```

- **예상 효과: ~100-200ms** (가장 느린 WS 체크 시간으로 수렴)

### 3. 품질 메트릭 이벤트 타임아웃 120s → 30s

| 파일 | 라인 | 변경 |
|---|---|---|
| `scripts/dev-quality-metric.sh` | 118 | `timeout=120` → `timeout=30` |
| `scripts/dev-live-test.sh` | 564 (`_chat_and_wait`) | `timeout=120` → `timeout=30` |

정상 이벤트는 5s 이내에 도착하고, `done`/`error`에서 루프 종료. 120s는 stall에만 발동.
**예상 효과: stall 시 90초 절약**

### 4. 서버/vChat/포트 폴링에 지수 백오프 적용

고정 150~200ms 간격 → 50ms에서 시작, 2배씩, 300ms 캡.

| 파일 | 현재 | 변경 |
|---|---|---|
| `scripts/dev-iterate.sh:226-236` | 40×0.15s (6s max) | 30회, 50→300ms 백오프 |
| `scripts/dev-iterate.sh:200-211` (vChat) | 80×0.2s (16s max) | 50회, 50→300ms 백오프 |
| `scripts/dev-iterate.sh:108-120` (포트) | 20×0.2s (4s max) | 15회, 30→200ms 백오프 |
| `scripts/dev-live-test.sh:74-81` | 30×0.2s (6s max) | 25회, 50→300ms 백오프 |
| `scripts/dev-live-test.sh:101-109` (포트) | 20×0.2s (4s max) | 15회, 30→200ms 백오프 |
| `scripts/dev-metric-gen.sh` (생성 코드) | 고정 간격 | 동일 백오프 |

인라인 패턴 (crash-exit 보존):
```bash
_WAIT_MS=50
for _ in $(seq 1 30); do
  if curl -sf "http://$HOST:$PORT/health" > /dev/null 2>&1; then
    HEALTHY=true; break
  fi
  if ! kill -0 "$GW_PID" 2>/dev/null; then break; fi
  sleep "$(awk "BEGIN {printf \"%.3f\", $_WAIT_MS/1000}")"
  _WAIT_MS=$(( _WAIT_MS * 2 ))
  (( _WAIT_MS > 300 )) && _WAIT_MS=300
done
```

## 수정 파일

- `scripts/dev-quality-test.py` — Semaphore(2) + 파이프라이닝 + `--concurrency` 플래그
- `scripts/dev-iterate.sh` — smoke 병렬 + 헬스/vChat/포트 폴링 백오프
- `scripts/dev-live-test.sh` — smoke 병렬 + 타임아웃 감소 + 폴링 백오프
- `scripts/dev-quality-metric.sh` — 타임아웃 감소
- `scripts/dev-metric-gen.sh` — 생성 코드 내 폴링 백오프

## 예상 효과 요약

| 병목 | Before | After | 절약 |
|---|---|---|---|
| 품질 300케이스 | 600-3000s | 330-1650s | **~1.8x** |
| Smoke 3체크 | 200-500ms | 100-300ms | ~100-200ms |
| 품질 메트릭 stall | 120s | 30s | **90s** |
| 서버 헬스 폴링 | ~1.05s | ~0.45s | ~600ms |
| vChat 기동 폴링 | 16s | ~10s | ~1-3s |
| 포트 정리 | 4s | ~2.5s | ~50-200ms |

## Verification

```bash
# 1. dev-iterate.sh — ITERATE_RESULT 정상 확인
scripts/dev-iterate.sh

# 2. smoke — 3/3 통과 (병렬 실행 확인)
scripts/dev-live-test.sh restart && scripts/dev-live-test.sh smoke

# 3. quality 병렬 실행 — 전체 시간 비교
time scripts/dev-live-test.sh quality core
time scripts/dev-live-test.sh quality daily

# 4. concurrency 동작 확인
python3 scripts/dev-quality-test.py --port 18790 --scenario core --concurrency 1  # 순차 (기존 동작)
python3 scripts/dev-quality-test.py --port 18790 --scenario core --concurrency 2  # 병렬

# 5. quality metric — 정상 점수 반환
scripts/dev-live-test.sh quality-custom "안녕"
```
