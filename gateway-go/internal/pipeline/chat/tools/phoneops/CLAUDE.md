# Phoneops (phone_read / phone_write)

Owns the agent-facing phone tools. Parent `runtimeops` no longer implements
them — this package is the phone change cluster.

## Entry points

- `phone.go` — `ToolPhoneRead`, `ToolPhoneWrite`
- `phone_action.go` — P1 allowlist + `buildPhoneAction` / `dispatchPhoneAction`
- `phone_location.go` / `phone_usage.go` / `phone_calllog.go` — app-pushed caches
- `phone_messages.go` — `domain/phoneledger` digest for `what=messages`

## Dependency direction and invariants

- **Dependency / boundary**: may import `tooldeps`, `toolport`, `phoneledger`,
  `config`, `jsonutil`. Must not import parent `runtimeops`.
- **Invariant**: dispatch uses `tooldeps.PhoneActionFunc` only. A nil sender
  reports unavailable; `ErrPhoneActionUnconfirmed` is cautionary success, not
  a retry. The allowlist in `phoneActions` is closed — never emit an action
  outside it.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/phoneops
```
