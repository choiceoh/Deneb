# Session Domain 변경 지도

이 패키지는 gateway session의 in-memory 상태, lifecycle 전이, event bus와
crash-resume marker를 소유하는 독립 도메인 계층이다. chat, cron, RPC와
runtime/server는 이 계약을 소비하며 session 상태 규칙을 복제하지 않는다.

## 진입점과 책임

- `manager.go`의 `Session`, `Manager`, `NewManager`,
  `Manager.Create`, `Manager.Set`, `Manager.Patch`,
  `Manager.ApplyLifecycleEvent`가 상태 조회·변경의 공개 경계다.
- `state_machine.go`의 `IsValidTransition`, `IsTerminal`,
  `ValidateTransition`이 run status 전이 표의 단일 소스다.
- `lifecycle.go`의 `LifecycleEvent`, `LifecycleSnapshot`,
  `DeriveLifecycleSnapshot`이 start/end/error event를 순수 상태로
  변환한다.
- `events.go`의 `EventBus`, `NewEventBus`, `EventBus.Subscribe`,
  `EventBus.Emit`이 비동기 lifecycle 알림을 소유한다.
- `run_marker.go`의 `RunMarkerStore`, `NewRunMarkerStore`가 crash 후
  resume를 위한 최소 on-disk marker를 원자적으로 기록한다.
- `native_keys.go`의 `RestorableTranscriptChannel`과
  `HeartbeatTargetSession`이 native session key 해석을 중앙화한다.

## 의존 방향과 불변조건

- 이 package는 domain leaf이며 Go 표준 라이브러리 외 internal package를
  import하지 않는다. 의존 방향은 `pipeline/platform/runtime → domain/session`다.
- 상태 mutation은 `Manager`를 통과한다. 기존 session의 non-empty status
  변경은 `ValidateTransition`을 지키고, snapshot getter가 내부 pointer나
  map을 외부에 노출하지 않는다.
- `Manager` lock 순서는 `emitGate → mu → defaultsMu`다. event callback은
  lock 밖에서 실행되어 재진입 가능해야 한다.
- `EventBus.Emit`은 slow subscriber 때문에 producer를 막지 않는다.
  `Subscribe`가 반환한 unsubscribe를 수명주기 종료 때 호출해 worker를
  누수시키지 않는다.
- lifecycle terminal 우선순위(error, explicit abort, timeout, done)와
  start/end timestamp fallback을 호출자마다 다시 계산하지 않는다.
- `RunMarkerStore`는 temp file + rename과 key sanitization을 유지한다.
  marker에는 credential이나 transcript body를 추가하지 않는다.
- `Manager.StartGC`는 전달된 context 취소로 종료되고 한 번만 시작한다.
  새 background loop를 별도로 만들지 않는다.

## 집중 검증

상태 전이, lifecycle timestamp, concurrent mutation/event 재진입, GC 취소와
marker crash-safety를 검증한다. 결정적 패키지 검증 명령은:

`cd gateway-go && go test -count=1 ./internal/domain/session`

## State register (필드 블래스트 반경)

`Session` 필드를 추가/변경하기 전에 `make state-register`로 재생성되는
[docs/research/state-register-session.md](../../../../docs/research/state-register-session.md)를
보라 — 필드별 write/read 지점을 패키지 경계 너머까지 펼친 맵이다 (콜그래프가
못 잡는 상태 결합·크로스-패키지 리더 포함). 필드나 그 소비처를 바꿨으면 같은
커밋에서 재생성한다.
