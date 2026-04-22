---
description: 뮤텍스·채널·goroutine 패턴 — 데드락 방지와 응답성 유지를 위한 규칙
globs: gateway-go/**/*.go
---

# Concurrency Rules

> **이 프로젝트는 과거 cron emit 재진입 데드락과 tracks-process drain panic으로 프로덕션이 행 걸린 이력이 있습니다. 새 코드가 같은 함정에 빠지지 않도록 이 체크리스트를 엄격히 따르세요.**

## 1. 뮤텍스 재진입 금지

Go의 `sync.Mutex` / `sync.RWMutex`는 **재진입 불가**. 다음 패턴은 무조건 데드락입니다:

```go
// 잘못된 예
func (s *Service) Add(x X) {
    s.mu.Lock()
    defer s.mu.Unlock()
    ...
    s.emit(event)  // emit 내부에서 s.mu.Lock() 재호출 → 데드락
}

func (s *Service) emit(e Event) {
    s.mu.Lock()    // 이미 Add에서 잡혀있음
    ...
}
```

**규칙**
- 뮤텍스 잡은 상태에서 같은 receiver의 다른 메서드를 부를 땐 내부 락 취득 여부 확인
- 내부용 헬퍼는 `xxxLocked` 접미사로 "caller가 락 보유 가정"임을 명시
- listener / subscriber 목록 같은 보조 상태는 **별도 뮤텍스**로 분리 (예: `listenersMu`)

## 2. Lock hierarchy 문서화

구조체에 뮤텍스가 2개 이상 있으면 struct docstring에 **순서 명시**:

```go
// Lock hierarchy (acquire in this order; never reverse):
//
//   Service.mu  →  Store.mu  →  TrackedEntry.mu
//   Service.listenersMu (independent — safe to hold under Service.mu)
```

역순으로 획득하는 경로가 없음을 리뷰로 검증. 새 경로 추가 시 이 문서 업데이트가 PR 필수 항목.

## 3. 외부 콜백은 락 해제 후 호출

subscriber / listener / 사용자 콜백을 invoke할 때는 반드시 snapshot-후-해제 패턴:

```go
// 좋은 예
b.mu.RLock()
snapshot := append([]Handler(nil), b.subs...)
b.mu.RUnlock()

for _, h := range snapshot {
    h(event)  // 락 없이 호출 — 핸들러가 다시 버스를 부를 수 있음
}
```

콜백 내부에서 어떤 서비스 메서드를 부를지 알 수 없으므로 항상 락 밖에서 실행.

## 4. goroutine은 panic 복구 필수

장기 goroutine(주기 작업, subscriber worker, poll loop)은 `defer recover()`로 감싸지 않으면 단 한 번의 panic이 프로세스 전체를 죽임:

```go
go func() {
    defer func() {
        if r := recover(); r != nil {
            logger.Error("panic in X loop", "panic", r)
        }
    }()
    ...
}()
```

`pkg/safego` 헬퍼를 우선 사용:

```go
safego.GoWithSlog(logger, "my-worker", func() { ... })
```

## 5. goroutine은 종료 경로 가져야 함

모든 장기 goroutine은 **반드시** 종료 조건을 가집니다:

- `ctx.Done()` 수신 (선호) — 서버 shutdown에 연동됨
- 전용 `done chan struct{}` close

외부에서 종료 신호를 못 보내는 goroutine은 리크. `Server.ShutdownCtx()`에서 파생받으면 서버 종료 시 자동 취소.

## 6. 채널 close는 owner만

- 채널을 close할 수 있는 goroutine은 **생성자(owner) 하나뿐**
- 여러 goroutine이 close할 가능성이 있으면 `sync.Once`로 단일-close 보장
- closed 채널에 send하면 panic — dispatcher는 close 전에 subscriber를 slice에서 제거

`select { case <-ch: default: close(ch) }` 패턴은 race에 취약. `sync.Once` 사용할 것.

## 7. context.Background() 사용 최소화

장기 background 작업이 **꼭 필요한 경우가 아니면** 항상 request ctx 또는 `Server.ShutdownCtx()`에서 파생:

- ❌ `context.WithTimeout(context.Background(), 5*time.Minute)` — 서버 종료 무시
- ✅ `context.WithTimeout(server.ShutdownCtx(), 5*time.Minute)` — graceful shutdown 연동
- ✅ `context.WithTimeout(ctx, X)` — request-scoped 작업

`context.Background()`를 쓰려면 주석으로 이유 명시 + bounded timeout 필수.

## 8. 사용자 응답 경로의 deadline

Telegram inbound → chat pipeline → tool 실행 경로는 `server.DefaultTurnDeadline` (현재 5분)에 묶임.
툴이 자기 sub-timeout을 더 짧게 설정할 순 있지만, request ctx를 **절대 `context.Background()`로 대체하지 말 것.**

## PR 체크리스트

새 뮤텍스나 goroutine을 추가하는 PR은 아래를 확인:

- [ ] 뮤텍스 2개 이상이면 struct docstring에 hierarchy 명시
- [ ] `xxxLocked` 메서드가 내부에서 같은 락 잡지 않음
- [ ] subscriber/listener 콜백은 snapshot-후-해제로 호출
- [ ] 새 goroutine에 `defer recover()` 또는 `pkg/safego`
- [ ] 새 goroutine에 종료 경로 (ctx.Done 또는 done 채널)
- [ ] 장기 background 작업은 `Server.ShutdownCtx()` 또는 request ctx에서 파생
- [ ] `go test -race` 통과
