# `internal/autoreply` 분할 계획 (`rules` / `cmd-dispatch` / `subagent`)

이 문서는 `internal/autoreply`가 비대해졌을 때 단계적으로 분리하기 위한 실행 계획이다.

## 1) 목표

- 명령 파싱/디스패치 로직과 하위 에이전트(subagent) 흐름을 분리해 변경 영향을 국소화한다.
- 규칙(rules) 평가 레이어를 명확히 분리해 테스트를 단순화한다.
- 점진적 마이그레이션으로 런타임 동작 변경 없이 구조만 정리한다.

## 2) 제안 모듈 경계

### A. `internal/autoreply/rules`

역할:
- 오토리플라이 실행 조건, 큐/디렉티브/타이핑 정책, 응답 정규화 등 “판단 규칙”을 담당.

주요 후보 이동 대상:
- `queue/*`
- `directives/*`
- `typing/*`
- `reply/*` 중 정책성 로직 (`send_policy`, `reply_directives`, `normalize_reply`)

공개 API 초안:
- `EvaluateQueuePolicy(...)`
- `ParseDirectives(...)`
- `ResolveTypingMode(...)`
- `BuildReplyPolicy(...)`

---

### B. `internal/autoreply/cmd-dispatch`

역할:
- 커맨드 파싱, 커맨드 라우팅, 핸들러 연결(플러그인/MCP/설정/세션)을 담당.

주요 후보 이동 대상:
- `handlers/commands*`
- `handlers/config_commands*`
- `handlers/mcp_commands*`
- `handlers/debug_commands*`
- `handlers/btw_command*`

공개 API 초안:
- `ParseCommand(...)`
- `DispatchCommand(...)`
- `RegisterBuiltins(...)`

---

### C. `internal/autoreply/subagent`

역할:
- ACP 변환/의존성, 서브에이전트 실행/합성, 관련 상태 관리를 담당.

주요 후보 이동 대상:
- `acp/*`
- `handlers/commands_subagents*`
- `handlers/subagents_utils.go`
- `agent_runner.go` 중 서브에이전트 전용 경로

공개 API 초안:
- `TranslateACP(...)`
- `ResolveSubagentDeps(...)`
- `RunSubagent(...)`

## 3) 의존성 규칙

분할 후 import 방향은 아래처럼 유지한다.

- `cmd-dispatch` → `rules` 허용
- `subagent` → `rules` 허용
- `rules` → (`cmd-dispatch`, `subagent`) 금지

즉, `rules`는 저수준 순수 정책 패키지로 유지한다.

## 4) 단계별 마이그레이션

1. **Facade 도입**
   - 기존 `autoreply` 패키지에서 새 패키지 호출용 thin facade를 만든다.
   - 기존 호출부 시그니처는 유지한다.
2. **명령 계층 이동**
   - `handlers/commands*`를 먼저 `cmd-dispatch`로 이동한다.
   - 테스트(`commands_*_test.go`)를 동반 이동한다.
3. **subagent 계층 이동**
   - `acp/*`, `commands_subagents*`를 `subagent`로 이동한다.
   - ACP 번역 테스트를 우선 고정한다.
4. **rules 계층 이동**
   - queue/directives/typing/reply 정책을 `rules`로 이동한다.
   - 사이드이펙트 없는 순수 함수 우선 정리.
5. **호환 레이어 정리**
   - TypeScript/레거시 경로 호환 코드는 최상위 1개 파일로 집약한다.

## 5) Done 기준

- `internal/autoreply` 최상위에서 커맨드/서브에이전트 구현 상세가 제거되어, 오케스트레이션만 남는다.
- 신규 패키지별 단위 테스트가 독립 실행 가능하다.
- import cycle 없이 `go test ./internal/autoreply/...`가 통과한다.

## 6) 리뷰 체크리스트

- 패키지 이동 시 공개 심볼 최소화 (`internal` 경계 적극 활용)
- 상태 저장/세션 접근은 adapter 인터페이스로 주입
- 파싱/정책/실행 단계 분리를 테스트에서 명시
- 마이그레이션 단계마다 동작 diff가 없음을 회귀 테스트로 확인
