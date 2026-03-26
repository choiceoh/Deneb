# Copilot 개선 설계: 게이트웨이 통합 코파일럿

## 현재 상태

수동적 시스템 모니터. 5개 외부 체크(sglang, 디스크, GPU, 프로세스, 로그)를 15분 주기로
실행하고 텔레그램으로 알림. 게이트웨이 내부 상태(프로바이더 헬스, 컴팩션 실패, 세션 에러)와
연결 없음.

### 현재 한계

- 게이트웨이 런타임과 분리: LLM 프로바이더 장애, context overflow, 컴팩션 실패 등 감지 불가
- 알림 스팸: 이슈 지속 시 매 사이클 동일 알림 반복, 복구 알림 없음
- 텔레그램 상호작용 없음: RPC로만 제어, 사용자가 `/copilot`으로 조회 불가
- 메모리/스왑 체크 누락
- 순차 실행: 독립 체크들이 불필요하게 직렬

---

## 설계: 3-Layer 아키텍처

```
Layer 3: Service (주기 실행 + 알림)
    │
Layer 2: Advisor (자동 개입 판단)
    │
Layer 1: Tracker (이벤트 수집)
    │
게이트웨이 런타임 (chat/run.go, compaction.go)
```

---

## Layer 1: Tracker (`tracker.go`)

게이트웨이 내부 이벤트를 실시간 수집하는 경량 in-memory 트래커.

### 타입

```go
type ProviderStats struct {
    TotalCalls     int     // 전체 호출 수
    Failures       int     // 실패 횟수
    Timeouts       int     // 타임아웃 횟수
    Fallbacks      int     // sglang 폴백 횟수
    AvgLatencyMs   float64 // 평균 응답 시간
    LastErrorAt    int64   // 마지막 에러 시각 (unix ms)
    LastError      string  // 마지막 에러 메시지
    ConsecutiveFail int    // 현재 연속 실패 카운트
}

type SessionMetrics struct {
    TotalRuns         int // 에이전트 실행 총 횟수
    ContextOverflows  int // context overflow 발생 횟수
    CompactionSuccess int // 컴팩션 성공
    CompactionFails   int // 컴팩션 실패
    SglangFallbacks   int // 프로바이더 실패 → sglang 폴백
}

type ErrorEvent struct {
    Ts       int64  // unix ms
    Source   string // "provider", "compaction", "session"
    Provider string // 관련 프로바이더 ID
    Message  string // 에러 메시지
}

type TrackerSnapshot struct {
    Providers    map[string]*ProviderStats `json:"providers"`
    Sessions     SessionMetrics            `json:"sessions"`
    RecentErrors []ErrorEvent              `json:"recentErrors"` // 최근 50건
}
```

### Public 메서드

```go
func NewTracker() *Tracker

// 이벤트 기록 (향후 chat/run.go에서 호출)
func (t *Tracker) RecordCall(provider string, latencyMs int64)
func (t *Tracker) RecordError(source, provider, msg string)
func (t *Tracker) RecordOverflow()
func (t *Tracker) RecordCompaction(success bool)
func (t *Tracker) RecordFallback(fromProvider string)

// 상태 조회
func (t *Tracker) ProviderHealthy(provider string) bool  // 실패율 < 30%
func (t *Tracker) Snapshot() TrackerSnapshot

// 즉시 알림 콜백 (연속 실패 3회 시 호출)
func (t *Tracker) SetAlertCallback(fn func(msg string))
```

### 수집 지점 (향후 연결)

| 위치 | 이벤트 | 호출 |
|------|--------|------|
| `chat/run.go:279` 뒤 | 성공 호출 | `tracker.RecordCall(providerID, elapsed)` |
| `chat/run.go:282` | context overflow | `tracker.RecordOverflow()` |
| `chat/run.go:287-290` | 컴팩션 결과 | `tracker.RecordCompaction(success)` |
| `chat/run.go:299` | sglang 폴백 | `tracker.RecordFallback(providerID)` |
| `chat/run.go:313` | 최종 에러 | `tracker.RecordError("provider", providerID, err)` |

연결 방법: `runDeps` 구조체에 `tracker *copilot.Tracker` 필드 추가 (nil-safe 호출).

---

## Layer 2: Advisor (`advisor.go`)

Tracker 데이터 기반으로 게이트웨이 동작 변경을 판단하는 어드바이저.

### Public 메서드

```go
func NewAdvisor(tracker *Tracker) *Advisor

// 프로바이더 강제 전환 판단
// 연속 실패 3회 이상이면 true → run.go에서 sglang으로 강제 라우팅
func (a *Advisor) ShouldForceSglang(provider string) bool

// 컴팩션 파라미터 조정
// 컴팩션 실패율 > 50%이면 더 공격적 파라미터 반환
// threshold: 0.85 → 0.75, freshTail: 32 → 16
func (a *Advisor) AdjustCompaction(threshold float64, freshTail int) (float64, int)

// 프로바이더 복구 처리 (5분 쿨다운 후 자동 해제)
func (a *Advisor) MarkRecovered(provider string)

// 현재 sglang 강제 전환 중인 프로바이더 목록
func (a *Advisor) ForcedProviders() []string
```

### 개입 지점 (향후 연결)

| 위치 | 개입 | 설명 |
|------|------|------|
| `chat/run.go:211-222` | 모델 선택 전 | `advisor.ShouldForceSglang(providerID)` true면 sglang 라우팅 |
| `chat/compaction.go:77` | 컴팩션 설정 | `advisor.AdjustCompaction(threshold, freshTail)` |

### 자동 복구 흐름

```
프로바이더 A 연속 실패 3회
  → ShouldForceSglang("A") = true
  → 텔레그램: "⚡ [자동 전환] A 연속 실패, sglang으로 전환"
  → 5분 후 자동 해제 시도
  → RecordCall("A", latency) 성공
  → 텔레그램: "✅ [복구] A 정상화, 원래 프로바이더 복원"
```

---

## Layer 3: Service 개선 (`service.go`)

### 알림 쿨다운 + 복구 알림

```go
// Service에 추가
prevResults    []CheckResult           // 이전 사이클 결과
alertCooldown  map[string]time.Time    // 체크별 마지막 알림 시각
```

**상태 전환 로직**:
- 이전 ok → 현재 warning/critical: 신규 알림 전송, 쿨다운 시작
- 이전 warning/critical → 현재 ok: 복구 알림 `"✅ [복구] {name}: {message}"`
- 쿨다운 중 (1시간 이내 동일 체크): 알림 스킵

### 병렬 체크 실행

```go
func (s *Service) executeChecks(ctx context.Context) []CheckResult {
    checks := []func(context.Context) CheckResult{...}
    results := make([]CheckResult, len(checks))
    var wg sync.WaitGroup
    for i, check := range checks {
        wg.Add(1)
        go func(idx int, fn func(context.Context) CheckResult) {
            defer wg.Done()
            results[idx] = fn(ctx)
        }(i, check)
    }
    wg.Wait()
    return results
}
```

### Tracker/Advisor 내장

```go
type Service struct {
    // ...기존 필드
    tracker  *Tracker  // 자동 생성, Tracker() 메서드로 외부 접근
    advisor  *Advisor  // 자동 생성, Advisor() 메서드로 외부 접근
}
```

`NewService()`에서 Tracker + Advisor 자동 생성. 외부에서 `svc.Tracker()`로 접근하여 `chat/run.go`에 전달.

---

## 신규 체크 (`checks.go`)

### checkMemoryUsage

`/proc/meminfo` 파싱. MemAvailable / MemTotal 비율.

| 조건 | 상태 |
|------|------|
| 가용 < 5% | critical |
| 가용 < 10% | warning |
| 스왑 > 80% | critical |
| 스왑 > 50% | warning |

### checkProviderHealth

`tracker.Snapshot().Providers` 기반. tracker nil이면 skip(ok 반환).

| 조건 | 상태 |
|------|------|
| 실패율 > 60% | critical |
| 실패율 > 30% | warning |
| 강제 sglang 전환 중 | warning + 해당 프로바이더 표시 |

### checkGatewayRuntime

`tracker.Snapshot().Sessions` 기반. tracker nil이면 skip.
에러 밀도/폴백 빈도가 높으면 `askLocalLLM`으로 패턴 분석.

---

## 텔레그램 `/copilot` 명령

### 파싱 (`slash_commands.go`)

```go
case "copilot":
    return &SlashResult{
        Handled: true,
        Command: "copilot",
        Args:    args, // "check" 또는 빈 문자열
    }
```

### 핸들링 (`chat.go`)

- `/copilot` → 상태 요약:
  ```
  🤖 Copilot 상태
  ━━━━━━━━━━━━━━
  시스템: ✅ GPU 정상 | ✅ 디스크 78% | ✅ 메모리 정상
  프로바이더: ✅ zai (99%) | ⚠️ anthropic → sglang 전환 중
  런타임: 세션 42회 | 컴팩션 3회 | 폴백 1회
  마지막 점검: 3분 전
  ```
- `/copilot check` → 즉시 점검 실행 후 결과 반환

`Handler`에 `copilotSvc *copilot.Service` 필드 추가 (nil-safe).
`HandlerConfig`에 `CopilotSvc *copilot.Service` 추가.

---

## 전체 데이터 흐름 (향후 완성 시)

```
chat/run.go ─── RecordCall/Error/Overflow/Fallback ──→ Tracker
chat/compaction.go ── RecordCompaction ──────────────→ (in-memory)
                                                          │
                                                          ▼
  ┌─────────── Advisor ←── ProviderHealthy() ──── Tracker.Snapshot()
  │               │
  │    ShouldForceSglang()          AdjustCompaction()
  │         │                              │
  ▼         ▼                              ▼
run.go     resolveClient 전              compaction.go sweepCfg 조정
  (sglang 강제 라우팅)                    (공격적 컴팩션)

Service ──── 15분 주기 ──→ checkProviderHealth() ──→ Tracker.Snapshot()
       │                 → checkGatewayRuntime() ──→ askLocalLLM 분석
       │                 → checkMemory/Disk/GPU/sglang/Process/Logs
       │
       ├── notifyIssues() ──→ Telegram (쿨다운 + 복구 알림)
       └── Tracker 즉시 콜백 ──→ Telegram (연속 실패 즉시 알림)

Telegram /copilot ──→ slash_commands.go ──→ chat.go ──→ Service.Status()
                                                        + Tracker.Snapshot()
```

---

## 수정 대상 파일

| 파일 | 변경 | 비고 |
|------|------|------|
| `copilot/types.go` | CheckStatus 상수, Tracker 타입, Config 확장 | 수정 |
| `copilot/tracker.go` | 이벤트 수집기 | **신규** |
| `copilot/advisor.go` | 자동 개입 어드바이저 | **신규** |
| `copilot/checks.go` | checkMemoryUsage, checkProviderHealth, checkGatewayRuntime | 수정 |
| `copilot/service.go` | 쿨다운, 복구 알림, 병렬 실행, Tracker/Advisor 내장 | 수정 |
| `chat/slash_commands.go` | `/copilot` 파싱 | 수정 |
| `chat/chat.go` | copilotSvc 필드 + handleSlashCommand copilot 분기 | 수정 |

**수정 불필요**: `chat/run.go`, `chat/compaction.go`, `server/server.go` (향후 연결 시 수정)

---

## 향후 연결 작업 (이 문서 범위 밖)

1. `chat/run.go`의 `runDeps`에 `tracker *copilot.Tracker` 추가 + 이벤트 기록 코드 삽입 (6곳)
2. `chat/run.go` 모델 선택 전 `advisor.ShouldForceSglang()` 호출 삽입
3. `chat/compaction.go`에 `advisor.AdjustCompaction()` 호출 삽입
4. `server/server.go`에서 `copilotSvc.Tracker()`를 `chat.HandlerConfig`에 전달
