# Typing

> 70 nodes · cohesion 0.05

## Key Concepts

- **TypingController** (12 connections) — `gateway-go/internal/pipeline/autoreply/typing/typing.go`
- **.Len()** (8 connections) — `gateway-go/internal/ai/localai/queue.go`
- **.SignalTextDelta()** (8 connections) — `gateway-go/internal/pipeline/autoreply/typing/typing_mode.go`
- **.StartTypingLoop()** (8 connections) — `gateway-go/internal/pipeline/autoreply/typing/typing.go`
- **.Push()** (7 connections) — `gateway-go/internal/ai/localai/queue.go`
- **requestQueue** (7 connections) — `gateway-go/internal/ai/localai/queue.go`
- **bge-m3-server.py** (7 connections) — `scripts/deploy/bge-m3-server.py`
- **FullTypingSignaler** (7 connections) — `gateway-go/internal/pipeline/autoreply/typing/typing_mode.go`
- **NewTypingController()** (7 connections) — `gateway-go/internal/pipeline/autoreply/typing/typing.go`
- **.Signal()** (7 connections) — `gateway-go/internal/pipeline/autoreply/typing/typing.go`
- **embed()** (6 connections) — `scripts/deploy/bge-m3-server.py`
- **typing_mode.go** (6 connections) — `gateway-go/internal/pipeline/autoreply/typing/typing_mode.go`
- **TestPriorityQueue()** (6 connections) — `gateway-go/internal/ai/localai/hub_test.go`
- **TestQueueDropOldestBackground()** (6 connections) — `gateway-go/internal/ai/localai/hub_test.go`
- **queueHeap** (6 connections) — `gateway-go/internal/ai/localai/queue.go`
- **.PopWait()** (6 connections) — `gateway-go/internal/ai/localai/queue.go`
- **newRequestQueue()** (6 connections) — `gateway-go/internal/ai/localai/queue.go`
- **NewFullTypingSignaler()** (6 connections) — `gateway-go/internal/pipeline/autoreply/typing/typing_mode.go`
- **TestFullTypingSignaler_SignalRunStart()** (6 connections) — `gateway-go/internal/pipeline/autoreply/typing/typing_mode_test.go`
- **TestFullTypingSignaler_SignalRunStart_Never()** (6 connections) — `gateway-go/internal/pipeline/autoreply/typing/typing_mode_test.go`
- **TestFullTypingSignaler_SignalTextDelta_FiltersSilentReply()** (6 connections) — `gateway-go/internal/pipeline/autoreply/typing/typing_mode_test.go`
- **TestFullTypingSignaler_SignalTextDelta_StartsOnRealText()** (6 connections) — `gateway-go/internal/pipeline/autoreply/typing/typing_mode_test.go`
- **.Stop()** (6 connections) — `gateway-go/internal/pipeline/autoreply/typing/typing.go`
- **queue.go** (5 connections) — `gateway-go/internal/ai/localai/queue.go`
- **typing.go** (5 connections) — `gateway-go/internal/pipeline/autoreply/typing/typing.go`
- *... and 45 more nodes in this community*

## Relationships

- No strong cross-community connections detected

## Source Files

- `gateway-go/internal/ai/localai/hub_test.go`
- `gateway-go/internal/ai/localai/queue.go`
- `gateway-go/internal/core/corecache/lru.go`
- `gateway-go/internal/pipeline/autoreply/typing/typing.go`
- `gateway-go/internal/pipeline/autoreply/typing/typing_mode.go`
- `gateway-go/internal/pipeline/autoreply/typing/typing_mode_test.go`
- `gateway-go/internal/platform/media/fetch.go`
- `scripts/deploy/bge-m3-server.py`

## Audit Trail

- EXTRACTED: 169 (60%)
- INFERRED: 112 (40%)
- AMBIGUOUS: 0 (0%)

---

*Part of the graphify knowledge wiki. See [[index]] to navigate.*