---
summary: "Multi-language gateway architecture: Go server, Rust core (CGo FFI)"
read_when:
  - Working on gateway protocol, clients, or transports
  - Understanding the Go/Rust runtime split
title: "Gateway Architecture"
---

# Gateway architecture

Last updated: 2026-04-01

## Overview

Deneb runs as a multi-language gateway with two cooperating runtimes:

- **Go gateway** — the primary server process. Handles HTTP/WS, RPC dispatch, session state, auth, cron, and daemon management.
- **Rust core** (`core-rs`) — high-performance library linked into Go via CGo FFI. Handles protocol validation, security (constant-time compare, HTML sanitization, SSRF checks), media detection, markdown parsing, memory search (cosine similarity, BM25, hybrid merge), and context engine (assembly, compaction, sweep).

A single long-lived Gateway owns all messaging surfaces (Telegram is the primary channel).

<Tip>
Telegram is the primary production channel. Other channels exist in the
codebase but may not receive the same depth of optimization. See
[design philosophy](/concepts/design-philosophy) for details.
</Tip>

Control-plane clients (CLI, web UI, automations) connect over **WebSocket** on the configured bind host (default `127.0.0.1:18789`).

**Nodes** (Android/headless) also connect over **WebSocket**, but declare `role: node` with explicit caps/commands.

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

    RPC --> Proto
    RPC --> Security
    RPC --> Media
    RPC --> Markdown
    RPC --> Memory
    RPC --> Context
```

### IPC boundaries

| Path             | Transport                           | Use case                                                               |
| ---------------- | ----------------------------------- | ---------------------------------------------------------------------- |
| Go to Rust       | CGo FFI (in-process)                | Protocol validation, security, media, markdown, memory, context engine |
| CLI to Gateway   | WebSocket                           | Commands, agent runs, status                                           |
| Protobuf schemas | Shared source of truth              | Cross-language type definitions (`proto/*.proto`)                      |

## Full system overview

End-to-end view of every major component and how data flows from messaging channels through the gateway to LLM providers and back.

```mermaid
graph TB
    subgraph "Messaging Channels"
        TG["📱 Telegram<br>(primary channel)"]
    end

    subgraph "Control Plane Clients"
        CLI["CLI (cli-rs)<br>Rust → WebSocket"]
        WEB["Web Admin UI"]
    end

    subgraph "Nodes"
        AND["Android Node"]
        HL["Headless Node"]
    end

    subgraph DGX["NVIDIA DGX Spark"]
        subgraph GW["Go Gateway (gateway-go)"]
            HTTP["HTTP Server<br>/health, /api/v1/rpc,<br>OpenAI API, Responses API"]
            WSS["WebSocket Server"]
            RPC["RPC Dispatcher<br>130+ methods"]
            SESS["Session Manager<br>IDLE→RUNNING→DONE"]
            AUTH["Auth + Pairing"]
            CHAT["Chat Pipeline<br>system prompt, tools,<br>context files"]
            CRON["Cron Scheduler"]
            CHAN["Channel Registry<br>Plugin interface"]
        end

        subgraph FFI["Rust Core (CGo FFI)"]
            PROTO["Protocol Validation"]
            SEC["Security<br>sanitize, SSRF"]
            MED["Media Detection<br>21 formats"]
            MD["Markdown Parser"]
            MEM["Memory Search<br>SIMD cosine, BM25"]
            CTX["Context Engine<br>Aurora assembly"]
            CMP["Compaction<br>sweep engine"]
            PARSE["Parsing<br>URL, HTML→MD"]
        end

        subgraph VEGA["Vega Search Engine"]
            FTS["SQLite FTS5"]
        end

        SKILLS["Skills (15)<br>github, weather,<br>coding-agent, tmux,<br>summarize..."]
        PROTO_SCHEMA["Proto Schemas<br>gateway.proto<br>channel.proto<br>session.proto"]
    end

    subgraph "LLM Providers"
        ANTH["Anthropic"]
        OAI["OpenAI"]
        GGL["Google"]
        LOCAL["Local GGUF<br>(on-device)"]
    end

    %% Channel → Gateway
    TG -->|"Bot API"| CHAN

    %% Control plane
    CLI -->|"WebSocket"| WSS
    WEB -->|"WebSocket"| WSS

    %% Nodes
    AND -->|"WS role:node"| WSS
    HL -->|"WS role:node"| WSS

    %% Internal gateway flow
    WSS --> RPC
    HTTP --> RPC
    RPC --> AUTH
    RPC --> SESS
    SESS --> CHAT
    CHAT --> CRON
    CHAN --> RPC

    %% FFI calls
    RPC --> PROTO
    RPC --> SEC
    RPC --> MED
    CHAT --> MD
    CHAT --> MEM
    CHAT --> CTX
    CHAT --> CMP
    CHAT --> PARSE

    %% Vega
    MEM --> FTS

    %% Proto schema codegen
    PROTO_SCHEMA -.->|"codegen"| PROTO
    PROTO_SCHEMA -.->|"codegen"| RPC

    %% LLM
    CHAT -->|"API calls"| ANTH
    CHAT -->|"API calls"| OAI
    CHAT -->|"API calls"| GGL
    CHAT -->|"local inference"| LOCAL
```

## Gateway internal architecture

Detailed view of the Go gateway's internal package structure and request processing pipeline.

```mermaid
graph LR
    subgraph "Inbound"
        REQ_HTTP["HTTP Request<br>/api/v1/rpc"]
        REQ_WS["WebSocket Frame<br>type: req"]
        REQ_CHAN["Channel Message<br>Telegram"]
    end

    subgraph "gateway-go/internal"
        subgraph server["server/"]
            SRV["HTTP + WS Listener<br>:18789"]
            HEALTH["/health endpoint"]
            OAIAPI["OpenAI-compat API"]
            RESPAPI["Responses API"]
        end

        subgraph auth["auth/"]
            TOKEN["Token Auth"]
            ALLOW["Allowlists"]
            PAIR["Device Pairing"]
        end

        subgraph rpc["rpc/"]
            DISPATCH["Method Dispatcher<br>thread-safe registry"]
        end

        subgraph session["session/"]
            LIFECYCLE["Lifecycle State Machine"]
            EVENTS["Event Pub/Sub Bus"]
            STATES["IDLE → RUNNING →<br>DONE / FAILED /<br>KILLED / TIMEOUT"]
        end

        subgraph channel["channel/"]
            REGISTRY["Plugin Registry"]
            PLUGIN["Plugin Interface<br>Meta + Capabilities"]
            CHANMGR["Lifecycle Manager<br>start / stop / health"]
        end

        subgraph chat["chat/"]
            SYSPROMPT["System Prompt<br>Assembly"]
            TOOLS["Tool Registry<br>exec, read, write,<br>edit, grep, find, ls, web"]
            CTXFILES["Context Files<br>CLAUDE.md, SOUL.md,<br>TOOLS.md, IDENTITY.md,<br>USER.md, MEMORY.md"]
            SLASH["Slash Commands<br>/reset /status /kill<br>/model /think"]
            SILENT["Silent Reply<br>NO_REPLY detection"]
        end

        subgraph llm["llm/"]
            SAMPLING["Sampling Params<br>top_p, top_k, penalties"]
            MULTIMOD["Multimodal<br>ImageSource"]
        end

        subgraph ffi["ffi/"]
            CGO["CGo Bindings<br>7 *_cgo.go files"]
            NOFFI["No-FFI Fallbacks<br>*_noffi.go"]
        end

        subgraph vega_int["vega/"]
            VEGAEXEC["Vega Execute"]
            VEGASRCH["Vega Search"]
            AUTODET["Model Autodetect<br>~/.deneb/models/*.gguf"]
        end
    end

    subgraph "Rust Core (libdeneb_core.a)"
        RUSTFNS["deneb_validate_frame<br>deneb_constant_time_eq<br>deneb_detect_mime<br>deneb_sanitize_html<br>deneb_is_safe_url<br>deneb_vega_execute<br>deneb_vega_search<br>deneb_memory_cosine_similarity<br>deneb_memory_bm25_rank_to_score"]
    end

    subgraph "External"
        LLM_EXT["LLM Providers<br>Anthropic, OpenAI,<br>Google, Local"]
    end

    %% Inbound routing
    REQ_HTTP --> SRV
    REQ_WS --> SRV
    REQ_CHAN --> REGISTRY

    %% Server → Auth → RPC
    SRV --> TOKEN
    TOKEN --> DISPATCH

    %% RPC → Session → Chat
    DISPATCH --> LIFECYCLE
    LIFECYCLE --> SYSPROMPT
    SYSPROMPT --> CTXFILES
    SYSPROMPT --> TOOLS
    SYSPROMPT --> SLASH

    %% Chat → LLM
    TOOLS --> SAMPLING
    SAMPLING --> LLM_EXT

    %% Channel flow
    REGISTRY --> PLUGIN
    PLUGIN --> CHANMGR
    CHANMGR --> DISPATCH

    %% FFI
    DISPATCH --> CGO
    CGO --> RUSTFNS

    %% Vega
    DISPATCH --> VEGAEXEC
    VEGAEXEC --> CGO
    VEGASRCH --> CGO
    AUTODET -.-> VEGAEXEC

    %% Events
    LIFECYCLE --> EVENTS
    EVENTS -->|"streaming"| SRV
```

## Rust core crate architecture

The `core-rs/` workspace contains 3 crates.

```mermaid
graph TB
    subgraph "deneb-core (main crate)"
        direction TB
        LIBRS["lib.rs<br>30+ C FFI exports<br>(deneb_* functions)"]

        subgraph modules["Core Modules"]
            PROTO_M["protocol/<br>Frame validation<br>RequestFrame, ResponseFrame,<br>EventFrame, ErrorShape"]
            SEC_M["security/<br>constant_time_eq<br>sanitize_html<br>is_safe_url (SSRF)<br>is_valid_session_key"]
            MED_M["media/<br>Magic-byte MIME detection<br>21 formats, OOXML, ISOBMFF"]
            MEM_M["memory_search/<br>SIMD cosine similarity<br>BM25, FTS query builder<br>hybrid merge, MMR diversity"]
            MD_M["markdown/<br>pulldown-cmark parser<br>code spans, fences, tables"]
            CTX_M["context_engine/<br>Aurora assembly<br>DAG-aware token budgeting<br>handle-based FFI"]
            CMP_M["compaction/<br>Sweep state machine<br>chunk selection<br>threshold evaluation"]
            PARSE_M["parsing/<br>URL extraction<br>HTML-to-Markdown<br>base64, media tokens"]
        end

        LIBRS --> PROTO_M
        LIBRS --> SEC_M
        LIBRS --> MED_M
        LIBRS --> MEM_M
        LIBRS --> MD_M
        LIBRS --> CTX_M
        LIBRS --> CMP_M
        LIBRS --> PARSE_M
    end

    subgraph "deneb-vega"
        VEGA_CORE["SQLite FTS5<br>Search Engine"]
        VEGA_CMD["Commands<br>search, brief, changelog,<br>dashboard"]
        VEGA_DB["DB Layer<br>schema, importer,<br>parser, classifier"]
        VEGA_SEARCH["Search Pipeline<br>query analysis → FTS5 →<br>semantic → fusion/rerank"]
    end

    subgraph "deneb-agent-runtime"
        ART_MODEL["Model Selection<br>provider normalization<br>catalog, thinking levels"]
        ART_SCOPE["Scope Resolution<br>agent registry<br>session key parsing"]
        ART_SUB["Subagent Lifecycle<br>Pending → Running → Terminal"]
        ART_USAGE["Usage Tracking<br>multi-provider normalization"]
    end

    subgraph "Feature Flags"
        F_DEFAULT["default<br>(napi_binding)"]
        F_VEGA["vega"]
    end

    %% Crate dependencies
    LIBRS -->|"optional"| VEGA_CORE

    %% Feature flag chain
    F_DEFAULT -.-> F_VEGA

    subgraph "Build Targets"
        B_RUST["make rust<br>(minimal, no vega)"]
        B_VEGA["make rust-vega<br>(FTS)"]
    end

    F_DEFAULT -.-> B_RUST
    F_VEGA -.-> B_VEGA

    subgraph "Proto Schemas (proto/)"
        PGW["gateway.proto<br>ErrorCode, RequestFrame,<br>ResponseFrame, EventFrame"]
        PCH["channel.proto<br>ChannelCapabilities,<br>ChannelMeta"]
        PSS["session.proto<br>SessionRunStatus,<br>SessionKind"]
    end

    %% Proto → codegen
    PGW -->|"prost-build"| PROTO_M
    PCH -->|"prost-build"| PROTO_M
    PSS -->|"prost-build"| PROTO_M

    subgraph "Output Artifacts"
        STATIC["libdeneb_core.a<br>(staticlib → Go CGo)"]
        RLIB["rlib<br>(workspace internal)"]
    end

    LIBRS --> STATIC
    LIBRS --> RLIB
```

## Components and flows

### Gateway (Go process)

- Primary server process; starts HTTP and WebSocket listeners.
- Dispatches RPC methods through a thread-safe registry (130+ built-in methods).
- Manages session lifecycle (state machine: RUNNING, DONE, FAILED, KILLED, TIMEOUT).
- Runs auth middleware with scope-based authorization.
- Calls Rust core functions via CGo FFI for CPU-intensive operations.

### Clients (CLI / web admin)

- One WS connection per client.
- Send requests (`health`, `status`, `send`, `agent`, `system-presence`).
- Subscribe to events (`tick`, `agent`, `presence`, `shutdown`).

### Nodes (Android / headless)

- Connect to the **same WS server** with `role: node`.
- Provide a device identity in `connect`; pairing is **device-based** (role `node`) and approval lives in the device pairing store.
- Expose commands like `canvas.*`, `camera.*`, `screen.record`, `location.get`.

Protocol details: [Gateway protocol](/gateway/protocol)

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
- Supervision: systemd for auto-restart.

## Invariants

- Exactly one Gateway per host.
- Go gateway is the primary process; Node.js plugin host is a managed subprocess.
- Rust core functions are called in-process via CGo FFI (zero IPC overhead).
- Handshake is mandatory; any non-JSON or non-connect first frame is a hard close.
- Events are not replayed; clients must refresh on gaps.
