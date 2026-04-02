---
description: "코드 변경 후 라이브 테스트 + 품질 검증 필수 — 코드만 완성하고 실제 동작/품질을 검증하지 않으면 안 된다"
globs: ["gateway-go/**/*.go", "core-rs/**/*.rs", "proto/**/*.proto"]
---

# Live Testing & Quality Verification (필수)

> **코드 완성도가 높아도 실제 작동 품질이 나쁘면 의미 없다.**
> 단위 테스트 통과 ≠ 제품 품질. 반드시 실제 게이트웨이에서 작동 + 품질을 검증하라.

## 도구

프로덕션(18789)과 분리된 dev 인스턴스(포트 18790)를 관리한다.

### Lifecycle: `scripts/dev-live-test.sh`

| Command | Description |
|---|---|
| `restart` | 빌드 + dev 게이트웨이 재시작 |
| `start` / `stop` | dev 인스턴스 시작/종료 |
| `status` | 상태 + /health 응답 |

### Functional Testing

| Command | Description |
|---|---|
| `smoke` | Health + Ready + WebSocket RPC smoke test |
| `rpc METHOD [PARAMS]` | 단일 RPC 호출 |
| `session CMD1 CMD2...` | 여러 턴 — 하나의 WebSocket에서 복수 RPC 순서 실행 |
| `chat MESSAGE` | 채팅 메시지 전송 + 스트리밍 응답 수신 |

### Quality Testing

| Command | Description |
|---|---|
| `quality` | **전체 품질 테스트** (health + chat + tool + formatting) |
| `quality health` | 서브시스템 상태 품질 |
| `quality chat` | 한국어 응답 품질 (언어, 톤, 내용 충실도) |
| `quality tools` | 도구 사용 품질 (적절한 도구 선택, 완료, 에러 없음) |
| `quality format` | 포맷 품질 (마크다운, 목록, Telegram 안전성) |
| `quality-custom "메시지"` | 커스텀 메시지로 품질 테스트 |

### Log Analysis

| Command | Description |
|---|---|
| `logs [N]` | 최근 N줄 |
| `logs-watch` | 실시간 팔로우 |
| `logs-grep PATTERN` | 패턴 검색 |
| `logs-errors` | 에러/경고만 |
| `logs-since SECS` | 최근 N초 |

## 품질 검증 항목

`quality` 명령은 다음을 자동 검증한다:

| 검증 항목 | 설명 |
|---|---|
| **한국어 응답** | 응답의 한국어 비율이 충분한지 (Korean-first 원칙) |
| **내용 충실도** | 빈 응답, 너무 짧은 응답 감지 |
| **AI 필러 없음** | "좋은 질문!", "물론이죠" 등 무의미한 서두 감지 |
| **마크업 누출 없음** | `<function=`, `<thinking>`, `NO_REPLY` 등 내부 토큰 유출 |
| **Telegram 안전성** | 4096자 제한, HTML 태그 매칭 |
| **도구 사용 완결** | 시작된 도구가 반드시 완료되었는지, 에러 없는지 |
| **스트리밍 흐름** | 이벤트가 정상적으로 흘렀는지 |
| **응답 시간** | 레이턴시 임계값 이내인지 |

## 필수 절차: 코드 수정 완료 후

### Step 1: 빌드 + 시작
```bash
scripts/dev-live-test.sh restart
```

### Step 2: Smoke test (작동 여부)
```bash
scripts/dev-live-test.sh smoke
```

### Step 3: Quality test (작동 품질)
```bash
scripts/dev-live-test.sh quality
```
**전체 시나리오 통과해야** 한다. 실패 항목 있으면 수정 → 재시작 → 재검증.

### Step 4: 변경 관련 품질 검증

수정한 기능과 직접 관련된 시나리오를 추가로 테스트:

```bash
# 채팅 파이프라인 수정했으면
scripts/dev-live-test.sh quality chat
scripts/dev-live-test.sh quality-custom "수정한 기능을 테스트할 메시지"

# 도구 관련 수정했으면
scripts/dev-live-test.sh quality tools

# 포맷/렌더링 수정했으면
scripts/dev-live-test.sh quality format

# 여러 턴 흐름 테스트
scripts/dev-live-test.sh session "health" "session.list {}"
```

### Step 5: 로그로 숨은 문제 확인
```bash
scripts/dev-live-test.sh logs-errors
scripts/dev-live-test.sh logs-since 60
```

### Step 6: 정리
```bash
scripts/dev-live-test.sh stop
```

## Rust 코어 변경 시

```bash
make rust          # 또는 make rust-debug
scripts/dev-live-test.sh restart
scripts/dev-live-test.sh smoke
scripts/dev-live-test.sh quality
```

## 반복 테스트: `scripts/dev-iterate.sh`

**코드 수정 → 빌드 → 라이브 검증을 한 번에 실행하는 원샷 스크립트.**
클로드 코드가 상수값이나 코드를 수정한 뒤 바로 실행해서 결과를 확인한다.

```bash
scripts/dev-iterate.sh
# 출력: build... ok → start... ok → smoke... 3/3 → ITERATE_RESULT metric=3 ...
```

### 사용 패턴: 상수 최적화 루프

클로드 코드가 직접 반복하는 루프:

1. 대상 파일에서 상수값 읽기
2. 가설 세우기 (클로드 코드 자체가 LLM)
3. 상수값 수정 (Edit 도구)
4. `scripts/dev-iterate.sh` 실행
5. ITERATE_RESULT 확인: metric이 올랐으면 keep, 내렸으면 revert
6. 반복

### ITERATE_RESULT 파싱

마지막 줄의 형식:
```
ITERATE_RESULT metric=3 build=ok server=ok checks=3/3 latency_ms=1835
```

- `metric=N` — 통과한 체크 수 (기본: 3이 최대)
- `build=ok|fail` — 빌드 성공 여부
- `server=ok|fail` — 서버 기동 성공 여부
- `checks=P/T` — 통과/전체
- `latency_ms=N` — 전체 소요 시간

### 커스텀 metric

```bash
scripts/dev-iterate.sh --metric "python3 my_metric.py"
# metric 스크립트는 stdout에 metric_value=N 출력해야 함
```

## 주의사항

- 반복 테스트는 포트 **18791** (dev=18790, prod=18789와 분리)
- `DEV_LIVE_PORT` 환경변수로 포트 변경 가능
- 프로덕션에 절대 영향 없음
- **quality test 실패 시 "완료"라고 하지 마라** — 품질 문제를 수정하고 재검증해야 한다
- **로그에서 에러/경고 없는 것까지 확인**해야 진짜 완료
