# serverwire

Composition-root package for GatewayHub RPC wiring.

- `Ports` — explicit dependency bag Server fills (`wirePorts`)
- `early/` — early-phase Handler Deps registration
- `late/` — late-phase (chat-dependent) registration

Must not import `runtime/server`. See `docs/agent-rules/hub-wiring.md`.
