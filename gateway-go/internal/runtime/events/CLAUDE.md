# Gateway events broadcast map

Owns SSE/event fan-out for the gateway: `Broadcaster` delivery, typed
`Publisher` helpers, and gateway subscription routing. Payloads at the
package boundary are `EventPayload` (opaque JSON bytes).

## Entry points

- `broadcaster.go` — `NewBroadcaster`, `Broadcast`, `BroadcastWithOpts`,
  `BroadcastToConnIDs`, `Subscribe`, `RegisterTap`
- `publisher.go` — `NewPublisher`, `Publisher` session/agent helpers
- `gateway_subscriptions.go` — `NewGatewayEventSubscriptions`,
  `AgentEvent`, `TranscriptUpdate`
- `event_payload.go` — `EventPayload`, `PayloadFromRaw`, `PayloadOf`

## Dependency direction and invariants

- **Dependency direction / port boundary**: events is a leaf transport
  package — it must not import chat, wiki, or handler packages. Callers
  marshal typed payloads to `EventPayload` (`PayloadOf`) before `Broadcast`.
- **Invariant**: subscriber filters must never panic a broadcast loop;
  taps observe after encode and must tolerate bad payloads; session
  message delivery only reaches connIDs explicitly subscribed to that
  session key; tool-event recipients are scoped by runID registration.
- Prefer `Publisher` helpers over ad-hoc payload maps at call sites so
  event names stay a single source of truth.

## Local change scope

Keep transport changes local to this package.

- May co-change: `runtime/rpc` hub wiring that constructs the broadcaster,
  and SSE gateway HTTP adapters.
- Do not touch: chat run lifecycle policy, wiki Store, or miniapp RPC
  method handlers (they consume events, they do not own them).

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/runtime/events
```
