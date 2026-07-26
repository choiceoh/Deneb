# Proactive delivery map

Owns native/workfeed proactive delivery: relay into sessions, alert dedupe, and
card title/summary extraction. Native-client push fan-out lives in
`runtime/nativepush` and is injected as a port. Composition roots wire `Deps`;
this package must stay a leaf relative to `runtime/server`.

## Entry points

- `proactive_relay.go` — `NewRelay`, `Relay`, `RelayCollapsed`, `RelayNative`,
  `PublishDeliverable` (session/workfeed delivery paths)
- `alert_gate.go` — `NewAlertGate`, `AlertGate.ShouldRelay` (dedupe gate)
- `workfeed_title_llm.go` — `CardTitleSummary` / `EvaluateCardTitleRole`

## Dependency direction and invariants

- **Dependency direction**: import domain/push, session, workfeed, and
  `pipeline/chat/denebui` for fence parsing — never import `runtime/server`
  or RPC handlers. Server injects stores through `Deps`.
- **Invariant**: narration-only / empty deliverables must never create
  workfeed cards; `AlertGate` must allow only one first-sighting relay per
  condition; native push stays behind the injected `nativepush.Hub` port.
- Prefer collapsing cards via `RelayCollapsed` when the payload is already a
  deneb-ui fence — do not re-parse HTML in the relay hot path.

## Local change scope

Keep delivery policy changes inside this package plus the injected ports.

- May co-change: `runtime/nativepush`, `domain/workfeed`,
  `pipeline/chat/denebui`, and the server wiring that builds `Deps`.
- Do not touch: chat turn execution (`pipeline/chat` run_*), wiki Store APIs,
  or genesis acceptance.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/runtime/proactive
```
