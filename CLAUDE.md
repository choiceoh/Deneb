# CLAUDE.md

Guidance for AI agents (and humans) working in this repo. Read this first — it
maps the architecture, the conventions, and the two recipes you'll use most
(add a resource, add a pane). Keep it current when you change structure.

## What this is

**Andromeda** — a personal AI work-command **workstation**, the desktop cockpit
for the Deneb gateway (a single-agent personal assistant). Three columns: nav +
work area (grids/editors) + Deneb AI collaboration. Data and AI come from the
Deneb gateway over `miniapp.*` RPC, `chat/stream` (SSE), and `events` (SSE).

Stack: **Tauri 2** (Rust desktop shell) + **React 18** + **Refine** (headless
admin framework) + **Vite**. Full design rationale: [`docs/DESIGN.md`](docs/DESIGN.md).

## Commands

```bash
pnpm verify        # typecheck + lint + format:check + test + build — run before pushing
pnpm dev           # Vite dev server (web) on :1420
pnpm test          # Vitest (jsdom) — pnpm test:watch for watch mode
pnpm typecheck     # tsc --noEmit
pnpm lint          # eslint .
pnpm format        # prettier --write .   (format:check to verify only)
pnpm build         # tsc && vite build (web bundle → dist/)
pnpm tauri:dev     # run the desktop shell (needs Rust + system GUI libs)
```

**Always run `pnpm verify` before pushing.** CI (`.github/workflows/ci.yml`) runs
the same steps on every PR.

## Architecture & data flow

```
Gateway (Deneb)  ──miniapp.* RPC──▶  gateway.ts (callRpc)
                                          │
                       ┌──────────────────┼───────────────────┐
                       ▼                   ▼                   ▼
              dataProvider.ts        chatStream()         events.ts
            (Refine CRUD glue)      (AI SSE stream)    (proactive SSE)
                       │                   │                   │
                  resources.ts          hooks.ts (useChat / useEvents / useGatewayStatus)
            (resource↔RPC registry)        │
                       ▼                   ▼
         Refine useList/useCreate…    components/  (Workstation · Sidebar · AIPanel ·
                       │                   ProactivePanel · Grid · panes/*)
                       ▼                   │
                 panes/*  ◀──── workspaceContext.tsx (active pane pushes its
                                 AI-text projection + backing resource)
```

### File responsibilities (`src/`)

| File                   | Role                                                                                                                                                                     |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `gateway.ts`           | Raw gateway client: `callRpc`, `ping`, `chatStream`, token/config storage. The only place that talks HTTP to the gateway (plus `events.ts`).                             |
| `events.ts`            | Proactive push SSE client (`subscribeEvents`).                                                                                                                           |
| `dataProvider.ts`      | Refine `DataProvider` — generic CRUD→RPC glue, **derived from `resources.ts`**.                                                                                          |
| `resources.ts`         | **Single source of truth** for resource↔RPC mapping + Refine resource metadata. Query-driven RPCs (memory/search) live here as constants, not CRUD.                      |
| `authProvider.ts`      | Token-based Refine auth provider (DESIGN §4).                                                                                                                            |
| `workspaceContext.tsx` | Shared workstation state + the mechanism where the active pane publishes its serialized content to the AI panel (`useRegisterPane`). Holds `cfg` for query-driven panes. |
| `hooks.ts`             | `useChat`, `useEvents`, `useGatewayStatus`.                                                                                                                              |
| `log.ts`               | Namespaced, leveled logger (see Logging).                                                                                                                                |
| `format.ts`            | Pure display helpers (`text`, `fmtDate`, `calSpan`, `errText`). No React.                                                                                                |
| `theme.ts`             | Design tokens + shared style objects. **No hardcoded colors in components.**                                                                                             |
| `types.ts`             | Domain row types + the `View` union (pane keys).                                                                                                                         |
| `components/`          | `Workstation` (3-col shell), `Sidebar` (nav + connect), `AIPanel`, `ProactivePanel`, `Grid` (+`GridNotice`), `ErrorBoundary`.                                            |
| `components/panes/`    | One file per pane + `index.ts` = the **pane registry** (`PANES`).                                                                                                        |

## Recipe: add a resource-backed grid pane

The registry makes this declarative. To add e.g. a `notes` grid:

1. **`resources.ts`** — add to `RESOURCE_DEFS`:
   ```ts
   { name: "notes", label: "노트", list: "miniapp.notes.list", create: "miniapp.notes.create" }
   ```
2. **`types.ts`** — add `Note` row interface and add `"notes"` to the `View` union.
3. **`components/panes/NotePane.tsx`** — copy `PeoplePane.tsx` (the simplest read
   grid): `useList<Note>({ resource: "notes", queryOptions: { enabled: connected } })`,
   build `aiText`, call `useRegisterPane("notes", aiText)`, render `<GridNotice><Grid…/></GridNotice>`.
4. **`components/panes/index.ts`** — add one `PANES` entry `{ key, label, shortcut, Component }`.

Nav button, ⌘-shortcut, rendering, and AI context all follow automatically.

## Recipe: add a query-driven (non-CRUD) pane

For search-like panes (input → RPC → results), don't use the data provider. Copy
`SearchPane.tsx`: pull `cfg` from `useWorkspace()`, call `callRpc(cfg, "<rpc>", params)`
directly, manage local state, and `useRegisterPane(undefined, aiText)`. Add the RPC
name to `resources.ts` as a constant. See `WikiPane.tsx` for read+edit+save.

## Conventions

- **TypeScript strict.** `any` only at the dynamic RPC boundary (`dataProvider.ts`,
  `callRpc<T>`), never in UI code.
- **Logging, not `console`.** `import { log } from "./log"; const x = log.child("foo");`
  then `x.debug/info/warn/error`. Raise verbosity at runtime:
  `localStorage.setItem("andromeda.logLevel","debug")`.
- **Styling via `theme.ts`** tokens (`color`, `field`, `line`, `navButton`, …). No
  inline hex.
- **The AI sees text, not pixels.** Each pane serializes its content to text and
  pushes it via `useRegisterPane`; the AI panel reads that. Keep new panes doing this.
- **Errors:** surface via `GridNotice` (lists) or let `ErrorBoundary` catch render
  crashes. Use `errText()` for messages.
- **Formatting/lint** are enforced in CI. Run `pnpm format` before committing.

## Testing

- Vitest + jsdom + Testing Library; no browser needed. Co-locate `*.test.ts(x)`.
- Pure logic (`format`, `resources`, `log`, `events`) → direct unit tests.
- UI → `src/test/util.tsx`: `renderWithProviders(ui, { connected, dataProvider })`
  with `fakeProvider(fixtures)` to drive grids without a gateway. See `App.test.tsx`.
- Add/extend tests with every behavior change; `pnpm verify` must stay green.

## Gotchas / environment

- **No live gateway in CI/sandbox.** RPC-dependent behavior can't be E2E-verified
  here; the disconnected/error UI paths are what tests exercise. Field names in
  panes are best-effort vs DESIGN §5 — reconcile against the real gateway.
- **Tauri:** full `cargo`/`tauri build` needs system GUI libs (webkit2gtk-4.1) that
  the dev sandbox lacks; only config/structure/dep-resolution are verifiable here.
  `@tauri-apps/api` is dynamically imported so the web build never pulls it.
- **pnpm supply-chain wrapper:** this pnpm distribution (locally _and_ in CI) gates
  build scripts and fails `install` until each is approved in `pnpm-workspace.yaml`
  (`allowBuilds:`). esbuild is already approved there. Add new ones the same way.

## Roadmap (DESIGN §8)

Phase 0–1 done (connection, workstation MVP, mail/calendar/todo grids, two-way AI).
Phase 2 in progress (wiki editor, unified search, more resources — skeletons in).
Foundation laid: tests/CI, Tauri shell, events layer, logging, lint. **Next:** capture (OCR/ASR), then dashboards/multi-window (Phase 3).
