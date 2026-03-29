---
summary: "RPC adapters for external CLIs (signal-cli) and gateway patterns"
read_when:
  - Adding or changing external CLI integrations
  - Debugging RPC adapters (signal-cli)
title: "RPC Adapters"
---

# RPC adapters

Deneb integrates external CLIs via JSON-RPC.

## Pattern A: HTTP daemon (signal-cli)

- `signal-cli` runs as a daemon with JSON-RPC over HTTP.
- Event stream is SSE (`/api/v1/events`).
- Health probe: `/api/v1/check`.
- Deneb owns lifecycle when `channels.signal.autoStart=true`.

See the Signal channel for setup and endpoints.

## Adapter guidelines

- Gateway owns the process (start/stop tied to provider lifecycle).
- Keep RPC clients resilient: timeouts, restart on exit.
- Prefer stable IDs (e.g., `chat_id`) over display strings.
