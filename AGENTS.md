# Repository Guidelines

- Repo: https://github.com/deneb/deneb
- In chat replies, file references must be repo-root relative only (example: `extensions/bluebubbles/src/channel.ts:80`); never absolute paths or `~/...`.
- Do not edit files covered by security-focused `CODEOWNERS` rules unless a listed owner explicitly asked for the change or is already reviewing it with you. Treat those paths as restricted surfaces, not drive-by cleanup.

## Project Structure & Module Organization

### Top-Level Directory Map

- `src/` — core application source (TypeScript, ESM).
- `core-rs/` — Rust core library (protocol validation, security, media). Builds as cdylib + staticlib + rlib.
- `gateway-go/` — Go gateway server scaffolding (HTTP/WS server, RPC dispatch, session management, channel registry, Node.js bridge).
- `native/` — napi-rs native addon for Node.js (gitignore matching, EXIF, PNG encoding).
- `proto/` — shared Protobuf schemas (gateway frames, channel types, session models). Source of truth for cross-language types.
- `extensions/` — channel and feature plugin packages (each is an independent package).
- `ui/` — Lit-based web control UI (separate pnpm workspace).
- `vega/` — Python project management tool (models, router, database, CLI).
- `skills/` — user-facing skill plugins (github, weather, summarize, coding-agent, etc.).
- `docs/` — Mintlify documentation site. Built output lives in `dist/`.
- `scripts/` — build, dev, CI, audit, and release scripts.
- `test/` — shared test utilities, fixtures, mocks, and helpers.
- `apps/` — mobile/desktop app projects (`apps/ios/`, `apps/android/`, `apps/macos/`).
- `.agents/skills/` — maintainer agent skills (release, GHSA, PR, Parallels smoke).
- `.github/` — CI workflows, custom actions, issue/PR templates, labeler, CODEOWNERS.
- `patches/` — pnpm dependency patches (patched deps must use exact versions).
- `dist/`, `dist-runtime/` — build output (generated, not committed).
- `bin/` — binary entry points (`deneb.mjs`).
- `git-hooks/` — pre-commit hooks.
- `Makefile` — multi-language build orchestration (Rust + Go + TypeScript + protobuf).
- Tests: colocated `*.test.ts` alongside source files; Rust tests inline `#[cfg(test)]`; Go tests `*_test.go`.

### Core Source (`src/`) Architecture

**Entry & exports:**

- `entry.ts` — primary binary entry point; environment validation, process respawn.
- `index.ts` — main exports barrel (CLI surface + library).
- `library.ts` — public library API (config loading, session management, port utilities).

**CLI system** (`src/cli/`):

- `program/` — command tree builder, command registry, registration modules per domain.
- Subcli modules: `gateway-cli/`, `daemon-cli/`, `autonomous-cli/`, `cron-cli/`, `browser-cli*.ts`, `nodes-cli/`, `update-cli/`, `send-runtime/`.
- Core utilities: argv parsing, channel auth/options, config CLI, shell completions, progress output.

**Commands** (`src/commands/`):

- Command handlers organized by feature: agent CRUD, channels setup/status, configure wizards, onboarding flows, status/health/doctor diagnostics, backup, auth/OAuth, models, sessions, dashboard.

**Gateway** (`src/gateway/`):

- Central message broker and execution engine (~150 files).
- Server core: HTTP routing, WebSocket runtime, RPC method registry.
- Session management: lifecycle state machine, history, patching, reset, kill.
- Auth: token/profile auth, device pairing, probe auth, RBAC, input allowlisting.
- Chat/LLM: attachments, sanitization, abort, OpenAI API bridge, Open Responses.
- Execution: node invocation with approval workflow, tool invocation over HTTP, hooks.
- Control UI: admin dashboard backend, routing, CSP, device metadata.
- Monitoring: channel health, live image probe, reconnect gating, self-watchdog.

**Channels framework** (`src/channels/`):

- Channel-agnostic core: plugin registry, session envelope, targeting, config schema loading.
- Message flow: state machine, typing indicators/lifecycle, reply formatting.
- Access control: mention gating, command gating, allowlists (`allowlists/`).
- Transport: message transport layer, stall watchdog, draft streaming, inbound debounce.
- Thread bindings: conversation binding policies.

**Built-in channels:**

- `src/telegram/`, `src/discord/`, `src/slack/`, `src/signal/`, `src/imessage/`, `src/web/` (WhatsApp web).

**Agents** (`src/agents/`):

- Agent runtime, command routing, scoping, file paths.
- Bash/exec tools: execution with approval workflow, host isolation, process management.
- Spawning: ACP spawn protocol for sub-agents, CLI runner, embedded Pi runtime.
- Auth profiles, API key rotation, auth health monitoring.
- Schema definitions, skill execution, command handling.

**Plugins** (`src/plugins/`):

- Plugin loading, discovery, manifest registry.
- Provider system: catalog, runtime, auth (OAuth/API key), model definitions, discovery, validation.
- Hooks: execution, wiring, integration points.
- Installation, update, uninstall, bundled plugin sources.
- Config schema, schema validation, feature toggles.

**Plugin SDK** (`src/plugin-sdk/`):

- Public extension API surface (~130 files, 160+ subpath exports in `package.json`).
- Channel SDK: lifecycle, config, runtime, reply pipeline, setup.
- Provider SDK: auth, setup, runtime, catalog.
- Media/speech runtime: media understanding, image generation, TTS.
- Gateway/agent runtime, ACP/hook runtime.
- Utilities: secret input, webhook processing, JSON store, SSRF policy, testing helpers.

**Infrastructure** (`src/infra/`):

- Environment: env vars, OS detection, machine ID, hardware profile, WSL/Tailscale detection.
- Process: command execution (with approvals/safety), safe binary policies, shell integration, process respawn/tracking.
- File system: file ops, boundary checking, path utilities, gitignore, archives, backups.
- Networking: net utilities, port detection, mDNS/Bonjour, HTTP client, WebSocket, SSH, TLS.
- Package management: plugin/package install, npm utilities, PM detection, Homebrew.
- State: JSON file handling, state migrations, dotenv loading.
- Reliability: backoff, retry, deduplication, heartbeat, locking, temp file handling.

**Config** (`src/config/`): configuration I/O, schema validation, type definitions, migrations.

**Routing** (`src/routing/`): session key resolution, account binding, route resolution.

**Media** (`src/media/`): media fetch/parse, MIME detection, FFmpeg execution, image ops, audio processing, PDF extraction, base64 encoding, input validation, outbound attachments.

**Other subsystems:**

- `src/memory/` — conversation memory storage and retrieval.
- `src/context-engine/` — LLM context management.
- `src/autonomous/` — agent autonomy features.
- `src/auto-reply/` — reply templates, heartbeat generation.
- `src/hooks/` — hook definitions and bundled hooks (system commands, etc.).
- `src/browser/` — headless browser automation.
- `src/canvas-host/` — A2UI canvas rendering (bundled via `pnpm canvas:a2ui:bundle`).
- `src/cron/` — scheduled task execution.
- `src/daemon/` — daemon process management.
- `src/sessions/` — session storage and lifecycle.
- `src/secrets/` — credential management.
- `src/security/` — security policies and validation.
- `src/terminal/` — ANSI output, tables, palette (`palette.ts`), progress bars.
- `src/logging/` — structured logging and filtering.
- `src/tts/` — text-to-speech providers.
- `src/image-generation/` — image generation providers.
- `src/media-understanding/` — vision/media analysis integrations.
- `src/web-search/` — search provider abstraction.
- `src/link-understanding/` — URL preview/extraction.
- `src/acp/` — Agent Control Protocol implementation.
- `src/interactive/` — interactive tool UI.
- `src/i18n/` — internationalization.
- `src/markdown/` — markdown utilities.
- `src/providers/` — LLM/model provider integrations.
- `src/wizard/` — interactive setup wizards.
- `src/compat/` — backward compatibility layers.
- `src/bindings/` — JavaScript/native bindings.
- `src/utils/`, `src/shared/`, `src/types/` — general utilities and type definitions.

### Extensions

**Channel extensions** (each implements `ChannelPlugin` from plugin-sdk):

- `extensions/discord/` — Discord Bot API (discord.js).
- `extensions/telegram/` — Telegram Bot API (grammy).
- `extensions/slack/` — Slack workspace messaging (@slack/bolt).
- `extensions/matrix/` — Matrix protocol (matrix-js-sdk).
- `extensions/whatsapp/` — WhatsApp via Baileys library.
- `extensions/line/` — LINE Bot API.
- `extensions/twitch/` — Twitch chat/API (twurple).
- `extensions/feishu/` — Feishu/Lark (ByteDance SDK).

**Feature extensions:**

- `extensions/memory-core/` — core memory search plugin.
- `extensions/acpx/` — ACP runtime backend.
- `extensions/diagnostics-otel/` — OpenTelemetry diagnostics exporters.

**Shared:** `extensions/shared/` — shared utilities for extensions.

**Extension rules:**

- Plugins/extensions: live under `extensions/*` (workspace packages). Keep plugin-only deps in the extension `package.json`; do not add them to the root `package.json` unless core uses them.
- Plugins: install runs `npm install --omit=dev` in plugin dir; runtime deps must live in `dependencies`. Avoid `workspace:*` in `dependencies` (npm install breaks); put `deneb` in `devDependencies` or `peerDependencies` instead (runtime resolves `deneb/plugin-sdk` via jiti alias).
- Import boundaries: extension production code should treat `deneb/plugin-sdk/*` plus local `api.ts` / `runtime-api.ts` barrels as the public surface. Do not import core `src/**`, `src/plugin-sdk-internal/**`, or another extension's `src/**` directly.

### Rust Core Library (`core-rs/`)

CPU-intensive core functions in Rust, exposed via C FFI and napi-rs.

- `src/lib.rs` — FFI entry: `deneb_validate_frame`, `deneb_constant_time_eq`, `deneb_detect_mime`, `deneb_validate_session_key`, `deneb_sanitize_html`, `deneb_is_safe_url`, `deneb_validate_error_code`.
- `src/protocol/mod.rs` — Gateway frame validation (replaces AJV). Types: `RequestFrame`, `ResponseFrame`, `EventFrame`, `ErrorShape`, `StateVersion`.
- `src/protocol/error_codes.rs` — `ErrorCode` enum (14 codes matching TypeScript `error-codes.ts`), wire-format roundtrip, retryability classification.
- `src/protocol/gen.rs` — prost-generated protobuf types (`gen::gateway`, `gen::channel`, `gen::session`).
- `src/security/mod.rs` — `constant_time_eq`, `is_safe_input`, `sanitize_control_chars`, `is_valid_session_key`, `sanitize_html`, `is_safe_url` (SSRF protection).
- `src/media/mod.rs` — Magic-byte MIME detection (21 formats, zero-allocation).
- `src/media/extensions.rs` — MIME-to-extension mapping (35+ types), `MediaCategory` classification, `detect_mime_with_info`, `is_image`/`is_audio`/`is_video` helpers.
- `build.rs` — prost-build code generation from `proto/*.proto`.
- Crate types: `cdylib` (Go/Node FFI), `staticlib` (C linking), `rlib` (Rust consumers).
- Build: `cd core-rs && cargo build --release` or `make rust`.
- Test: `cd core-rs && cargo test` or `make rust-test`.

### Go Gateway (`gateway-go/`)

HTTP/WS gateway server scaffolding (Phase 2 target: replace Node.js gateway).

- `cmd/gateway/main.go` — Entry point with `--port`/`--bind` flags, graceful shutdown.
- `internal/server/` — HTTP server: `/health`, `/api/v1/rpc`. Connection tracking.
- `internal/rpc/` — Registry-based RPC method dispatcher (thread-safe). 15+ built-in methods including FFI-backed security/media validation.
- `internal/session/` — Session management with lifecycle state machine (`IDLE → RUNNING → DONE/FAILED/KILLED/TIMEOUT`), state transition validation, event pub/sub bus.
- `internal/channel/` — Channel plugin registry with `Plugin` interface, `Meta`, `Capabilities`. Lifecycle manager for concurrent start/stop/health-check orchestration.
- `internal/bridge/` — Node.js plugin host bridge via Unix socket frame protocol.
- `pkg/protocol/` — Hand-written JSON wire types + generated protobuf types in `gen/`.
- `pkg/protocol/consistency_test.go` — Bidirectional reflection tests ensuring hand-written and generated types stay in sync.
- Build: `cd gateway-go && go build ./...` or `make go`.
- Test: `cd gateway-go && go test ./...` or `make go-test`.

### Protobuf Schemas (`proto/`)

Shared type definitions compiled to Go, Rust, and TypeScript.

- `gateway.proto` — `ErrorCode` enum, `RequestFrame`, `ResponseFrame`, `EventFrame`, `ErrorShape`, `StateVersion`, `GatewayFrame`, `PresenceEntry`, `HelloOk`.
- `channel.proto` — `ChannelCapabilities`, `ChannelMeta`, `ChannelAccountSnapshot`.
- `session.proto` — `SessionRunStatus`, `SessionKind`, `GatewaySessionRow`, `SessionPreviewItem`, `SessionTransition`, `SessionLifecyclePhase`, `SessionLifecycleEvent`.
- `buf.yaml` — buf lint/breaking config.
- `buf.gen.go.yaml` — Go codegen config (protoc-gen-go).
- `buf.gen.ts.yaml` — TypeScript codegen config (ts-proto).
- Generation: `./scripts/proto-gen.sh` (parallel Go+Rust+TS). See also `make proto`.
- Outputs: `gateway-go/pkg/protocol/gen/*.pb.go`, `src/protocol/generated/*.ts`, Rust via `OUT_DIR`.
- CI: `.github/workflows/proto-check.yml` validates generation + breaking changes on PR.

### Native Addon (`native/`)

napi-rs Node.js addon for performance-critical TypeScript callers.

- `src/lib.rs` — napi entry.
- `src/gitignore.rs` — globset-based gitignore matching.
- `src/exif.rs` — EXIF orientation extraction (kamadak-exif).
- `src/png.rs` — PNG encoding (crc32fast + flate2).
- Build: `pnpm native:build` (napi build --release).
- Test: `pnpm native:test` (cargo test).

### Multi-Language IPC Architecture

- **Go ↔ Rust:** CGo FFI (in-process, zero overhead). Go calls `deneb_*` C functions from `core-rs`.
- **Go ↔ Node.js:** Unix domain socket + gateway frame protocol. Node.js runs as plugin host subprocess.
- **Go ↔ Python:** Subprocess + JSONL/MCP (existing vega integration).
- **CLI ↔ Gateway:** WebSocket (existing).
- Proto schemas are the cross-language source of truth for frame types.

### Monorepo Topology

- pnpm workspaces: root + `ui/` (extensions are NOT workspace packages; they are installed separately).
- TypeScript path aliases: `deneb/plugin-sdk` → `src/plugin-sdk/index.ts`, `deneb/plugin-sdk/*` → `src/plugin-sdk/*.ts`.
- Package exports: 160+ plugin-sdk subpath exports defined in `package.json`.
- Entry point: `bin/deneb.mjs` → `src/entry.ts` → `src/cli/run-main.ts`.
- Build output: `dist/` (main), `dist-runtime/` (runtime).
- Multi-language build: `Makefile` orchestrates Rust (`cargo`), Go (`go`), TypeScript (`pnpm`), and protobuf (`buf`/`prost`).

### Key Architectural Flows

1. **CLI execution:** `entry.ts` → `cli/run-main.ts` → `cli/program/build-program.ts` (command tree) → `commands/*` handlers.
2. **Gateway message routing:** `gateway/server.ts` → session lifecycle state machine → channel registry → plugin hooks → RPC methods → channel plugin send/receive.
3. **Plugin loading:** `plugins/loader.ts` → `plugins/manifest-registry.ts` → plugin-sdk contracts → `extensions/*` implementations.
4. **Channel message flow:** `channels/registry.ts` → `channels/run-state-machine.ts` → transport layer → channel plugin (extension or built-in).
5. **Provider resolution:** `plugins/provider-catalog.ts` → `plugins/provider-runtime.ts` → provider auth → LLM/image/search/TTS provider.
6. **Protobuf type flow:** `proto/*.proto` → `scripts/proto-gen.sh` → Go (`gen/*.pb.go`), Rust (prost `OUT_DIR`), TypeScript (`src/protocol/generated/*.ts`).
7. **Go gateway (target):** `gateway-go/cmd/gateway/main.go` → `internal/server` (HTTP/WS) → `internal/rpc` (dispatch) → `internal/session` (state) → `internal/bridge` (Node.js plugin host via Unix socket).
8. **Rust FFI flow:** `core-rs/src/lib.rs` (C ABI) → Go CGo calls or napi-rs Node.js addon → protocol validation / security / media detection.

### Cross-Cutting Concerns

- Installers served from `https://deneb.ai/*`: live in the sibling repo `../deneb.ai` (`public/install.sh`, `public/install-cli.sh`, `public/install.ps1`).
- Messaging channels: always consider **all** built-in + extension channels when refactoring shared logic (routing, allowlists, pairing, command gating, onboarding, docs).
  - Core channel docs: `docs/channels/`
  - Core channel code: `src/telegram`, `src/discord`, `src/slack`, `src/signal`, `src/imessage`, `src/web` (WhatsApp web), `src/channels`, `src/routing`
  - Extensions (channel plugins): `extensions/*` (e.g. `extensions/msteams`, `extensions/matrix`, `extensions/zalo`, `extensions/zalouser`, `extensions/voice-call`)
- When adding channels/extensions/apps/docs, update `.github/labeler.yml` and create matching GitHub labels (use existing channel/extension label colors).

## Docs Linking (Mintlify)

- Docs are hosted on Mintlify (docs.deneb.ai).
- Internal doc links in `docs/**/*.md`: root-relative, no `.md`/`.mdx` (example: `[Config](/configuration)`).
- When working with documentation, read the mintlify skill.
- For docs, UI copy, and picker lists, order services/providers alphabetically unless the section is explicitly describing runtime behavior (for example auto-detection or execution order).
- Section cross-references: use anchors on root-relative paths (example: `[Hooks](/configuration#hooks)`).
- Doc headings and anchors: avoid em dashes and apostrophes in headings because they break Mintlify anchor links.
- When Peter asks for links, reply with full `https://docs.deneb.ai/...` URLs (not root-relative).
- When you touch docs, end the reply with the `https://docs.deneb.ai/...` URLs you referenced.
- README (GitHub): keep absolute docs URLs (`https://docs.deneb.ai/...`) so links work on GitHub.
- Docs content must be generic: no personal device names/hostnames/paths; use placeholders like `user@gateway-host` and “gateway host”.

## Docs Syntax Rules (Mintlify)

- Frontmatter (YAML) is required on every doc file with these fields:
  - `title` (required): matches the page H1 heading; 2-5 words.
  - `summary` (required): 1-2 sentences, max ~100 chars.
  - `read_when` (required): array of 2-3 user scenarios/intents describing when to read this page.
  - `sidebarTitle` (optional): shorter label for the sidebar.
- Heading structure: one H1 (`#`) per page matching frontmatter `title`; H2 (`##`) for major sections (3-5 per page typical); H3 (`###`) for subsections; H4 (`####`) rarely.
- Code blocks: always use language tags (`bash`, `json5`, `python`, `typescript`, `powershell`, `swift`, `mermaid`). Use `json5` (not `json`) for config examples (supports comments and trailing commas). Use inline code (single backticks) for file paths, commands, config keys, and JSON fields.
- Mintlify components are globally available (no imports needed):
  - `<Steps>` / `<Step title=”...”>`: numbered procedures, quick starts.
  - `<Tabs>` / `<Tab title=”...”>`: platform/OS variants, mutually exclusive content.
  - `<Info>`, `<Tip>`, `<Warning>`, `<Note>`, `<Check>`: callout boxes.
  - `<AccordionGroup>` / `<Accordion title=”...”>`: collapsible optional/advanced content.
  - `<CardGroup cols={N}>` / `<Card title=”...” icon=”...” href=”...”>`: feature grids, navigation.
  - `<Columns>` / `<Card>`: responsive card layouts (alternative to CardGroup).
  - `<Tooltip headline=”...” tip=”...”>`: hover definitions.
  - `<Frame caption=”...”>`: image wrapper with caption.
  - Icons use the Lucide library (e.g. `icon=”rocket”`, `icon=”settings”`, `icon=”message-square”`).
- Images: use root-relative paths (`/assets/...`). For light/dark mode, use paired `<img>` tags with `class=”dark:hidden”` and `class=”hidden dark:block”`.
- Tables: standard Markdown tables for feature matrices, mode mappings, option lists. Use `✅` / `❌` for yes/no cells.
- File conventions: all doc files are `.md` (Mintlify processes MDX syntax transparently). File naming: lowercase, hyphenated (`getting-started.md`, `voice-wake.md`).
- Validation scripts: `pnpm docs:dev` (local preview), `pnpm docs:check-links` (link audit), `pnpm docs:spellcheck` (spell check; `pnpm docs:spellcheck:fix` to auto-fix), `pnpm format:docs` (format check; `pnpm format:docs:fix` to auto-fix).

## Docs i18n (zh-CN)

- `docs/zh-CN/**` is generated; do not edit unless the user explicitly asks.
- Pipeline: update English docs → adjust glossary (`docs/.i18n/glossary.zh-CN.json`) → run `scripts/docs-i18n` → apply targeted fixes only if instructed.
- Before rerunning `scripts/docs-i18n`, add glossary entries for any new technical terms, page titles, or short nav labels that must stay in English or use a fixed translation (for example `Doctor` or `Polls`).
- `pnpm docs:check-i18n-glossary` enforces glossary coverage for changed English doc titles and short internal doc labels before translation reruns.
- Translation memory: `docs/.i18n/zh-CN.tm.jsonl` (generated).
- See `docs/.i18n/README.md`.
- The pipeline can be slow/inefficient; if it’s dragging, ping @jospalmbier on Discord instead of hacking around it.

## exe.dev VM ops (general)

- Access: stable path is `ssh exe.dev` then `ssh vm-name` (assume SSH key already set).
- SSH flaky: use exe.dev web terminal or Shelley (web agent); keep a tmux session for long ops.
- Update: `sudo npm i -g deneb@latest` (global install needs root on `/usr/lib/node_modules`).
- Config: use `deneb config set ...`; ensure `gateway.mode=local` is set.
- Discord: store raw token only (no `DISCORD_BOT_TOKEN=` prefix).
- Restart: stop old gateway and run:
  `pkill -9 -f deneb-gateway || true; nohup deneb gateway run --bind loopback --port 18789 --force > /tmp/deneb-gateway.log 2>&1 &`
- Verify: `deneb channels status --probe`, `ss -ltnp | rg 18789`, `tail -n 120 /tmp/deneb-gateway.log`.

## Build, Test, and Development Commands

- Runtime baseline: Node **22+** (keep Node + Bun paths working).
- Install deps: `pnpm install`
- If deps are missing (for example `node_modules` missing, `vitest not found`, or `command not found`), run the repo’s package-manager install command (prefer lockfile/README-defined PM), then rerun the exact requested command once. Apply this to test/build/lint/typecheck/dev commands; if retry still fails, report the command and first actionable error.
- Pre-commit hooks: `prek install` (runs same checks as CI)
- Also supported: `bun install` (keep `pnpm-lock.yaml` + Bun patching in sync when touching deps/patches).
- Prefer Bun for TypeScript execution (scripts, dev, tests): `bun <file.ts>` / `bunx <tool>`.
- Node remains supported for running built output (`dist/*`) and production installs.
- Hard gate: before any commit, `pnpm check` MUST be run and MUST pass for the change being committed.
- Hard gate: before any push to `main`, `pnpm check` MUST be run and MUST pass, and `pnpm test` MUST be run and MUST pass.
- Hard gate: if the change can affect build output, packaging, lazy-loading/module boundaries, or published surfaces, `pnpm build` MUST be run and MUST pass before pushing `main`.
- Hard gate: do not commit or push with failing format, lint, type, build, or required test checks.
- Hard gate: if the change touches `core-rs/`, `gateway-go/`, or `proto/`, run `make check` (includes `proto-check`, `rust-test`, `go-test`, `ts-check`) before pushing.
- Multi-language toolchain: Rust (stable via rustup), Go (1.24+), buf (latest), protoc, protoc-gen-go, ts-proto.

### Multi-Language Build (Makefile)

| Command            | Description                                        |
| ------------------ | -------------------------------------------------- |
| `make all`         | Build Rust + Go (release)                          |
| `make rust`        | Build Rust core library (release)                  |
| `make rust-test`   | Run Rust tests (`cargo test`)                      |
| `make go`          | Build Go gateway                                   |
| `make go-test`     | Run Go tests (`go test ./...`)                     |
| `make go-run`      | Run Go gateway locally                             |
| `make test`        | Run Rust + Go tests                                |
| `make check`       | Full check: proto-check + rust-test + go-test + ts |
| `make clean`       | Clean Rust + Go build artifacts                    |
| `make proto`       | Generate protobuf code (Go + Rust + TS, parallel)  |
| `make proto-go`    | Generate Go protobuf structs only                  |
| `make proto-rust`  | Generate Rust protobuf structs only                |
| `make proto-ts`    | Generate TypeScript protobuf types only            |
| `make proto-check` | Generate + verify no uncommitted diffs             |
| `make proto-lint`  | Lint proto files only (buf lint)                   |
| `make proto-watch` | Watch proto files and regenerate on change         |

### Development

| Command                  | Description                                |
| ------------------------ | ------------------------------------------ |
| `pnpm dev`               | Run CLI in dev mode                        |
| `pnpm deneb ...`         | Run CLI in dev mode (alias)                |
| `pnpm deneb:rpc`         | Run agent in RPC/JSON mode                 |
| `pnpm start`             | Run CLI (alias)                            |
| `pnpm gateway:dev`       | Run gateway in dev mode (channels skipped) |
| `pnpm gateway:dev:reset` | Run gateway in dev mode with reset         |
| `pnpm gateway:watch`     | Watch mode for gateway                     |
| `pnpm ui:dev`            | Run control UI dev server                  |
| `pnpm ui:install`        | Install control UI dependencies            |

### Build

| Command                     | Description                                                    |
| --------------------------- | -------------------------------------------------------------- |
| `pnpm build`                | Full production build (A2UI bundle + tsdown + DTS + postbuild) |
| `pnpm build:docker`         | Docker-optimized build (skips A2UI bundle and DTS)             |
| `pnpm build:plugin-sdk:dts` | Generate plugin-sdk type declarations only                     |
| `pnpm build:strict-smoke`   | Strict smoke build (A2UI + tsdown + DTS, no postbuild scripts) |
| `pnpm ui:build`             | Build control UI                                               |
| `pnpm canvas:a2ui:bundle`   | Bundle A2UI canvas assets                                      |
| `pnpm prepack`              | Pre-publish build (`pnpm build` + `pnpm ui:build`)             |

### TypeScript / Type Checking

| Command     | Description                  |
| ----------- | ---------------------------- |
| `pnpm tsgo` | Run TypeScript type checking |

### Formatting

| Command                  | Description                                      |
| ------------------------ | ------------------------------------------------ |
| `pnpm format`            | Format all files (oxfmt --write)                 |
| `pnpm format:check`      | Check formatting without writing (oxfmt --check) |
| `pnpm format:fix`        | Fix formatting (alias for `pnpm format`)         |
| `pnpm format:diff`       | Format and show git diff                         |
| `pnpm format:all`        | Format TypeScript + Swift                        |
| `pnpm format:swift`      | Lint Swift formatting (swiftformat)              |
| `pnpm format:docs`       | Format docs markdown files                       |
| `pnpm format:docs:check` | Check docs markdown formatting                   |

### Linting

| Command              | Description                                                        |
| -------------------- | ------------------------------------------------------------------ |
| `pnpm check`         | Full pre-commit check (format + tsgo + lint + all boundary checks) |
| `pnpm lint`          | Run oxlint with type-aware rules                                   |
| `pnpm lint:fix`      | Auto-fix lint issues + reformat                                    |
| `pnpm lint:all`      | Lint TypeScript + Swift                                            |
| `pnpm lint:swift`    | Lint Swift code (swiftlint)                                        |
| `pnpm lint:docs`     | Lint documentation markdown                                        |
| `pnpm lint:docs:fix` | Auto-fix documentation lint issues                                 |
| `pnpm check:loc`     | Check TypeScript files max LOC (default 500)                       |

### Lint: Boundary Checks (run as part of `pnpm check`)

| Command                                                    | Description                                              |
| ---------------------------------------------------------- | -------------------------------------------------------- |
| `pnpm lint:tmp:no-random-messaging`                        | No random messaging patterns                             |
| `pnpm lint:tmp:channel-agnostic-boundaries`                | Channel-agnostic boundary enforcement                    |
| `pnpm lint:tmp:no-raw-channel-fetch`                       | No raw channel fetch usage                               |
| `pnpm lint:agent:ingress-owner`                            | Agent ingress owner context check                        |
| `pnpm lint:plugins:no-register-http-handler`               | No direct HTTP handler registration in plugins           |
| `pnpm lint:plugins:no-monolithic-plugin-sdk-entry-imports` | No monolithic plugin-sdk entry imports                   |
| `pnpm lint:plugins:no-extension-src-imports`               | No extension src imports from core                       |
| `pnpm lint:plugins:no-extension-test-core-imports`         | No core imports from extension tests                     |
| `pnpm lint:plugins:no-extension-imports`                   | Plugin extension import boundary                         |
| `pnpm lint:plugins:plugin-sdk-subpaths-exported`           | All plugin-sdk subpaths are exported                     |
| `pnpm lint:extensions:no-src-outside-plugin-sdk`           | Extensions must not import src outside plugin-sdk        |
| `pnpm lint:extensions:no-plugin-sdk-internal`              | Extensions must not import plugin-sdk internals          |
| `pnpm lint:extensions:no-relative-outside-package`         | Extensions must not use relative imports outside package |
| `pnpm lint:web-search-provider-boundaries`                 | Web search provider boundary check                       |
| `pnpm lint:webhook:no-low-level-body-read`                 | Webhook auth body order check                            |
| `pnpm lint:auth:no-pairing-store-group`                    | No pairing store group in auth                           |
| `pnpm lint:auth:pairing-account-scope`                     | Pairing account scope check                              |
| `pnpm lint:ui:no-raw-window-open`                          | No raw window.open in UI                                 |
| `pnpm plugin-sdk:check-exports`                            | Verify plugin-sdk exports are in sync                    |
| `pnpm check:bundled-provider-auth-env-vars`                | Check bundled provider auth env vars                     |
| `pnpm check:host-env-policy:swift`                         | Check Swift host env security policy                     |

### Tests

| Command                             | Description                                                      |
| ----------------------------------- | ---------------------------------------------------------------- |
| `pnpm test`                         | Run all tests (vitest, parallel)                                 |
| `pnpm test:fast`                    | Run unit tests only (vitest, no coverage)                        |
| `pnpm test:coverage`                | Run unit tests with V8 coverage                                  |
| `pnpm test:watch`                   | Run tests in watch mode                                          |
| `pnpm test:gateway`                 | Run gateway tests (forked pool)                                  |
| `pnpm test:channels`                | Run channel tests                                                |
| `pnpm test:extensions`              | Run extension tests                                              |
| `pnpm test:extension`               | Run a single extension’s tests                                   |
| `pnpm test:e2e`                     | Run end-to-end tests                                             |
| `pnpm test:ui`                      | Lint UI + run UI tests                                           |
| `pnpm test:contracts`               | Run channel + plugin contract tests                              |
| `pnpm test:contracts:channels`      | Run channel contract tests                                       |
| `pnpm test:contracts:plugins`       | Run plugin contract tests                                        |
| `pnpm test:auth:compat`             | Run auth compatibility baseline tests                            |
| `pnpm test:all`                     | Full test suite (lint + build + test + e2e + live + Docker)      |
| `pnpm test:build:singleton`         | Verify built plugin singleton invariant                          |
| `pnpm test:resume`                  | Resume a previously interrupted test run                         |
| `pnpm test:force`                   | Force-run tests ignoring cache                                   |
| `pnpm test:macmini`                 | Serial test run optimized for Mac mini (low fork/serial profile) |
| `pnpm test -- <path> [vitest args]` | Run specific tests (prefer over raw `vitest`)                    |

### Tests: Live (require real API keys)

| Command                               | Description                               |
| ------------------------------------- | ----------------------------------------- |
| `CLAWDBOT_LIVE_TEST=1 pnpm test:live` | Live tests (Deneb-only)                   |
| `LIVE=1 pnpm test:live`               | Live tests (includes provider live tests) |

### Tests: Docker

| Command                            | Description                        |
| ---------------------------------- | ---------------------------------- |
| `pnpm test:docker:all`             | Run all Docker test suites         |
| `pnpm test:docker:live-models`     | Docker live model tests            |
| `pnpm test:docker:live-gateway`    | Docker live gateway tests          |
| `pnpm test:docker:onboard`         | Docker onboarding E2E              |
| `pnpm test:docker:gateway-network` | Docker gateway network tests       |
| `pnpm test:docker:qr`              | Docker QR import tests             |
| `pnpm test:docker:doctor-switch`   | Docker doctor/install switch tests |
| `pnpm test:docker:plugins`         | Docker plugin tests                |
| `pnpm test:docker:cleanup`         | Clean up Docker test containers    |

### Tests: Install

| Command                           | Description                         |
| --------------------------------- | ----------------------------------- |
| `pnpm test:install:smoke`         | Smoke test install script in Docker |
| `pnpm test:install:e2e`           | Full E2E install test in Docker     |
| `pnpm test:install:e2e:anthropic` | E2E install test (Anthropic models) |
| `pnpm test:install:e2e:openai`    | E2E install test (OpenAI models)    |

### Tests: Parallels VM

| Command                       | Description                  |
| ----------------------------- | ---------------------------- |
| `pnpm test:parallels:linux`   | Parallels Linux smoke test   |
| `pnpm test:parallels:macos`   | Parallels macOS smoke test   |
| `pnpm test:parallels:windows` | Parallels Windows smoke test |

### Tests: Performance

| Command                              | Description                         |
| ------------------------------------ | ----------------------------------- |
| `pnpm test:perf:budget`              | Run performance budget checks       |
| `pnpm test:perf:hotspots`            | Detect test hotspots                |
| `pnpm test:perf:update-timings`      | Update test timing baselines        |
| `pnpm test:startup:memory`           | Check CLI startup memory usage      |
| `pnpm test:extensions:memory`        | Profile extension memory usage      |
| `pnpm test:gateway:watch-regression` | Check gateway watch for regressions |

### Documentation

| Command                         | Description                                             |
| ------------------------------- | ------------------------------------------------------- |
| `pnpm docs:dev`                 | Run Mintlify local preview                              |
| `pnpm docs:check-links`         | Audit documentation links                               |
| `pnpm docs:spellcheck`          | Spell check docs                                        |
| `pnpm docs:spellcheck:fix`      | Auto-fix doc spelling                                   |
| `pnpm docs:check-i18n-glossary` | Check i18n glossary coverage                            |
| `pnpm check:docs`               | Full docs check (format + lint + i18n glossary + links) |
| `pnpm config:docs:gen`          | Generate config doc baseline                            |
| `pnpm config:docs:check`        | Check config doc baseline is up to date                 |
| `pnpm docs:bin`                 | Build docs list for CLI                                 |
| `pnpm docs:list`                | List all doc pages                                      |

### Code Generation / Sync

| Command                                  | Description                                     |
| ---------------------------------------- | ----------------------------------------------- |
| `pnpm plugin-sdk:sync-exports`           | Sync plugin-sdk subpath exports in package.json |
| `pnpm plugins:sync`                      | Sync plugin versions                            |
| `pnpm protocol:gen`                      | Generate protocol schema (JSON)                 |
| `pnpm protocol:gen:swift`                | Generate protocol Swift models                  |
| `pnpm protocol:check`                    | Generate protocol + check for uncommitted diffs |
| `pnpm gen:host-env-policy:swift`         | Generate Swift host env security policy         |
| `pnpm stage:bundled-plugin-runtime-deps` | Stage bundled plugin runtime deps               |

### Dead Code Analysis

| Command                | Description                                          |
| ---------------------- | ---------------------------------------------------- |
| `pnpm deadcode:knip`   | Run Knip dead code analysis                          |
| `pnpm deadcode:report` | Full dead code report (Knip + ts-prune + ts-unused)  |
| `pnpm deadcode:ci`     | CI dead code report (Knip, outputs to `.artifacts/`) |
| `pnpm dup:check`       | Check for code duplication (jscpd)                   |
| `pnpm dup:check:json`  | Code duplication report as JSON                      |

### Changelog / Version

| Command                   | Description                               |
| ------------------------- | ----------------------------------------- |
| `pnpm changelog:generate` | Generate changelog from commits           |
| `pnpm changelog:check`    | Check changelog is up to date             |
| `pnpm version:bump`       | Bump version across all version locations |
| `pnpm version:check`      | Check version consistency                 |

### Release

| Command                          | Description                        |
| -------------------------------- | ---------------------------------- |
| `pnpm release:check`             | Run release readiness checks       |
| `pnpm release:deneb:npm:check`   | Check Deneb npm release readiness  |
| `pnpm release:plugins:npm:check` | Check plugin npm release readiness |
| `pnpm release:plugins:npm:plan`  | Plan plugin npm releases           |

### iOS

| Command                 | Description                                               |
| ----------------------- | --------------------------------------------------------- |
| `pnpm ios:build`        | Build iOS app (configure signing + xcodegen + xcodebuild) |
| `pnpm ios:run`          | Build and run iOS app in simulator                        |
| `pnpm ios:open`         | Generate and open iOS Xcode project                       |
| `pnpm ios:gen`          | Generate iOS Xcode project only                           |
| `pnpm ios:beta`         | Full iOS beta release                                     |
| `pnpm ios:beta:archive` | Archive iOS beta build                                    |
| `pnpm ios:beta:prepare` | Prepare iOS beta release                                  |

### Android

| Command                         | Description                            |
| ------------------------------- | -------------------------------------- |
| `pnpm android:run`              | Build, install, and launch Android app |
| `pnpm android:install`          | Install Android debug APK              |
| `pnpm android:assemble`         | Assemble Android debug APK             |
| `pnpm android:bundle:release`   | Build Android release AAB              |
| `pnpm android:test`             | Run Android unit tests                 |
| `pnpm android:test:integration` | Run Android integration tests (live)   |
| `pnpm android:lint`             | Run Android ktlint check               |
| `pnpm android:lint:android`     | Run Android lint (Gradle)              |
| `pnpm android:format`           | Format Android Kotlin code             |

### macOS

| Command            | Description               |
| ------------------ | ------------------------- |
| `pnpm mac:package` | Package macOS app         |
| `pnpm mac:open`    | Open built macOS app      |
| `pnpm mac:restart` | Restart macOS gateway app |

## AI Developer Workflow (scripts for AI agents patching Deneb)

When an AI agent is modifying the Deneb codebase, use these scripts to understand impact, run the right checks, and avoid breaking things. All scripts output JSON for easy parsing.

### Quick path: single-command PR (recommended)

After committing your changes, create a PR in one step:

```bash
bun scripts/dev-create-pr.ts --title "fix: description"
```

This runs the smart gate, pushes, and creates the PR automatically. Options: `--skip-gate` (already ran), `--draft`, `--base <branch>`, `--issue <num>`, `--full-gate`.

### Individual scripts (for debugging or manual control)

- **Analyze impact:** `bun scripts/dev-patch-impact.ts` — categorizes changed files, suggests which gates to run. Use `--staged` for staged changes only.
- **Find affected tests:** `bun scripts/dev-affected.ts [file...]` — traces the import graph to find test files and downstream dependents. Uses batched grep (single pass) for speed.
- **Run commit gates:** `bun scripts/dev-commit-gate.ts` — smart gate that auto-detects scope:
  - **Docs/config-only changes**: skips all gates (instant).
  - **Source changes**: runs `pnpm check` + affected tests in parallel discovery mode.
  - **Plugin-sdk/build-config changes**: runs full gate (check + all tests + build).
  - Use `--full` to force all gates, `--no-test` to skip tests.

### Recommended workflow

1. Make changes, commit via `scripts/committer "msg" file1 file2`.
2. Run `bun scripts/dev-create-pr.ts --title "verb: description"` — handles everything.
3. If gate fails, fix and rerun. Use `bun scripts/dev-affected.ts src/path/to/file.ts` for targeted debugging.

## Coding Style & Naming Conventions

- Language: TypeScript (ESM). Prefer strict typing; avoid `any`.
- Formatting/linting via Oxlint and Oxfmt; run `pnpm check` before commits.
- Never add `@ts-nocheck` and do not disable `no-explicit-any`; fix root causes and update Oxlint/Oxfmt config only when required.
- Dynamic import guardrail: do not mix `await import("x")` and static `import ... from "x"` for the same module in production code paths. If you need lazy loading, create a dedicated `*.runtime.ts` boundary (that re-exports from `x`) and dynamically import that boundary from lazy callers only.
- Dynamic import verification: after refactors that touch lazy-loading/module boundaries, run `pnpm build` and check for `[INEFFECTIVE_DYNAMIC_IMPORT]` warnings before submitting.
- Extension SDK self-import guardrail: inside an extension package, do not import that same extension via `deneb/plugin-sdk/<extension>` from production files. Route internal imports through a local barrel such as `./api.ts` or `./runtime-api.ts`, and keep the `plugin-sdk/<extension>` path as the external contract only.
- Extension package boundary guardrail: inside `extensions/<id>/**`, do not use relative imports/exports that resolve outside that same `extensions/<id>` package root. If shared code belongs in the plugin SDK, import `deneb/plugin-sdk/<subpath>` instead of reaching into `src/plugin-sdk/**` or other repo paths via `../`.
- Extension API surface rule: `deneb/plugin-sdk/<subpath>` is the only public cross-package contract for extension-facing SDK code. If an extension needs a new seam, add a public subpath first; do not reach into `src/plugin-sdk/**` by relative path.
- Never share class behavior via prototype mutation (`applyPrototypeMixins`, `Object.defineProperty` on `.prototype`, or exporting `Class.prototype` for merges). Use explicit inheritance/composition (`A extends B extends C`) or helper composition so TypeScript can typecheck.
- If this pattern is needed, stop and get explicit approval before shipping; default behavior is to split/refactor into an explicit class hierarchy and keep members strongly typed.
- In tests, prefer per-instance stubs over prototype mutation (`SomeClass.prototype.method = ...`) unless a test explicitly documents why prototype-level patching is required.
- Add brief code comments for tricky or non-obvious logic.
- Keep files concise; extract helpers instead of “V2” copies. Use existing patterns for CLI options and dependency injection via `createDefaultDeps`.
- Aim to keep files under ~700 LOC; guideline only (not a hard guardrail). Split/refactor when it improves clarity or testability.
- Naming: use **Deneb** for product/app/docs headings; use `deneb` for CLI command, package/binary, paths, and config keys.
- Written English: use American spelling and grammar in code, comments, docs, and UI strings (e.g. "color" not "colour", "behavior" not "behaviour", "analyze" not "analyse").

## Release / Advisory Workflows

- Use `$deneb-release-maintainer` at `.agents/skills/deneb-release-maintainer/SKILL.md` for release naming, version coordination, release auth, and changelog-backed release-note workflows.
- Use `$deneb-ghsa-maintainer` at `.agents/skills/deneb-ghsa-maintainer/SKILL.md` for GHSA advisory inspection, patch/publish flow, private-fork checks, and GHSA API validation.
- Release and publish remain explicit-approval actions even when using the skill.

## Testing Guidelines

- Framework: Vitest with V8 coverage thresholds (70% lines/branches/functions/statements).
- Naming: match source names with `*.test.ts`; e2e in `*.e2e.test.ts`.
- Run `pnpm test` (or `pnpm test:coverage`) before pushing when you touch logic.
- Agents MUST NOT modify baseline, inventory, ignore, snapshot, or expected-failure files to silence failing checks without explicit approval in this chat.
- For targeted/local debugging, keep using the wrapper: `pnpm test -- <path-or-filter> [vitest args...]` (for example `pnpm test -- src/commands/onboard-search.test.ts -t "shows registered plugin providers"`); do not default to raw `pnpm vitest run ...` because it bypasses wrapper config/profile/pool routing.
- Do not set test workers above 16; tried already.
- If local Vitest runs cause memory pressure (common on non-Mac-Studio hosts), use `DENEB_TEST_PROFILE=low DENEB_TEST_SERIAL_GATEWAY=1 pnpm test` for land/gate runs.
- Live tests (real keys): `CLAWDBOT_LIVE_TEST=1 pnpm test:live` (Deneb-only) or `LIVE=1 pnpm test:live` (includes provider live tests). Docker: `pnpm test:docker:live-models`, `pnpm test:docker:live-gateway`. Onboarding Docker E2E: `pnpm test:docker:onboard`.
- Full kit + what’s covered: `docs/help/testing.md`.
- Changelog: user-facing changes only; no internal/meta notes (version alignment, appcast reminders, release process).
- Changelog placement: in the active version block, append new entries to the end of the target section (`### Changes` or `### Fixes`); do not insert new entries at the top of a section.
- Changelog attribution: use at most one contributor mention per line; prefer `Thanks @author` and do not also add `by @author` on the same entry.
- Pure test additions/fixes generally do **not** need a changelog entry unless they alter user-facing behavior or the user asks for one.
- Mobile: before using a simulator, check for connected real devices (iOS + Android) and prefer them when available.

## Commit & Pull Request Guidelines

- Use `$deneb-pr-maintainer` at `.agents/skills/deneb-pr-maintainer/SKILL.md` for maintainer PR triage, review, close, search, and landing workflows.
- This includes auto-close labels, bug-fix evidence gates, GitHub comment/search footguns, and maintainer PR decision flow.
- For the repo's end-to-end maintainer PR workflow, use `$deneb-pr-maintainer` at `.agents/skills/deneb-pr-maintainer/SKILL.md`.

- `/landpr` lives in the global Codex prompts (`~/.codex/prompts/landpr.md`); when landing or merging any PR, always follow that `/landpr` process.
- Create commits with `scripts/committer "<msg>" <file...>`; avoid manual `git add`/`git commit` so staging stays scoped.
- Follow concise, action-oriented commit messages (e.g., `CLI: add verbose flag to send`).
- Group related changes; avoid bundling unrelated refactors.
- PR submission template (canonical): `.github/pull_request_template.md`
- Issue submission templates (canonical): `.github/ISSUE_TEMPLATE/`

## Git Notes

- If `git branch -d/-D <branch>` is policy-blocked, delete the local ref directly: `git update-ref -d refs/heads/<branch>`.
- Agents MUST NOT create or push merge commits on `main`. If `main` has advanced, rebase local commits onto the latest `origin/main` before pushing.
- Bulk PR close/reopen safety: if a close action would affect more than 5 PRs, first ask for explicit user confirmation with the exact PR count and target scope/query.

## Security & Configuration Tips

- Web provider stores creds at `~/.deneb/credentials/`; rerun `deneb login` if logged out.
- Pi sessions live under `~/.deneb/sessions/` by default; the base directory is not configurable.
- Environment variables: see `~/.profile`.
- Never commit or publish real phone numbers, videos, or live configuration values. Use obviously fake placeholders in docs, tests, and examples.
- Release flow: use the private [maintainer release docs](https://github.com/deneb/maintainers/blob/main/release/README.md) for the actual runbook, `docs/reference/RELEASING.md` for the public release policy, and `$deneb-release-maintainer` for the maintainership workflow.

## Local Runtime / Platform Notes

- Vocabulary: "makeup" = "mac app".
- Rebrand/migration issues or legacy config/service warnings: run `deneb doctor` (see `docs/gateway/doctor.md`).
- Use `$deneb-parallels-smoke` at `.agents/skills/deneb-parallels-smoke/SKILL.md` for Parallels smoke, rerun, upgrade, debug, and result-interpretation workflows across macOS, Windows, and Linux guests.
- For the macOS Discord roundtrip deep dive, use the narrower `.agents/skills/parallels-discord-roundtrip/SKILL.md` companion skill.
- Never edit `node_modules` (global/Homebrew/npm/git installs too). Updates overwrite. Skill notes go in `tools.md` or `AGENTS.md`.
- If you need local-only `.agents` ignores, use `.git/info/exclude` instead of repo `.gitignore`.
- When adding a new `AGENTS.md` anywhere in the repo, also add a `CLAUDE.md` symlink pointing to it (example: `ln -s AGENTS.md CLAUDE.md`).
- Signal: "update fly" => `fly ssh console -a flawd-bot -C "bash -lc 'cd /data/clawd/deneb && git pull --rebase origin main'"` then `fly machines restart e825232f34d058 -a flawd-bot`.
- CLI progress: use `src/cli/progress.ts` (`osc-progress` + `@clack/prompts` spinner); don’t hand-roll spinners/bars.
- Status output: keep tables + ANSI-safe wrapping (`src/terminal/table.ts`); `status --all` = read-only/pasteable, `status --deep` = probes.
- Gateway currently runs only as the menubar app; there is no separate LaunchAgent/helper label installed. Restart via the Deneb Mac app or `scripts/restart-mac.sh`; to verify/kill use `launchctl print gui/$UID | grep deneb` rather than assuming a fixed label. **When debugging on macOS, start/stop the gateway via the app, not ad-hoc tmux sessions; kill any temporary tunnels before handoff.**
- macOS logs: use `./scripts/clawlog.sh` to query unified logs for the Deneb subsystem; it supports follow/tail/category filters and expects passwordless sudo for `/usr/bin/log`.
- If shared guardrails are available locally, review them; otherwise follow this repo's guidance.
- SwiftUI state management (iOS/macOS): prefer the `Observation` framework (`@Observable`, `@Bindable`) over `ObservableObject`/`@StateObject`; don’t introduce new `ObservableObject` unless required for compatibility, and migrate existing usages when touching related code.
- Connection providers: when adding a new connection, update every UI surface and docs (macOS app, web UI, mobile if applicable, onboarding/overview docs) and add matching status + configuration forms so provider lists and settings stay in sync.
- Version locations: `package.json` (CLI), `apps/android/app/build.gradle.kts` (versionName/versionCode), `apps/ios/Sources/Info.plist` + `apps/ios/Tests/Info.plist` (CFBundleShortVersionString/CFBundleVersion), `apps/macos/Sources/Deneb/Resources/Info.plist` (CFBundleShortVersionString/CFBundleVersion), `docs/install/updating.md` (pinned npm version), and Peekaboo Xcode projects/Info.plists (MARKETING_VERSION/CURRENT_PROJECT_VERSION).
- "Bump version everywhere" means all version locations above **except** `appcast.xml` (only touch appcast when cutting a new macOS Sparkle release).
- **Restart apps:** “restart iOS/Android apps” means rebuild (recompile/install) and relaunch, not just kill/launch.
- **Device checks:** before testing, verify connected real devices (iOS/Android) before reaching for simulators/emulators.
- iOS Team ID lookup: `security find-identity -p codesigning -v` → use Apple Development (…) TEAMID. Fallback: `defaults read com.apple.dt.Xcode IDEProvisioningTeamIdentifiers`.
- A2UI bundle hash: `src/canvas-host/a2ui/.bundle.hash` is auto-generated; ignore unexpected changes, and only regenerate via `pnpm canvas:a2ui:bundle` (or `scripts/bundle-a2ui.sh`) when needed. Commit the hash as a separate commit.
- Release signing/notary credentials are managed outside the repo; maintainers keep that setup in the private [maintainer release docs](https://github.com/deneb/maintainers/tree/main/release).
- Lobster palette: use the shared CLI palette in `src/terminal/palette.ts` (no hardcoded colors); apply palette to onboarding/config prompts and other TTY UI output as needed.
- When asked to open a “session” file, open the Pi session logs under `~/.deneb/agents/<agentId>/sessions/*.jsonl` (use the `agent=<id>` value in the Runtime line of the system prompt; newest unless a specific ID is given), not the default `sessions.json`. If logs are needed from another machine, SSH via Tailscale and read the same path there.
- Do not rebuild the macOS app over SSH; rebuilds must be run directly on the Mac.
- Voice wake forwarding tips:
  - Command template should stay `deneb-mac agent --message "${text}" --thinking low`; `VoiceWakeForwarder` already shell-escapes `${text}`. Don’t add extra quotes.
  - launchd PATH is minimal; ensure the app’s launch agent PATH includes standard system paths plus your pnpm bin (typically `$HOME/Library/pnpm`) so `pnpm`/`deneb` binaries resolve when invoked via `deneb-mac`.

## Collaboration / Safety Notes

- When working on a GitHub Issue or PR, print the full URL at the end of the task.
- When answering questions, respond with high-confidence answers only: verify in code; do not guess.
- Never update the Carbon dependency.
- Any dependency with `pnpm.patchedDependencies` must use an exact version (no `^`/`~`).
- Patching dependencies (pnpm patches, overrides, or vendored changes) requires explicit approval; do not do this by default.
- **Multi-agent safety:** do **not** create/apply/drop `git stash` entries unless explicitly requested (this includes `git pull --rebase --autostash`). Assume other agents may be working; keep unrelated WIP untouched and avoid cross-cutting state changes.
- **Multi-agent safety:** when the user says "push", you may `git pull --rebase` to integrate latest changes (never discard other agents' work). When the user says "commit", scope to your changes only. When the user says "commit all", commit everything in grouped chunks.
- **Multi-agent safety:** do **not** create/remove/modify `git worktree` checkouts (or edit `.worktrees/*`) unless explicitly requested.
- **Multi-agent safety:** do **not** switch branches / check out a different branch unless explicitly requested.
- **Multi-agent safety:** running multiple agents is OK as long as each agent has its own session.
- **Multi-agent safety:** when you see unrecognized files, keep going; focus on your changes and commit only those.
- Lint/format churn:
  - If staged+unstaged diffs are formatting-only, auto-resolve without asking.
  - If commit/push already requested, auto-stage and include formatting-only follow-ups in the same commit (or a tiny follow-up commit if needed), no extra confirmation.
  - Only ask when changes are semantic (logic/data/behavior).
- **Multi-agent safety:** focus reports on your edits; avoid guard-rail disclaimers unless truly blocked; when multiple agents touch the same file, continue if safe; end with a brief “other files present” note only if relevant.
- Bug investigations: read source code of relevant npm dependencies and all related local code before concluding; aim for high-confidence root cause.
- Code style: add brief comments for tricky logic; keep files under ~500 LOC when feasible (split/refactor as needed).
- Tool schema guardrails (google-antigravity): avoid `Type.Union` in tool input schemas; no `anyOf`/`oneOf`/`allOf`. Use `stringEnum`/`optionalStringEnum` (Type.Unsafe enum) for string lists, and `Type.Optional(...)` instead of `... | null`. Keep top-level tool schema as `type: "object"` with `properties`.
- Tool schema guardrails: avoid raw `format` property names in tool schemas; some validators treat `format` as a reserved keyword and reject the schema.
- Never send streaming/partial replies to external messaging surfaces (WhatsApp, Telegram); only final replies should be delivered there. Streaming/tool events may still go to internal UIs/control channel.
- For manual `deneb message send` messages that include `!`, use the heredoc pattern noted below to avoid the Bash tool’s escaping.
- Release guardrails: do not change version numbers without operator’s explicit consent; always ask permission before running any npm publish/release step.
- Beta release guardrail: when using a beta Git tag (for example `vYYYY.M.D-beta.N`), publish npm with a matching beta version suffix (for example `YYYY.M.D-beta.N`) rather than a plain version on `--tag beta`; otherwise the plain version name gets consumed/blocked.
