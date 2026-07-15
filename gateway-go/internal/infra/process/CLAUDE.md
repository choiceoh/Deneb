# Process manager map

Owns tracked subprocess execution, stream buffers, and env sanitization for
gateway tools/RPC.

## Entry points

- `manager.go` — `NewManager`, `Manager`, `ExecRequest`, `ExecResult`,
  `TrackedProcess`, `ApprovalCallback`
- `stream_buffer.go` — `NewStreamBuffer`, `StreamBuffer`
- `env_blocklist.go` — `SanitizeEnv`

## Dependency direction and invariants

- **Dependency / boundary**: infra leaf — must not import chat or domain
  wiki. Approval is injected via `ApprovalCallback`; do not call RPC from
  inside the manager.
- **Invariant**: every tracked process must be waitable/killable; stream
  buffers are capacity-bounded; `SanitizeEnv` must strip blocklisted keys
  before spawn; never return unsanitized env to callers.
- Approval denials must fail closed (no process start).

## Local change scope

Process lifecycle stays in `infra/process`.

- May co-change: tool exec wrappers and approval domain callbacks.
- Do not touch: chat tool registry wiring beyond the exec tool surface.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/infra/process
```
