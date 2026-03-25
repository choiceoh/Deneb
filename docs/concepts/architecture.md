---
summary: "Multi-language gateway architecture: Go server, Rust core, Node.js plugin host"
read_when:
  - Working on gateway protocol, clients, or transports
  - Understanding the Go/Rust/TypeScript runtime split
title: "Gateway Architecture"
---

# Gateway architecture

Last updated: 2026-03-25

## Overview

Deneb runs as a multi-language gateway with three cooperating runtimes:

- **Go gateway** — the primary server process. Handles HTTP/WS, RPC dispatch, session state, auth, cron, and daemon management.
- **Rust core** (`core-rs`) — high-performance library linked into Go via CGo FFI. Handles protocol validation, security (constant-time compare, HTML sanitization, SSRF checks), media detection, markdown parsing, memory search (cosine similarity, BM25, hybrid merge), and context engine (assembly, compaction, sweep).
- **Node.js plugin host** — subprocess managed by the Go gateway over a Unix domain socket. Runs channel extensions, skill plugins, and provider integrations that use the TypeScript plugin SDK.

A single long-lived Gateway owns all messaging surfaces (Telegram via grammY, Discord, Slack, WhatsApp via Baileys, Signal, iMessage, WebChat, and extension channels).

Control-plane clients (macOS app, CLI, web UI, automations) connect over **WebSocket** on the configured bind host (default `127.0.0.1:18789`).

**Nodes** (macOS/iOS/Android/headless) also connect over **WebSocket**, but declare `role: node` with explicit caps/commands.

One Gateway per host. The **canvas host** is served under `/__deneb__/canvas/` and `/__deneb__/a2ui/` on the same port.

## Runtime architecture

```mermaid
graph TB
    subgraph "Go Gateway (primary process)"
        HTTP["HTTP Server<br>/health, /api/v1/rpc"]
        WS["WebSocket Server<br>clients, nodes"]
        RPC["RPC Dispatcher<br>100+ methods"]
        Session["Session Manager<br>lifecycle state machine"]
        Auth["Auth Middleware<br>scope-based RBAC"]
        Cron["Cron Scheduler"]
    end

    subgraph "Rust Core (CGo FFI)"
        Proto["Protocol Validation"]
        Security["Security<br>sanitize, SSRF, constant-time"]
        Media["Media Detection<br>MIME, 21 formats"]
        Markdown["Markdown Parser<br>pulldown-cmark"]
        Memory["Memory Search<br>SIMD cosine, BM25"]
        Context["Context Engine<br>assembly, compaction"]
    end

    subgraph "Node.js Plugin Host (subprocess)"
        Plugins["Channel Extensions"]
        Skills["Skill Plugins"]
        Providers["Provider Integrations"]
        SDK["Plugin SDK"]
    end

    RPC --> Proto
    RPC --> Security
    RPC --> Media
    RPC --> Markdown
    RPC --> Memory
    RPC --> Context
    RPC -->|"Unix socket<br>frame protocol"| Plugins
```

### IPC boundaries

| Path             | Transport                           | Use case                                                               |
| ---------------- | ----------------------------------- | ---------------------------------------------------------------------- |
| Go to Rust       | CGo FFI (in-process)                | Protocol validation, security, media, markdown, memory, context engine |
| Go to Node.js    | Unix domain socket + frame protocol | Channel extensions, skills, provider integrations                      |
| CLI to Gateway   | WebSocket                           | Commands, agent runs, status                                           |
| Protobuf schemas | Shared source of truth              | Cross-language type definitions (`proto/*.proto`)                      |

### Hardware-aware runtime

The gateway detects available hardware at startup and selects a **hardware profile** that tunes concurrency, memory, and acceleration:

| Profile              | Detection                       | Agent concurrency | Embedding batch | FFmpeg accel      |
| -------------------- | ------------------------------- | ----------------- | --------------- | ----------------- |
| **DGX Spark (GB10)** | NVIDIA Grace Blackwell GPU      | 10                | 8               | CUDA (h264_nvenc) |
| **Desktop GPU**      | Consumer NVIDIA (RTX 3090/4090) | 8                 | 6               | CUDA (h264_nvenc) |
| **CPU-only**         | No NVIDIA GPU detected          | 4                 | 2               | Software          |

GPU detection runs `nvidia-smi` at startup; override with `DENEB_GPU_ACCEL` env var (`dgx-spark`, `cuda`, `none`).

Each profile also tunes: V8 heap size, SQLite cache/mmap, UV threadpool, compute pool size (for worker threads), FFmpeg buffer/timeout, and image worker count.

## Components and flows

### Gateway (Go process)

- Primary server process; starts HTTP and WebSocket listeners.
- Dispatches RPC methods through a thread-safe registry (100+ built-in methods).
- Manages session lifecycle (state machine: IDLE, RUNNING, DONE, FAILED, KILLED, TIMEOUT).
- Runs auth middleware with scope-based authorization.
- Spawns and supervises the Node.js plugin host subprocess.
- Calls Rust core functions via CGo FFI for CPU-intensive operations.

### Plugin host (Node.js subprocess)

- Communicates with Go gateway over Unix domain socket using a frame-based protocol (RequestFrame/ResponseFrame).
- Runs channel extensions (Telegram, Discord, Slack, WhatsApp, etc.).
- Executes skill plugins and provider integrations via the TypeScript plugin SDK.
- Auto-reconnects with exponential backoff (1s to 30s max) if the connection drops.

### Clients (macOS app / CLI / web admin)

- One WS connection per client.
- Send requests (`health`, `status`, `send`, `agent`, `system-presence`).
- Subscribe to events (`tick`, `agent`, `presence`, `shutdown`).

### Nodes (macOS / iOS / Android / headless)

- Connect to the **same WS server** with `role: node`.
- Provide a device identity in `connect`; pairing is **device-based** (role `node`) and approval lives in the device pairing store.
- Expose commands like `canvas.*`, `camera.*`, `screen.record`, `location.get`.

Protocol details: [Gateway protocol](/gateway/protocol)

### WebChat

- Static UI that uses the Gateway WS API for chat history and sends.
- In remote setups, connects through the same SSH/Tailscale tunnel as other clients.

## Connection lifecycle (single client)

```mermaid
sequenceDiagram
    participant Client
    participant Gateway

    Client->>Gateway: req:connect
    Gateway-->>Client: res (ok)
    Note right of Gateway: or res error + close
    Note left of Client: payload=hello-ok<br>snapshot: presence + health

    Gateway-->>Client: event:presence
    Gateway-->>Client: event:tick

    Client->>Gateway: req:agent
    Gateway-->>Client: res:agent<br>ack {runId, status:"accepted"}
    Gateway-->>Client: event:agent<br>(streaming)
    Gateway-->>Client: res:agent<br>final {runId, status, summary}
```

## Wire protocol (summary)

- Transport: WebSocket, text frames with JSON payloads.
- First frame **must** be `connect`.
- After handshake:
  - Requests: `{type:"req", id, method, params}` → `{type:"res", id, ok, payload|error}`
  - Events: `{type:"event", event, payload, seq?, stateVersion?}`
- If `DENEB_GATEWAY_TOKEN` (or `--token`) is set, `connect.params.auth.token`
  must match or the socket closes.
- Idempotency keys are required for side-effecting methods (`send`, `agent`) to
  safely retry; the server keeps a short-lived dedupe cache.
- Nodes must include `role: "node"` plus caps/commands/permissions in `connect`.

## Pairing + local trust

- All WS clients (operators + nodes) include a **device identity** on `connect`.
- New device IDs require pairing approval; the Gateway issues a **device token**
  for subsequent connects.
- **Local** connects (loopback or the gateway host's own tailnet address) can be
  auto-approved to keep same-host UX smooth.
- All connects must sign the `connect.challenge` nonce.
- Signature payload `v3` also binds `platform` + `deviceFamily`; the gateway
  pins paired metadata on reconnect and requires repair pairing for metadata
  changes.
- **Non-local** connects still require explicit approval.
- Gateway auth (`gateway.auth.*`) still applies to **all** connections, local or
  remote.

Details: [Gateway protocol](/gateway/protocol), [Pairing](/channels/pairing),
[Security](/gateway/security).

## Protocol typing and codegen

- **Protobuf schemas** (`proto/`) are the cross-language source of truth for frame types.
- Generated outputs: Go (`gateway-go/pkg/protocol/gen/`), Rust (prost, `OUT_DIR`), TypeScript (`src/protocol/generated/`).
- TypeBox schemas define the WebSocket protocol surface.
- JSON Schema is generated from TypeBox schemas.
- Swift models are generated from the JSON Schema.
- Generation: `make proto` (parallel Go + Rust + TS).

## Remote access

- Preferred: Tailscale or VPN.
- Alternative: SSH tunnel

  ```bash
  ssh -N -L 18789:127.0.0.1:18789 user@gateway-host
  ```

- The same handshake + auth token apply over the tunnel.
- TLS + optional pinning can be enabled for WS in remote setups.

## Operations snapshot

- Start: `deneb gateway` (foreground, logs to stdout).
- Health: `health` over WS (also included in `hello-ok`), or `GET /health` over HTTP.
- Supervision: launchd/systemd for auto-restart.

## Invariants

- Exactly one Gateway per host.
- Go gateway is the primary process; Node.js plugin host is a managed subprocess.
- Rust core functions are called in-process via CGo FFI (zero IPC overhead).
- Handshake is mandatory; any non-JSON or non-connect first frame is a hard close.
- Events are not replayed; clients must refresh on gaps.
