---
description: "코드 변경 후 라이브 테스트 + 품질 검증 필수 — 코드만 완성하고 실제 동작/품질을 검증하지 않으면 안 된다"
globs: ["gateway-go/**/*.go", "proto/**/*.proto"]
---

# Live Testing & Quality Verification (필수)

> **코드 완성도가 높아도 실제 작동 품질이 나쁘면 의미 없다.**
> 단위 테스트 통과 ≠ 제품 품질. 반드시 실제 게이트웨이에서 작동 + 품질을 검증하라.

> **모든 채팅/품질/재현 테스트는 실제 텔레그램 경로를 사용한다 (WebSocket 테스트 제거됨).**

### Telegram 테스트 전제조건

- `~/.deneb/.env`에 `TELEGRAM_API_ID`, `TELEGRAM_API_HASH` 설정
- `~/.deneb/telegram-test.session` 생성: `python3 scripts/telegram-session-init.py`
- `DENEB_DEV_BOT_USERNAME` (기본: nebdev1bot)
- dev 게이트웨이 실행 중 + `DENEB_DEV_TELEGRAM_TOKEN` 설정

## 도구

프로덕션(18789)과 분리된 dev 인스턴스(포트 18790)를 관리한다.

### Lifecycle: `scripts/dev-live-test.sh`

| Command | Description |
|---|---|
| `restart` | 빌드 + dev 게이트웨이 재시작 |
| `start` / `stop` | dev 인스턴스 시작/종료 |
| `status` | 상태 + /health 응답 |

### Dev 환경 (프로덕션 동등)

dev 인스턴스는 항상 프로덕션 config를 기반으로 시작한다 (빈 config 모드 없음):

| 항목 | Dev | Production | 남은 차이 |
|---|---|---|---|
| Config | 프로덕션 config (dev-config-gen.sh) | `~/.deneb/deneb.json` | 없음 |
| Providers/Auth | 로딩 | 로딩 | 없음 |
| Hooks/Agents | 로딩 | 로딩 | 없음 |
| Telegram | dev 봇 (DENEB_DEV_TELEGRAM_TOKEN) | 프로덕션 봇 | 별도 봇 (의도적) |
| Bind | loopback | config-driven | 포트만 다름 (의도적) |

**환경 차이 확인:**
```bash
scripts/dev-live-test.sh parity    # dev vs prod 환경 비교 리포트
```

**Telegram 설정:** `~/.deneb/.env`에 dev 전용 봇 토큰 필수:
```bash
# ~/.deneb/.env
DENEB_DEV_TELEGRAM_TOKEN=<dev bot token>         # dev-live-test.sh (port 18790)
DENEB_ITERATE_TELEGRAM_TOKEN=<iterate bot token>  # dev-iterate.sh (port 18791)
```
- 각 토큰은 @BotFather에서 별도 봇을 만들어서 획득
- 프로덕션 봇과 다른 봇이므로 409 충돌 없이 동시 실행 가능
- 토큰 미설정 시 텔레그램 비활성 (그 외 모든 코드 경로는 동일하게 실행)

### Functional Testing

| Command | Description |
|---|---|
| `smoke` | Health + Ready smoke test |
| `chat MESSAGE` | 텔레그램으로 채팅 메시지 전송 + 응답 수신 |

### Quality Testing

| Command | Description |
|---|---|
| `quality` | **전체 품질 테스트** (health + chat + tool + formatting + tools-deep + edge) |
| `quality health` | 서브시스템 상태 품질 |
| `quality chat` | 한국어 응답 품질 (언어, 톤, 내용 충실도) |
| `quality tools` | 도구 사용 품질 (적절한 도구 선택, 완료, 에러 없음) |
| `quality format` | 포맷 품질 (마크다운, 목록, Telegram 안전성) |
| `quality tools-deep` | 도구 심층 테스트 (파일 읽기/검색/실행/메모리 결과 정확성, 에러 핸들링) |
| `quality edge` | 에지 케이스 테스트 (빈 입력/긴 입력/특수문자/코드블록/모호한 의도/멀티턴) |
| `quality-custom "메시지"` | 커스텀 메시지로 품질 테스트 |

### Baseline Tracking (회귀 감지)

| Command | Description |
|---|---|
| `baseline save` | 현재 결과를 베이스라인으로 저장 |
| `baseline compare` | 현재 vs 베이스라인 비교 (회귀 시 경고) |
| `baseline show` | 현재 브랜치 베이스라인 표시 |

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
| **도구 결과 정확성** | read/grep/exec 등 도구 결과가 실제 데이터와 일치하는지 |
| **도구 에러 핸들링** | 존재하지 않는 파일 읽기 등 에러 상황에서 크래시 없이 안내하는지 |
| **에지 입력 안정성** | 빈 입력, 5000자+ 장문, 특수문자, 코드블록 등에서 크래시 없는지 |
| **멀티턴 컨텍스트** | 같은 세션 내 이전 대화 내용을 기억하고 참조하는지 |

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

## 반복 테스트: `scripts/dev-iterate.sh`

**코드 수정 → 빌드 → 라이브 검증을 한 번에 실행하는 원샷 스크립트.**
클로드 코드가 상수값이나 코드를 수정한 뒤 바로 실행해서 결과를 확인한다.

```bash
scripts/dev-iterate.sh
# 출력: build... ok → start... ok → smoke... 2/2 → ITERATE_RESULT metric=2 ...
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

출력의 마지막 두 줄:
```
ITERATE_RESULT metric=2 build=ok server=ok checks=2/2 latency_ms=1835
DENEB_TEST_JSON {"version":1,"commit":"abc1234","phase":{...},"checks":[...],...}
```

- `ITERATE_RESULT` — 레거시 포맷 (하위 호환)
  - `metric=N` — 통과한 체크 수 (기본: 2가 최대)
  - `build=ok|fail` — 빌드 성공 여부
  - `server=ok|fail` — 서버 기동 성공 여부
  - `checks=P/T` — 통과/전체
  - `latency_ms=N` — 전체 소요 시간
- `DENEB_TEST_JSON` — 구조화된 JSON (에이전트용)
  - 각 체크별 pass/fail + 소요시간
  - 품질 메트릭 breakdown
  - 실패 시 `diagnostics` 필드에 원인 분류 + 제안

### 새 플래그

```bash
# 베이스라인 비교/저장
scripts/dev-iterate.sh --baseline         # 테스트 후 베이스라인과 비교
scripts/dev-iterate.sh --save-baseline    # 결과를 새 베이스라인으로 저장

# 커스텀 metric
scripts/dev-iterate.sh --metric "python3 my_metric.py"
# metric 스크립트는 stdout에 metric_value=N 출력해야 함
```

## 유저 증상 재현 (Reproduction)

유저가 문제를 보고하면, AI 에이전트가 직접 유저 역할을 해서 증상을 라이브로 재현한다.

### chat-check: 메시지 + assertion

유저의 실제 메시지를 보내고 assertion으로 증상 유무를 판별:
```bash
# 한국어 응답 확인
scripts/dev-live-test.sh chat-check "안녕" --expect-korean

# 특정 패턴이 응답에 있는지
scripts/dev-live-test.sh chat-check "날씨 알려줘" --expect "날씨|기온|온도"

# 특정 패턴이 없는지 (누출 검사)
scripts/dev-live-test.sh chat-check "안녕" --expect-not "<thinking>"

# 특정 도구가 호출되는지
scripts/dev-live-test.sh chat-check "시스템 상태" --expect-tool health

# 레이턴시 확인
scripts/dev-live-test.sh chat-check "안녕" --max-latency 10000

# 조합
scripts/dev-live-test.sh chat-check "파일 목록 보여줘" \
    --expect-korean --expect-tool fs --max-latency 30000
```

### 멀티턴 재현: multi-chat

같은 세션에서 여러 턴을 보내 컨텍스트 유지 문제를 재현:
```bash
# 컨텍스트 유지 확인
scripts/dev-live-test.sh multi-chat \
    "내 이름은 홍길동이야" \
    "내 이름이 뭐라고 했지?" \
    --expect-context "홍길동"

# 연속 대화 흐름
scripts/dev-live-test.sh multi-chat \
    "프로젝트 상태 알려줘" \
    "더 자세히 설명해줘"
```

### 도구 호출 검증: tool-check

특정 도구가 올바르게 호출 + 완료되는지:
```bash
scripts/dev-live-test.sh tool-check health "시스템 상태 확인해줘"
scripts/dev-live-test.sh tool-check vega "최근 대화 검색해줘"
```

### AI 에이전트의 증상 재현 절차

1. 유저가 보고한 메시지를 그대로 `chat-check`에 넣고 적절한 assertion 조합
2. 실패한 체크를 기반으로 코드 수정
3. 수정 후 같은 테스트 재실행하여 수정 확인
4. `logs-errors`로 숨은 에러 확인

## 주의사항

- 반복 테스트는 포트 **18791** (dev=18790, prod=18789와 분리)
- `DEV_LIVE_PORT` 환경변수로 포트 변경 가능
- 프로덕션에 절대 영향 없음
- **quality test 실패 시 "완료"라고 하지 마라** — 품질 문제를 수정하고 재검증해야 한다
- **로그에서 에러/경고 없는 것까지 확인**해야 진짜 완료
