# Single-Core Interrupt: 작업 중 빠른 응답

## 한 줄 요약

에이전트 루프의 **턴 사이**에 끼어들어, 이미 캐싱된 컨텍스트로 빠르게 응답하고, 작업을 이어서 계속한다.

## 배경

현재 구현 (듀얼코어): 작업 중 메시지가 오면 별도 LLM 호출을 병렬로 실행.
- 시스템 프롬프트/컨텍스트/클라이언트를 처음부터 재구축 → 5~15초
- 작업코어가 대화 내용을 못 봄 (별도 호출이라서)
- race condition 방어에 800+ LOC

싱글코어 인터럽트: 에이전트 루프 안에서 처리.
- 이미 빌드된 system prompt, messages, client 재사용 → 1~3초
- 대화가 messages 배열에 남아서 작업코어가 다음 턴에서 봄
- 싱글 goroutine이라 race condition 없음

## 현재 에이전트 루프 구조

`gateway-go/internal/chat/agent.go` — `RunAgent()`:

```
for turn := 0; turn < MaxTurns; turn++ {
    ① LLM 스트리밍 호출 (consumeStream — blocking, 수초~수십초)
    ② stop 조건 체크 (end_turn이면 리턴)
    ③ assistant 블록을 messages에 추가
    ④ 도구 병렬 실행 (wg.Wait() — blocking, 수초~수십초)
    ⑤ ctx 취소 체크
    ⑥ tool_result를 messages에 추가
}
```

시그니처:
```go
func RunAgent(
    ctx context.Context,
    cfg AgentConfig,
    messages []llm.Message,
    client *llm.Client,
    tools ToolExecutor,
    hooks StreamHooks,
    logger *slog.Logger,
    runLog *agentlog.RunLogger,
) (*AgentResult, error)
```

## 설계

### 1. InterruptBox — 메시지 전달 채널

```go
// gateway-go/internal/chat/interrupt_box.go (NEW)

// PendingMessage는 작업 중 도착한 사용자 메시지.
type PendingMessage struct {
    Message    string
    Delivery   *DeliveryContext
    ReceivedAt time.Time
}

// InterruptBox는 Send()와 에이전트 루프 사이의 메시지 전달 채널.
// 세션당 하나. 용량 1 — 새 메시지가 오면 이전 pending을 교체한다.
type InterruptBox struct {
    mu     sync.Mutex
    ch     chan PendingMessage
    closed bool
}
```

**API:**
- `NewInterruptBox() *InterruptBox` — 생성
- `Offer(msg PendingMessage)` — 메시지 넣기. 이전 pending이 있으면 drain 후 교체.
- `Poll() (PendingMessage, bool)` — non-blocking 꺼내기. 없으면 false.
- `Close()` — 채널 닫기. 에이전트 런 종료 시 호출.

**설계 결정:**
- 용량 1: 작업 중 메시지 여러 개 오면 **최신 것만** 처리. 오래된 건 transcript에 persist만 하고 응답은 안 함.
- Offer에서 이전 메시지 drain: `select { case <-ib.ch: default: }` 후 새 메시지 push.

### 2. Handler 변경 — `chat.go`

Handler에 추가:
```go
interruptBoxMu sync.RWMutex
interruptBoxes map[string]*InterruptBox  // sessionKey → box
```

메서드:
```go
func (h *Handler) getInterruptBox(sessionKey string) *InterruptBox
func (h *Handler) setInterruptBox(sessionKey string, box *InterruptBox)
func (h *Handler) clearInterruptBoxIfOwner(sessionKey string, box *InterruptBox)
```

### 3. Send() 라우팅 변경 — `chat.go`

```go
func (h *Handler) Send(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
    // ... validation, slash command 처리 (기존 그대로) ...

    // 작업 중이면 → InterruptBox 라우팅
    if box := h.getInterruptBox(p.SessionKey); box != nil {
        // 명시적 중단 키워드: /kill, 그만, 중단 등 → 기존 interrupt 흐름
        if isExplicitInterrupt(p.Message) {
            h.InterruptActiveRun(p.SessionKey)
            return h.startAsyncRun(req.ID, runParams, false)
        }

        // 일반 메시지: InterruptBox에 넣고 즉시 리턴
        box.Offer(PendingMessage{
            Message:    sanitizeInput(p.Message),
            Delivery:   p.Delivery,
            ReceivedAt: time.Now(),
        })

        resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
            "status": "queued_for_interrupt",
        })
        return resp
    }

    // 작업 없으면 → 기존 흐름
    h.InterruptActiveRun(p.SessionKey)
    return h.startAsyncRun(req.ID, runParams, false)
}
```

`isExplicitInterrupt(msg string) bool` — 기존 route_classifier의 슬래시 커맨드 + 키워드 + 부정 체크를 인라인 함수로 축소. 별도 파일 불필요.

### 4. startAsyncRun() 변경 — `chat.go`

InterruptBox를 생성하고 RunAgent에 전달:

```go
func (h *Handler) startAsyncRun(...) {
    // ... 기존 session 생성, abort entry 등록 ...

    box := NewInterruptBox()
    h.setInterruptBox(params.SessionKey, box)

    go func() {
        defer runCancel()
        defer h.cleanupAbort(params.ClientRunID)
        defer box.Close()
        defer h.clearInterruptBoxIfOwner(params.SessionKey, box)
        // ...
        runAgentAsync(runCtx, params, deps, box)  // box 전달
    }()
}
```

### 5. RunAgent() 변경 — `agent.go`

시그니처에 `interruptBox *InterruptBox` 추가:

```go
func RunAgent(
    ctx context.Context,
    cfg AgentConfig,
    messages []llm.Message,
    client *llm.Client,
    tools ToolExecutor,
    hooks StreamHooks,
    logger *slog.Logger,
    runLog *agentlog.RunLogger,
    interruptBox *InterruptBox,  // NEW — nil이면 인터럽트 비활성
) (*AgentResult, error)
```

인터럽트 체크포인트 2곳:

```go
for turn := 0; turn < cfg.MaxTurns; turn++ {
    // ▶ 체크포인트 B: 턴 시작 전 (도구 없는 작업에서도 체크)
    messages = handlePendingInterrupt(ctx, cfg, messages, client, hooks, logger, interruptBox, iDeps)

    // ... LLM 호출, consumeStream ...
    // ... stop 조건 체크 ...
    // ... 도구 병렬 실행, wg.Wait() ...

    // ▶ 체크포인트 A: 도구 실행 완료 후 (가장 자연스러운 지점)
    messages = handlePendingInterrupt(ctx, cfg, messages, client, hooks, logger, interruptBox, iDeps)

    messages = append(messages, llm.NewBlockMessage("user", toolResults))
}
```

### 6. handlePendingInterrupt() — 빠른 응답 핵심 함수

`agent.go`에 추가:

```go
// interruptDeps는 인터럽트 응답에 필요한 의존성.
// RunAgent 호출 시 context value로 전달하거나, 별도 struct로 넘긴다.
type interruptDeps struct {
    transcript TranscriptStore
    replyFunc  ReplyFunc
    sessionKey string
}

func handlePendingInterrupt(
    ctx context.Context,
    cfg AgentConfig,
    messages []llm.Message,
    client *llm.Client,
    hooks StreamHooks,
    logger *slog.Logger,
    box *InterruptBox,
    deps interruptDeps,
) []llm.Message {
    if box == nil {
        return messages
    }
    pending, ok := box.Poll()
    if !ok {
        return messages
    }

    // 1. user message를 messages에 추가
    messages = append(messages, llm.NewTextMessage("user", pending.Message))

    // 2. 도구 없는 단일 LLM 호출 — 같은 client, 같은 system prompt (캐시 히트)
    quickReq := llm.ChatRequest{
        Model:     cfg.Model,
        Messages:  messages,
        System:    cfg.System,   // 이미 빌드된 시스템 프롬프트
        MaxTokens: 4096,
        Tools:     nil,          // 도구 없음 — 대화만
        Stream:    true,
    }

    var events <-chan llm.StreamEvent
    var err error
    if cfg.APIType == "anthropic" {
        events, err = client.StreamChat(ctx, quickReq)
    } else {
        events, err = client.StreamChatOpenAI(ctx, quickReq)
    }
    if err != nil {
        logger.Warn("interrupt response: LLM call failed", "error", err)
        return messages
    }

    // 3. 스트리밍 소비 (기존 hooks 재사용 → Telegram에 실시간 전달)
    result, err := consumeStream(ctx, events, hooks)
    if err != nil || result.text == "" {
        return messages
    }

    // 4. assistant 응답을 messages에 추가 (작업코어가 다음 턴에서 봄)
    messages = append(messages, llm.NewTextMessage("assistant", result.text))

    // 5. Transcript에 저장
    now := time.Now().UnixMilli()
    if deps.transcript != nil {
        deps.transcript.Append(deps.sessionKey, ChatMessage{
            Role: "user", Content: pending.Message, Timestamp: now,
        })
        deps.transcript.Append(deps.sessionKey, ChatMessage{
            Role: "assistant", Content: result.text, Timestamp: now,
        })
    }

    // 6. Telegram 최종 전송
    if deps.replyFunc != nil && pending.Delivery != nil {
        replyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        _ = deps.replyFunc(replyCtx, pending.Delivery, result.text)
    }

    logger.Info("interrupt response delivered",
        "chars", len(result.text),
        "pendingAge", time.Since(pending.ReceivedAt).Milliseconds(),
    )

    return messages
}
```

### 7. 파이프라인 연결 — `run.go`

`executeAgentRun()`에서 interruptDeps를 context에 넣거나 직접 전달:

```go
func executeAgentRun(..., box *InterruptBox, ...) {
    // ... 기존 system prompt 빌드, context 조립, client 해석, tools 빌드 ...

    // RunAgent 호출 시 box 전달
    agentResult, runErr = RunAgent(ctx, cfg, messages, client, deps.tools, hooks, logger, runLog, box)
}
```

`runAgentAsync()` 시그니처도 box를 받도록 수정:
```go
func runAgentAsync(ctx context.Context, params RunParams, deps runDeps, box *InterruptBox)
```

interruptDeps 전달: RunAgent 내부에서 context value로 꺼내거나, handlePendingInterrupt에 직접 전달. 방법은 구현자 재량. Context value가 깔끔:
```go
ctx = withInterruptDeps(ctx, interruptDeps{
    transcript: deps.transcript,
    replyFunc:  deps.replyFunc,
    sessionKey: params.SessionKey,
})
```

### 8. 듀얼코어 코드 제거

| 파일 | 조치 |
|------|------|
| `concurrent_response.go` | **삭제** |
| `task_progress.go` | **삭제** |
| `task_progress_test.go` | **삭제** |
| `route_classifier.go` | **삭제** (isExplicitInterrupt로 축소, chat.go에 인라인) |
| `route_classifier_test.go` | **삭제** → interrupt 테스트로 대체 |
| `dualcore_test.go` | **삭제** → interrupt 테스트로 대체 |

Handler에서 제거:
- `taskProgressMu`, `taskProgress` 맵
- `concRespMu`, `concRespCancel` 맵
- `getTaskProgress`, `setTaskProgress`, `clearTaskProgressIfOwner`
- `CancelConcurrentResponse`
- `startConcurrentResponse`

### 9. 다른 호출자 시그니처 맞춤

RunAgent 시그니처가 바뀌므로 모든 호출자 수정:
- `run.go:executeAgentRun()` → box 전달
- `run.go:executeAgentRunWithDelta()` → box=nil (동기 호출이라 인터럽트 불필요)
- `send_sync.go:SendSync()` → box=nil
- `send_sync.go:SendSyncStream()` → box=nil

### 10. isExplicitInterrupt — 인라인 분류

```go
// chat.go에 인라인
var interruptKeywords = []string{"중단", "그만", "멈춰", "취소", "중지", "스톱", "stop", "cancel", "abort", "kill"}
var negationSuffixes = []string{"하지 마", "하지마", "지 마", "지마", "말고", "말아"}

func isExplicitInterrupt(message string) bool {
    lower := strings.ToLower(strings.TrimSpace(message))
    // 슬래시 커맨드
    for _, prefix := range []string{"/kill", "/stop", "/reset", "/new"} {
        if strings.HasPrefix(lower, prefix) { return true }
    }
    // 짧은 메시지 (≤30 rune) + 키워드 + 부정 아님
    if utf8.RuneCountInString(lower) <= 30 {
        for _, kw := range interruptKeywords {
            if strings.Contains(lower, kw) && !isNegated(lower, kw) {
                return true
            }
        }
    }
    return false
}
```

## 대기 시간

| 상황 | 대기 시간 | 이유 |
|------|----------|------|
| LLM 스트리밍 중 메시지 도착 | ~수초~30초 | 현재 턴의 스트리밍이 끝나야 체크 |
| 도구 실행 중 메시지 도착 | ~수초~10초 | wg.Wait()가 끝나야 체크 |
| 도구 실행 직후 메시지 도착 | ~1~3초 | 즉시 체크 → LLM 1회 호출 |
| 도구 없는 턴 사이 | ~1~3초 | 턴 시작 전 체크 → LLM 1회 호출 |

**최악 40초, 일반 3~8초, 최상 1~3초.**
듀얼코어의 5~15초보다 일반적으로 빠르고, 최악이 더 느릴 수 있지만 대부분의 실사용에서는 도구 실행 중에 메시지가 옴 (사용자가 도구 반응을 보고 질문하므로).

## 핵심 이점

1. **작업코어가 대화를 본다** — messages 배열에 Q&A가 남아서 다음 턴에 반영
2. **Anthropic 캐시 히트** — system prompt + tools가 이미 캐시됨
3. **race condition 없음** — 싱글 goroutine, 순차 처리
4. **코드량 감소** — 듀얼코어 800+ LOC 삭제, ~200 LOC 추가
5. **자연스러운 transcript** — 인터리빙 없이 시간순 기록

## 테스트 시나리오

1. 작업 없을 때 메시지 → 기존 흐름 (회귀 없음)
2. 도구 실행 중 메시지 → 도구 완료 후 빠른 응답 → 작업 계속
3. 연속 메시지 2개 → InterruptBox가 최신만 유지, 1개만 응답
4. `/kill` → 기존 InterruptActiveRun
5. "중단" → 기존 InterruptActiveRun
6. "중단하지 마" → InterruptBox로 전달 (부정 감지)
7. 작업 완료 직전 메시지 → box.Close() 후 Poll 실패, 다음 Send에서 정상 처리
8. SendSync/SendSyncStream → box=nil, 인터럽트 비활성 (회귀 없음)
