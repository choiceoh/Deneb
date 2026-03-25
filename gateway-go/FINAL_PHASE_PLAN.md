# Final Phase: Node.js 브릿지 완전 제거

## Context

Go 게이트웨이 마이그레이션 Phase 1-4 완료. 대부분의 RPC 메서드가 네이티브 Go로 포팅됨.
아직 `methods_bridge.go`에 11개 메서드가 Node.js로 포워딩되고 있고, `internal/bridge/` 패키지 +
Plugin Host 서브프로세스 관리 코드가 남아있음. 이것을 완전히 제거하여 Go 단독 실행을 달성하는 것이 목표.

## 1단계: 남은 브릿지 메서드 11개 네이티브 포팅

### 현재 `methods_bridge.go`에 남은 메서드

| 메서드 | 난이도 | 구현 방향 |
|--------|--------|----------|
| `channels.logout` | **낮음** | `LifecycleManager.StopChannel()` 호출 — 이미 인프라 있음 |
| `sessions.patch` | **완료** | `methods_sessions.go`에 이미 구현됨 — 브릿지 fallback만 제거 |
| `sessions.reset` | **완료** | `methods_sessions.go`에 이미 구현됨 — 브릿지 fallback만 제거 |
| `sessions.compact` | **완료** | `methods_sessions.go`에 이미 구현됨 — 브릿지 fallback만 제거 |
| `sessions.preview` | **완료** | `methods_sessions.go`에 이미 구현됨 — 브릿지 fallback만 제거 |
| `sessions.resolve` | **완료** | `methods_sessions.go`에 이미 구현됨 — 브릿지 fallback만 제거 |
| `tools.catalog` | **완료** | `methods_tools_catalog.go`에 이미 구현됨 — 브릿지 등록만 제거 |
| `browser.request` | **제거** | 기능 자체 삭제됨 (commit `e26787d`) — 등록만 제거 |
| `web.login.start` | **낮음** | 단일 유저 배포 — "always authenticated" 스텁 |
| `web.login.wait` | **낮음** | 단일 유저 배포 — "always authenticated" 스텁 |

**핵심**: `sessions.*` 5개와 `tools.catalog`는 이미 Go 네이티브 핸들러가 `RegisterSessionMethods`와
`RegisterToolCatalogMethod`로 등록되어 있지만, `methods_sessions.go`의 `forwardToBridge()` 호출이
아직 브릿지를 fallback으로 사용하고 있음. 이 fallback 경로를 제거해야 함.

### 작업 내용

**파일 수정:**
- `gateway-go/internal/rpc/methods_bridge.go` — 메서드 리스트 비우기 또는 파일 삭제
- `gateway-go/internal/rpc/methods_sessions.go` — `forwardToBridge()` 호출 제거, 순수 Go 로직만 유지
- `gateway-go/internal/rpc/method_scopes.go` — `browser.request` 스코프 제거
- `gateway-go/internal/rpc/methods_bridge_test.go` — 브릿지 메서드 파리티 테스트 업데이트

**신규 파일:**
- `gateway-go/internal/rpc/methods_weblogin.go` — web.login 스텁 핸들러
- `gateway-go/internal/rpc/methods_channel_logout.go` — channels.logout 네이티브 핸들러

**예상 규모**: ~100 LOC 추가, ~200 LOC 수정/제거

## 2단계: `forwardToBridge()` 의존성 제거

`methods_sessions.go`의 5개 메서드가 아직 `forwardToBridge(ctx, deps.Forwarder, req)` 패턴을 사용.

| 메서드 | `forwardToBridge` 용도 | 제거 후 대체 |
|--------|----------------------|------------|
| `sessions.patch` | 영속 스토어 업데이트 | `Manager.Patch()` 결과만 반환 (이미 동작) |
| `sessions.reset` | 트랜스크립트 아카이브 + 스토어 리셋 | `transcript.Writer.Archive()` + `Manager.ResetSession()` |
| `sessions.compact` | JSONL 트랜스크립트 truncate | `transcript.Writer.Compact()` (이미 구현됨) |
| `sessions.preview` | 트랜스크립트 파일 읽기 | `transcript.Writer.ReadPreview()` (신규 필요) |
| `sessions.resolve` | 영속 스토어 조회 | `Manager` 인메모리 조회 (이미 동작) |

**핵심 작업**: `sessions.preview`에서 트랜스크립트 파일을 직접 읽는 `ReadPreview()` 메서드를
`transcript.Writer`에 추가해야 함. JSONL 파일의 마지막 N개 라인을 읽어 프리뷰 반환.

**`SessionDeps.Forwarder` 필드 제거** → `Forwarder` 인터페이스 의존성 해소.

**파일 수정:**
- `gateway-go/internal/rpc/methods_sessions.go` — `Forwarder` 필드 제거, `forwardToBridge()` 삭제
- `gateway-go/internal/transcript/writer.go` — `ReadPreview()` 메서드 추가

**예상 규모**: ~80 LOC 추가, ~60 LOC 제거

## 3단계: tools.invoke 브릿지 fallback 제거

`methods_tools.go`의 `toolsInvoke()`:
- bash/exec 도구: 이미 `process.Manager`로 로컬 실행 ✅
- 기타 도구: `deps.Forwarder.Forward()` 로 브릿지에 위임 ❌

**구현 방향:**
- 배포 환경이 단일 유저/단일 서버이므로, 지원하는 도구 목록을 명시적으로 정의
- bash/exec 외의 도구 호출은 에러 반환 또는 도구별 네이티브 핸들러 추가
- `tools.list`도 브릿지 fallback 대신 정적 카탈로그 반환

**파일 수정:**
- `gateway-go/internal/rpc/methods_tools.go` — `Forwarder` 필드 제거, 브릿지 fallback 제거

**예상 규모**: ~30 LOC 수정

## 4단계: Chat 핸들러 브릿지 의존성 제거

`chat.Handler`의 `Forwarder` 필드:
- `chat.send`: Phase 4에서 이미 `RegisterSessionExecMethods`로 네이티브 포팅됨
- `chat.history`: 브릿지로 트랜스크립트 조회 → `transcript.Writer`로 직접 읽기
- `chat.inject`: 브릿지로 트랜스크립트 추가 → `transcript.Writer.AppendStructured()`

**파일 수정:**
- `gateway-go/internal/chat/chat.go` — `Forwarder` 필드/인터페이스 제거
- `gateway-go/internal/chat/chat.go` — `History()`, `Inject()` 메서드를 트랜스크립트 직접 접근으로 변경

**예상 규모**: ~60 LOC 수정

## 5단계: Provider auth 브릿지 의존성 제거

`provider.AuthManager.Forwarder`:
- `Prepare()` 메서드에서 로컬 인증 실패 시 브릿지로 `providers.auth.prepare` 요청
- 단일 유저 배포에서는 API 키가 환경변수/config에 직접 설정되므로 브릿지 fallback 불필요

**파일 수정:**
- `gateway-go/internal/provider/auth.go` — `Forwarder` 필드/인터페이스 제거, `SetForwarder()` 삭제

**예상 규모**: ~20 LOC 제거

## 6단계: server.go 브릿지 와이어링 제거

`SetBridge()` 메서드와 관련 코드 전체 제거:

| 와이어링 | 대체 |
|----------|------|
| `s.dispatcher.SetForwarder(b)` | 디스패처 기본 포워더 제거 (unknown method → NOT_FOUND) |
| `s.authManager.SetForwarder(b)` | 제거 (5단계에서 처리) |
| `s.chatHandler.SetBroadcastRaw(...)` | 브로드캐스터 직접 와이어링 |
| Process approval callback | Go 네이티브 승인 UI (이미 `internal/approval/` 존재) |
| `b.SetEventHandler(...)` | 이벤트 핸들러 전체 제거 (Go가 모든 이벤트 생성) |
| `lazyForwarder` 타입 | 삭제 |
| `bridgeStatus()` | 삭제 |

**파일 수정:**
- `gateway-go/internal/server/server.go` — `SetBridge()`, `lazyForwarder`, 브릿지 필드 제거
- `gateway-go/internal/rpc/dispatch.go` — `Forwarder` 인터페이스 + `SetForwarder()` 제거

**예상 규모**: ~150 LOC 제거

## 7단계: CLI + 프로세스 관리 제거

`cmd/gateway/main.go`에서:
- `--bridge` / `--plugin-host-cmd` CLI 플래그 제거
- `setupBridge()` 함수 제거
- `startPluginHostChannels()` 함수 제거
- `bridge.SpawnPluginHost()` 호출 제거

**파일 수정:**
- `gateway-go/cmd/gateway/main.go`

**예상 규모**: ~80 LOC 제거

## 8단계: bridge 패키지 삭제 + 정리

- `gateway-go/internal/bridge/` 디렉토리 전체 삭제 (6개 파일)
- `gateway-go/internal/chat/bridge_events.go` 삭제 (브릿지 이벤트 처리)
- 모든 파일에서 `bridge` 임포트 제거
- 테스트 업데이트

**삭제 대상 파일:**
```
gateway-go/internal/bridge/bridge.go
gateway-go/internal/bridge/spawn.go
gateway-go/internal/bridge/protocol.go
gateway-go/internal/bridge/bridge_test.go
gateway-go/internal/bridge/spawn_test.go
gateway-go/internal/bridge/event_handler_test.go
gateway-go/internal/chat/bridge_events.go
```

**예상 규모**: ~-1500 LOC (순 삭제)

## 구현 순서 (의존성 기반)

```
1단계: 남은 11개 브릿지 메서드 네이티브 포팅/스텁/제거
  ↓
2단계: sessions.* forwardToBridge() 제거
  ↓
3단계: tools.invoke 브릿지 fallback 제거
  ↓
4단계: Chat 핸들러 브릿지 의존성 제거
  ↓
5단계: Provider auth 브릿지 의존성 제거
  ↓
6단계: server.go SetBridge() + 와이어링 제거
  ↓
7단계: CLI 플래그 + 프로세스 관리 제거
  ↓
8단계: bridge 패키지 삭제 + 정리
```

## 검증

1. `go build -tags no_ffi ./...` — 컴파일 확인
2. `go test -tags no_ffi ./...` — 모든 테스트 통과
3. `make go` — 풀 빌드 (FFI 포함, Rust 빌드 후)
4. `make go-test` — 풀 테스트
5. 게이트웨이 시작 시 Node.js 프로세스 없이 정상 동작 확인
6. `pgrep -f node` — Node.js 프로세스 없음 확인
7. Telegram 메시지 송수신 테스트
8. 에이전트 도구 호출 테스트
9. `deneb channels status --probe` — Telegram 연결 정상

## 위험 요소

1. **sessions.preview 트랜스크립트 읽기**: JSONL 파일 직접 읽기 구현 필요. 파일 포맷이 TS 쪽과 일치해야 함
2. **Process approval 워크플로우**: 브릿지 없이 승인 UI를 어떻게 표시할지 — `internal/approval/` 이미 있지만 완전한 승인 플로우인지 확인 필요
3. **Config reload**: `plugin-host.reload` 포워딩 제거 후 Go 쪽에서 config 변경 감지/적용 경로 확인
4. **이벤트 생성**: 브릿지에서 오던 이벤트(chat, agent, heartbeat 등)를 이제 Go가 직접 생성해야 함 — Phase 4에서 대부분 구현됨, 누락 확인 필요

## 예상 총 규모

| 단계 | 추가 | 삭제 | 순변경 |
|------|------|------|--------|
| 1-2단계 | ~180 | ~260 | -80 |
| 3-5단계 | ~0 | ~110 | -110 |
| 6-7단계 | ~0 | ~230 | -230 |
| 8단계 | ~0 | ~1500 | -1500 |
| **합계** | **~180** | **~2100** | **~-1920** |

순 삭제 ~1920줄. Node.js 의존성이 완전히 사라지고, 게이트웨이가 Go 단일 바이너리로 실행됨.
