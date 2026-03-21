# Deneb Modular Architecture Plan

## Executive Summary

This document proposes a phased modularization of the Deneb codebase to improve maintainability, testability, and developer velocity. The core issues are:

1. **Gateway monolith** — `src/gateway/` has 266 files (67K+ LOC), importing from nearly every other module
2. **Implicit module boundaries** — many `src/*` directories lack formal interfaces; consumers import deep internal paths
3. **Global state coupling** — plugin runtime, channel registry, and config share process-global singletons
4. **Extension SDK surface sprawl** — 30+ `plugin-sdk/*` subpaths with no stability tiers

The plan is organized into 5 phases, from low-risk structural improvements to full domain isolation.

---

## Current Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                        CLI Layer                         │
│  src/cli/  ─  program.ts, deps.ts, progress.ts          │
└───────────────────────┬─────────────────────────────────┘
                        │
┌───────────────────────▼─────────────────────────────────┐
│                    Gateway (monolith)                     │
│  src/gateway/  ─  266 files, 67K LOC                     │
│  server.impl.ts, server-methods/*, boot.ts, auth.ts      │
│  Imports: channels, plugins, auto-reply, routing,         │
│           infra, config, agents, media, sessions          │
└──┬────┬────┬────┬────┬────┬────┬────┬────┬──────────────┘
   │    │    │    │    │    │    │    │    │
   ▼    ▼    ▼    ▼    ▼    ▼    ▼    ▼    ▼
 channels plugins auto- routing infra config agents media sessions
          │       reply                         │
          ▼                                     ▼
    plugin-sdk ◄──────────────────────── extensions/*
```

### Key Metrics

| Module                | Files | Approx LOC | Imports From                    | Risk   |
| --------------------- | ----- | ---------- | ------------------------------- | ------ |
| `src/gateway/`        | 266   | 67K        | 9+ modules                      | HIGH   |
| `src/auto-reply/`     | ~40   | ~12K       | config, channels, agents, infra | MEDIUM |
| `src/infra/outbound/` | ~70   | ~18K       | gateway/call, channels, config  | MEDIUM |
| `src/plugins/`        | ~30   | ~8K        | config, media, tts, web-search  | LOW    |
| `src/routing/`        | ~20   | ~6K        | config, agents, channels        | MEDIUM |
| `src/channels/`       | ~25   | ~7K        | (minimal)                       | LOW    |
| `src/agents/`         | ~35   | ~10K       | config, routing                 | MEDIUM |
| `src/media/`          | ~44   | ~10K       | (minimal)                       | LOW    |
| `src/config/`         | ~20   | ~5K        | (minimal)                       | LOW    |

---

## Phase 1: Formal Module Boundaries (Low Risk)

**Goal:** Define explicit public APIs for each `src/*` module without moving files.

### 1.1 Add barrel exports (`index.ts`) for each module

Each top-level `src/*` directory gets an `index.ts` that re-exports only its public API. Internal files are not re-exported.

**Modules to add barrels:**

- `src/channels/index.ts` — `registerChannel`, `resolveChannel`, channel types
- `src/routing/index.ts` — `resolveRoute`, `SessionKey`, binding utilities
- `src/config/index.ts` — `loadConfig`, `ConfigSchema`, path utilities
- `src/agents/index.ts` — `AgentScope`, agent CRUD, auth utilities
- `src/media/index.ts` — `fetchMedia`, `convertMedia`, MIME utilities
- `src/auto-reply/index.ts` — `dispatchInboundMessage`, `createReplyDispatcher`
- `src/sessions/index.ts` — session store, session key types
- `src/infra/index.ts` — outbound send, archive, backup interfaces

**Rules:**

- Consumers import from `src/<module>/index.ts`, not deep paths
- ESLint `no-restricted-imports` rule enforces boundary (phase 2)
- Existing deep imports continue working during migration

### 1.2 Type-only boundary contracts

Create `src/<module>/types.ts` for each module's public types. Consumers use `import type` for cross-module type references.

```
src/channels/types.ts    — ChannelId, ChannelConfig, ChannelPlugin
src/routing/types.ts     — Route, SessionKey, Binding
src/agents/types.ts      — AgentScope, AgentConfig, AgentId
src/config/types.ts      — DenebConfig, ConfigPath
src/media/types.ts       — MediaRef, MediaFormat, MimeType
src/sessions/types.ts    — Session, SessionStore
```

### 1.3 Document dependency direction rules

```
Allowed dependency direction (top → bottom):

  CLI
   ↓
  Gateway
   ↓
  Commands
   ↓
  ┌──────────────────────────────────────┐
  │  auto-reply, infra/outbound, agents  │  (application services)
  └──────────┬───────────────────────────┘
             ↓
  ┌──────────────────────────────────────┐
  │  routing, channels, sessions, plugins│  (domain core)
  └──────────┬───────────────────────────┘
             ↓
  ┌──────────────────────────────────────┐
  │  config, media, shared, types, utils │  (foundation)
  └──────────────────────────────────────┘

Forbidden:
  - Foundation → Application Services
  - Domain Core → Application Services
  - Any module → Gateway (except CLI entry)
  - Extensions → anything except plugin-sdk/*
```

---

## Phase 2: Gateway Decomposition (Medium Risk)

**Goal:** Break `src/gateway/` from 266 files into 6-8 focused submodules.

### 2.1 Identify gateway subdomains

Based on file analysis, `src/gateway/` contains these distinct concerns:

| Subdomain       | Files                                       | Description                             |
| --------------- | ------------------------------------------- | --------------------------------------- |
| **auth**        | auth.ts, auth-\*.ts, rate-limit             | Authentication, API keys, rate limiting |
| **chat**        | chat-\*.ts, server-methods/chat.ts          | Chat session handling, abort, sanitize  |
| **channels**    | channel-\*.ts, server-methods/channels.ts   | Channel health, status, registration    |
| **agents**      | agent-_.ts, server-methods/agent_.ts        | Agent lifecycle, tools, events          |
| **models**      | model-\*.ts, server-methods/models.ts       | Model provider routing, fallback        |
| **sessions**    | session-\*.ts, server-methods/sessions.ts   | Session management, reset, history      |
| **server-core** | server.impl.ts, boot.ts, call.ts, ws-log.ts | HTTP server, WebSocket, boot sequence   |
| **config-api**  | server-methods/config.ts, connect.ts        | Config read/write API endpoints         |

### 2.2 Extract subdomains into subdirectories

```
src/gateway/
  auth/           ← auth.ts, auth-*.ts, rate-limit
  chat/           ← chat-*.ts, attachments, sanitize, abort
  channel-mgmt/   ← channel-health-*.ts, channel-status-*.ts
  agent-mgmt/     ← agent-*.ts, tools-*.ts
  model-routing/  ← model-*.ts, provider selection
  session-mgmt/   ← session-*.ts, reset, history
  server/         ← server.impl.ts, boot.ts, call.ts, ws
  index.ts        ← re-exports server boot + registration
```

Each subdomain:

- Has its own `index.ts` barrel
- Declares handler registration functions (e.g., `registerChatHandlers(server)`)
- Receives dependencies via function parameters, not global imports

### 2.3 Handler registration pattern

Replace monolithic server method wiring with composable registration:

```typescript
// src/gateway/server/boot.ts
export function bootGateway(deps: GatewayDeps) {
  const server = createServer(deps);
  registerAuthHandlers(server, deps);
  registerChatHandlers(server, deps);
  registerChannelHandlers(server, deps);
  registerAgentHandlers(server, deps);
  registerModelHandlers(server, deps);
  registerSessionHandlers(server, deps);
  return server;
}
```

---

## Phase 3: Dependency Injection Formalization (Medium Risk)

**Goal:** Replace global state and ad-hoc singletons with explicit dependency passing.

### 3.1 Formalize `GatewayDeps` interface

Extend the existing `createDefaultDeps` pattern into a typed dependency container:

```typescript
// src/gateway/deps.ts
export interface GatewayDeps {
  config: ConfigReader;
  channelRegistry: ChannelRegistry;
  pluginRuntime: PluginRuntime;
  sessionStore: SessionStore;
  routeResolver: RouteResolver;
  mediaService: MediaService;
  replyDispatcher: ReplyDispatcherFactory;
  agentService: AgentService;
  outboundSender: OutboundSender;
}
```

### 3.2 Eliminate process-global state

| Current Global                            | Replacement                            |
| ----------------------------------------- | -------------------------------------- |
| `GATEWAY_SUBAGENT_SYMBOL` on `globalThis` | Pass `PluginRuntime` via `GatewayDeps` |
| `registerChannel()` global map            | `ChannelRegistry` instance on deps     |
| Config singleton                          | `ConfigReader` instance on deps        |

### 3.3 Test isolation

Each gateway subdomain can be tested with mock deps:

```typescript
const mockDeps = createMockGatewayDeps({
  config: stubConfig({ channels: { telegram: { enabled: true } } }),
  channelRegistry: new InMemoryChannelRegistry(),
});
const result = await handleChat(request, mockDeps);
```

---

## Phase 4: Plugin SDK Stabilization (Medium Risk)

**Goal:** Tier the 30+ plugin-sdk subpaths and establish a versioning contract.

### 4.1 Stability tiers

```
Tier 1 — Stable (semver guarantees):
  deneb/plugin-sdk/channel-runtime
  deneb/plugin-sdk/channel-reply-pipeline
  deneb/plugin-sdk/reply-payload
  deneb/plugin-sdk/agent-runtime
  deneb/plugin-sdk/config-runtime

Tier 2 — Beta (may change in minor versions):
  deneb/plugin-sdk/reply-runtime
  deneb/plugin-sdk/media-runtime
  deneb/plugin-sdk/tools-runtime

Tier 3 — Internal (no stability guarantee):
  All other subpaths
```

### 4.2 SDK subpath consolidation

Reduce 30+ subpaths to ~10-12 by grouping related exports:

```
deneb/plugin-sdk/channel   ← merge channel-runtime, channel-reply-pipeline, channel types
deneb/plugin-sdk/agent     ← merge agent-runtime, agent tools, agent events
deneb/plugin-sdk/reply     ← merge reply-payload, reply-runtime, reply dispatch
deneb/plugin-sdk/config    ← merge config-runtime, config types
deneb/plugin-sdk/media     ← merge media-runtime, media types
deneb/plugin-sdk/core      ← shared types, utilities
```

### 4.3 Extension compliance validation

Add a build-time check that verifies extensions import only from `deneb/plugin-sdk/*`:

```bash
# scripts/check-extension-imports.ts
# Scans extensions/*/src/**/*.ts for forbidden imports
# Fails CI if any extension imports from src/** directly
```

---

## Phase 5: Workspace Package Extraction (High Risk, Long-term)

**Goal:** Extract foundational modules into workspace packages for independent versioning and faster builds.

### 5.1 Candidate packages

| Package           | Source                                    | Rationale                                  |
| ----------------- | ----------------------------------------- | ------------------------------------------ |
| `@deneb/config`   | `src/config/`                             | Pure data, no side effects, many consumers |
| `@deneb/media`    | `src/media/`                              | Self-contained pipeline, clear interface   |
| `@deneb/routing`  | `src/routing/`                            | Core domain logic, testable in isolation   |
| `@deneb/channels` | `src/channels/`                           | Registry + types, minimal deps             |
| `@deneb/shared`   | `src/shared/`, `src/types/`, `src/utils/` | Foundation utilities                       |

### 5.2 Package structure

```
packages/
  config/
    package.json
    src/
    tsconfig.json
  media/
    package.json
    src/
    tsconfig.json
  routing/
    ...
  channels/
    ...
  shared/
    ...
```

### 5.3 Migration strategy

1. Create package with its own `package.json` and `tsconfig.json`
2. Move source files
3. Update imports across the monorepo (use `pnpm` workspace protocol)
4. Verify with `pnpm build` — check for `[INEFFECTIVE_DYNAMIC_IMPORT]` warnings
5. Run full test suite

**Note:** This phase is optional and should only be pursued if phases 1-4 demonstrate clear benefits. The workspace overhead may not be justified for a single-product monorepo.

---

## Implementation Priority & Effort

| Phase                       | Effort             | Risk   | Value   | Priority |
| --------------------------- | ------------------ | ------ | ------- | -------- |
| 1. Module Boundaries        | Small (1-2 weeks)  | Low    | High    | **P0**   |
| 2. Gateway Decomposition    | Medium (3-4 weeks) | Medium | High    | **P0**   |
| 3. Dependency Injection     | Medium (2-3 weeks) | Medium | Medium  | **P1**   |
| 4. Plugin SDK Stabilization | Small (1-2 weeks)  | Medium | Medium  | **P1**   |
| 5. Workspace Extraction     | Large (4-6 weeks)  | High   | Low-Med | **P2**   |

---

## Success Criteria

- [ ] No `src/*` module imports from `src/gateway/` (except CLI entry)
- [ ] Gateway subdomains are independently testable with mock deps
- [ ] `pnpm build` passes with zero `[INEFFECTIVE_DYNAMIC_IMPORT]` warnings
- [ ] Extensions import only from `deneb/plugin-sdk/*`
- [ ] Each `src/*` module has a barrel `index.ts` with documented public API
- [ ] Circular dependency detector passes in CI
- [ ] Gateway file count per subdomain < 50 files
- [ ] Plugin SDK subpaths reduced from 30+ to ~12

---

## Risks & Mitigations

| Risk                                       | Impact | Mitigation                                                       |
| ------------------------------------------ | ------ | ---------------------------------------------------------------- |
| Import path changes break extensions       | HIGH   | Phase 1 keeps old paths working; lint rule added gradually       |
| Gateway decomposition causes regressions   | MEDIUM | Move files without logic changes; test before/after each move    |
| DI overhead slows hot paths                | LOW    | Profile before/after; keep DI at initialization, not per-request |
| Plugin SDK consolidation breaks extensions | HIGH   | Provide deprecated re-export shims for 2 minor versions          |
| Multi-agent conflicts during refactor      | MEDIUM | Scope PRs to single subdomain; coordinate via branch naming      |

---

## Next Steps

1. **Immediately:** Add barrel `index.ts` files for `channels`, `config`, `routing`, `media`
2. **This sprint:** Map all gateway files to subdomains (spreadsheet/issue)
3. **Next sprint:** Begin gateway auth + chat extraction
4. **Ongoing:** Add `no-restricted-imports` lint rules as modules stabilize
