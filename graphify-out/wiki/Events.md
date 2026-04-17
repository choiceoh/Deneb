# Events

> 128 nodes · cohesion 0.03

## Key Concepts

- **.Subscribe()** (27 connections) — `gateway-go/internal/runtime/session/events.go`
- **NewBroadcaster()** (25 connections) — `gateway-go/internal/runtime/events/broadcaster.go`
- **.Broadcast()** (22 connections) — `gateway-go/internal/runtime/rpc/rpcutil/gateway_hub.go`
- **Broadcaster** (20 connections) — `gateway-go/internal/runtime/events/broadcaster.go`
- **.BroadcastWithOpts()** (16 connections) — `gateway-go/internal/runtime/events/broadcaster.go`
- **broadcaster_test.go** (14 connections) — `gateway-go/internal/runtime/events/broadcaster_test.go`
- **EventsMethods()** (11 connections) — `gateway-go/internal/runtime/rpc/handler/handlerevents/events.go`
- **GatewayEventSubscriptions** (11 connections) — `gateway-go/internal/runtime/events/gateway_subscriptions.go`
- **TestUnsubscribe_CleansUpSessionSubs()** (10 connections) — `gateway-go/internal/runtime/events/broadcaster_test.go`
- **.BroadcastRaw()** (10 connections) — `gateway-go/internal/runtime/events/broadcaster.go`
- **newTestGatewaySubscriptions()** (10 connections) — `gateway-go/internal/runtime/events/gateway_subscriptions_test.go`
- **.emit()** (10 connections) — `gateway-go/internal/pipeline/chat/streaming/streaming.go`
- **TestStreamBroadcasterEvents()** (10 connections) — `gateway-go/internal/pipeline/chat/streaming/streaming_test.go`
- **Broadcaster** (9 connections) — `gateway-go/internal/pipeline/chat/streaming/streaming.go`
- **.runTranscriptLoop()** (8 connections) — `gateway-go/internal/runtime/events/gateway_subscriptions.go`
- **Publisher** (8 connections) — `gateway-go/internal/runtime/events/publisher.go`
- **.PublishSessionMessage()** (8 connections) — `gateway-go/internal/runtime/events/publisher.go`
- **NewGatewayEventSubscriptions()** (8 connections) — `gateway-go/internal/runtime/events/gateway_subscriptions.go`
- **TestGatewaySubscriptions_EmitTranscript_WithSessionEventSub()** (8 connections) — `gateway-go/internal/runtime/events/gateway_subscriptions_test.go`
- **.ID()** (8 connections) — `gateway-go/internal/ai/provider/runtime_test.go`
- **NewService()** (8 connections) — `gateway-go/internal/platform/cron/service.go`
- **.BroadcastToConnIDs()** (7 connections) — `gateway-go/internal/runtime/events/broadcaster.go`
- **.SessionEventSubscriberConnIDs()** (7 connections) — `gateway-go/internal/runtime/events/broadcaster.go`
- **.SubscribeSessionEvents()** (7 connections) — `gateway-go/internal/runtime/events/broadcaster.go`
- **.publishSessionChanged()** (7 connections) — `gateway-go/internal/runtime/events/publisher.go`
- *... and 103 more nodes in this community*

## Relationships

- No strong cross-community connections detected

## Source Files

- `gateway-go/internal/ai/provider/registry.go`
- `gateway-go/internal/ai/provider/runtime_test.go`
- `gateway-go/internal/pipeline/chat/streaming/streaming.go`
- `gateway-go/internal/pipeline/chat/streaming/streaming_test.go`
- `gateway-go/internal/platform/cron/service.go`
- `gateway-go/internal/runtime/events/broadcaster.go`
- `gateway-go/internal/runtime/events/broadcaster_test.go`
- `gateway-go/internal/runtime/events/gateway_subscriptions.go`
- `gateway-go/internal/runtime/events/gateway_subscriptions_test.go`
- `gateway-go/internal/runtime/events/publisher.go`
- `gateway-go/internal/runtime/events/publisher_test.go`
- `gateway-go/internal/runtime/rpc/handler/handlerevents/events.go`
- `gateway-go/internal/runtime/rpc/handler/handlerevents/events_test.go`
- `gateway-go/internal/runtime/rpc/rpcutil/gateway_hub.go`
- `gateway-go/internal/runtime/session/events.go`

## Audit Trail

- EXTRACTED: 307 (46%)
- INFERRED: 357 (54%)
- AMBIGUOUS: 0 (0%)

---

*Part of the graphify knowledge wiki. See [[index]] to navigate.*