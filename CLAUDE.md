# Repository Guidelines

- Repo: https://github.com/deneb/deneb
- In chat replies, file references must be repo-root relative only (example: `gateway-go/internal/server/server.go:80`); never absolute paths or `~/...`.
- Do not edit files covered by security-focused `CODEOWNERS` rules unless a listed owner explicitly asked for the change or is already reviewing it with you. Treat those paths as restricted surfaces, not drive-by cleanup.

## Project Philosophy & Deployment Context (MUST READ)

> **All AI agents MUST read and internalize this section before making any changes.** This defines the fundamental constraints and design principles for the entire project.

### Deployment Environment

- **Single operator, single user.** This instance serves exactly one person. There is no multi-tenant, multi-user, or team deployment. Ignore code paths related to user isolation, permission separation, or multi-user auth.
- **Hardware:** NVIDIA DGX Spark (local server). All services run on this single machine.
- **Sole I/O surface:** Telegram on Android (Samsung Galaxy S25). This is the only channel in active use. Optimize exclusively for this path. Other channels exist in the codebase but are not deployment targets.

### Development Model

- All development is done via **vibe coding** — the sole developer works entirely through Claude Code and AI agents. There is no separate human-written code workflow.
- Prioritize **depth over breadth**: optimize the narrow supported surface (Telegram + DGX Spark + single user) rather than expanding to new platforms or channels.

### Design Principles

- **High completeness and cohesion.** Every feature that ships must be fully finished and tightly integrated, not partially implemented.
- **Opinionated defaults over user configuration.** Follow an Apple-like philosophy: lock down settings at the program level to deliver a stable, predictable UX. Avoid exposing configuration knobs that let the user degrade their own experience. Robustness comes from fewer moving parts, not more options.
- **Narrow scope, deep quality.** When choosing between supporting more things shallowly or fewer things well, always choose the latter.

### AI Agent Development Guidelines

- Since all development is vibe-coded, always leave sufficient context and comments so the next AI session can seamlessly pick up the work.
- Break complex logic into small, well-named functions so AI agents can easily understand and modify them.

### Single-User Optimization

- Multi-user/multi-tenant code paths can be ignored (auth separation, user isolation, concurrent access, etc.).
- Prefer simple sequential processing over concurrency/race-condition handling.
- Minimize settings migration and onboarding flows — the operator configures directly.

### Telegram-Only Optimization

- Optimize for Telegram Bot API constraints: 4096-char message limit, MarkdownV2 parse mode, inline keyboards.
- Prioritize perfect Telegram behavior over cross-channel compatibility.
- Respect Telegram file size limits (50 MB for media uploads).

### DGX Spark Hardware Utilization

- Local GPU inference is available — minimize external API calls and consider local model utilization.
- Memory and GPU resources are abundant — leverage aggressive caching and preloading.

### Korean Language First

- The primary user is Korean-speaking. Default to Korean for UI text, responses, and user-facing messages.
- No i18n framework needed — keep it simple with a single language.

### Deployment Simplification

- Single server (DGX Spark) direct deployment — no CI/CD pipeline or container orchestration needed.
- Deployment is simply `git pull` + restart.

## Project Structure & Module Organization

### Top-Level Directory Map

- `core-rs/` — Rust core library (protocol validation, security, media, memory search, markdown, context engine, compaction, Vega search, ML inference). Workspace with 4 crates. Builds as staticlib (Go CGo) + rlib.
- `gateway-go/` — Go gateway server (HTTP/WS server, RPC dispatch, session management, channel registry, chat/LLM, tools, auth). The primary runtime.
- `cli-rs/` — Rust CLI entry point.
- `proto/` — shared Protobuf schemas (gateway frames, channel types, session models). Source of truth for cross-language types.
- `skills/` — user-facing skill plugins (github, weather, summarize, coding-agent, etc.).
- `docs/` — Mintlify documentation site.
- `scripts/` — build, dev, CI, audit, and release scripts.
- `.agents/skills/` — maintainer agent skills (release, GHSA, PR, Parallels smoke).
- `.github/` — CI workflows, custom actions, issue/PR templates, labeler, CODEOWNERS.
- `Makefile` — multi-language build orchestration (Rust + Go + protobuf).
- Tests: Rust tests inline `#[cfg(test)]`; Go tests `*_test.go`.

### Rust Core Library (`core-rs/`)

Rust workspace with 4 crates, exposed to Go via C FFI (CGo static linking).

**deneb-core** (main crate, `core/`):
- `src/lib.rs` — 30+ C FFI exports (`deneb_*` functions). Error codes synced with `gateway-go/internal/ffi/errors.go`.
- `src/protocol/` — Gateway frame validation. Types: `RequestFrame`, `ResponseFrame`, `EventFrame`, `ErrorShape`, `StateVersion`.
- `src/security/` — `constant_time_eq`, `sanitize_html`, `is_safe_url` (SSRF), `is_valid_session_key`.
- `src/media/` — Magic-byte MIME detection (21 formats), MIME-to-extension mapping (35+ types), `MediaCategory`.
- `src/memory_search/` — SIMD-accelerated cosine similarity, BM25, FTS query builder, hybrid search merge, keyword extraction.
- `src/markdown/` — Markdown-to-IR parser (pulldown-cmark), fenced code block detection.
- `src/context_engine/` — Aurora context assembly/expansion state machines (handle-based FFI).
- `src/compaction/` — Compaction evaluation and sweep state machines.
- `src/parsing/` — Link extraction, HTML-to-Markdown, base64 utilities, media token parsing.
- `build.rs` — prost-build code generation from `proto/*.proto`.
- Crate types: `staticlib` (Go CGo linking), `rlib` (workspace consumers).

**deneb-vega** (`vega/`): SQLite FTS5 search engine. Optional `ml` feature for semantic search.

**deneb-ml** (`ml/`): GGUF inference via llama-cpp-2. Optional `cuda` feature for GPU acceleration.

**deneb-agent-runtime** (`agent-runtime/`): Agent lifecycle, model selection.

**Feature flags** (deneb-core): `vega` → `ml` → `cuda` → `vega-ml` → `dgx` (full DGX Spark).

- Build: `make rust` (minimal), `make rust-vega` (FTS), `make rust-dgx` (full).
- Test: `cd core-rs && cargo test` or `make rust-test`.

### Go Gateway (`gateway-go/`)

Primary runtime — HTTP/WS gateway server.

- `cmd/gateway/main.go` — Entry point with `--port`/`--bind` flags, graceful shutdown.
- `internal/server/` — HTTP server: `/health`, `/api/v1/rpc`, OpenAI/Responses APIs, hooks, session endpoints. Connection tracking.
- `internal/rpc/` — Registry-based RPC method dispatcher (thread-safe). 130+ methods including FFI-backed security/media/memory/context/compaction.
- `internal/session/` — Session management with lifecycle state machine (`IDLE → RUNNING → DONE/FAILED/KILLED/TIMEOUT`), state transition validation, event pub/sub bus.
- `internal/channel/` — Channel plugin registry with `Plugin` interface, `Meta`, `Capabilities`. Lifecycle manager for concurrent start/stop/health-check orchestration.
- `internal/ffi/` — CGo bindings to Rust core (8 `*_cgo.go` files + `*_noffi.go` fallbacks). Build tag: `!no_ffi && cgo`.
- `internal/auth/` — Token auth, allowlists, security paths, credentials, probe auth.
- `pkg/protocol/` — Hand-written JSON wire types + generated protobuf types in `gen/`.
- `pkg/protocol/consistency_test.go` — Bidirectional reflection tests ensuring hand-written and generated types stay in sync.
- `internal/chat/tools_core.go` — Core tool registration (exec, process, read, write, edit, grep, find, ls, web) with full JSON schemas.
- `internal/chat/tools_fs.go` — File system tool implementations (read with line numbers, write with dir creation, edit with uniqueness check, grep via rg, find via WalkDir, ls).
- `internal/chat/system_prompt.go` — System prompt assembly (identity, tooling, tool call style, safety, skills, memory recall, workspace, reply tags, messaging, timestamp, context files, silent replies, runtime).
- `internal/chat/context_files.go` — Workspace context file loader (AGENTS.md, CLAUDE.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, MEMORY.md). Budget: 20K chars/file, 150K total.
- `internal/chat/silent_reply.go` — SILENT_REPLY_TOKEN (NO_REPLY) detection and stripping for delivery suppression.
- `internal/chat/slash_commands.go` — Slash command pre-processing (/reset, /status, /kill, /model, /think).
- `internal/llm/types.go` — Sampling parameters: top_p, top_k, stop_sequences, frequency_penalty, presence_penalty. ImageSource for multimodal content.
- Build: `cd gateway-go && go build ./...` or `make go`.
- Test: `cd gateway-go && go test ./...` or `make go-test`.

### Protobuf Schemas (`proto/`)

Shared type definitions compiled to Go and Rust.

- `gateway.proto` — `ErrorCode` enum, `RequestFrame`, `ResponseFrame`, `EventFrame`, `ErrorShape`, `StateVersion`, `GatewayFrame`, `PresenceEntry`, `HelloOk`.
- `channel.proto` — `ChannelCapabilities`, `ChannelMeta`, `ChannelAccountSnapshot`.
- `session.proto` — `SessionRunStatus`, `SessionKind`, `GatewaySessionRow`, `SessionPreviewItem`, `SessionTransition`, `SessionLifecyclePhase`, `SessionLifecycleEvent`.
- `buf.yaml` — buf lint/breaking config.
- `buf.gen.go.yaml` — Go codegen config (protoc-gen-go).
- Generation: `./scripts/proto-gen.sh` (parallel Go+Rust). See also `make proto`.
- Outputs: `gateway-go/pkg/protocol/gen/*.pb.go`, Rust via `OUT_DIR`.
- CI: `.github/workflows/proto-check.yml` validates generation + breaking changes on PR.

### IPC Architecture

- **Go ↔ Rust:** CGo FFI (in-process, zero overhead). Go calls `deneb_*` C functions from `core-rs/target/release/libdeneb_core.a`.
- **CLI ↔ Gateway:** WebSocket.
- Proto schemas are the cross-language source of truth for frame types.

### Key Architectural Flows

1. **Gateway startup:** `gateway-go/cmd/gateway/main.go` → `internal/server` (HTTP/WS) → `internal/rpc` (dispatch) → `internal/session` (state) → `internal/channel` (plugins).
2. **Rust FFI flow:** `core-rs/core/src/lib.rs` (C ABI) → `gateway-go/internal/ffi/*_cgo.go` (Go wrappers) → RPC methods / chat pipeline.
3. **Protobuf type flow:** `proto/*.proto` → `scripts/proto-gen.sh` → Go (`gen/*.pb.go`), Rust (prost `OUT_DIR`).
4. **Stateful FFI pattern:** `*_new()` → handle → `*_start(handle)` → `*_step(handle, response)` → `*_drop(handle)` (context engine, compaction).

### Cross-Cutting Concerns

- When adding channels/docs, update `.github/labeler.yml` and create matching GitHub labels.

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
- Validation scripts: `pnpm docs:dev` (local preview), `pnpm docs:spellcheck` (spell check; `pnpm docs:spellcheck:fix` to auto-fix).

## DGX Spark ops

- Restart gateway: `pkill -9 -f deneb-gateway || true; nohup ./gateway-go/deneb-gateway --bind loopback --port 18789 > /tmp/deneb-gateway.log 2>&1 &`
- Verify: `ss -ltnp | rg 18789`, `tail -n 120 /tmp/deneb-gateway.log`.

## Build, Test, and Development Commands

- Toolchain: Rust (stable via rustup), Go (1.24+), buf (latest), protoc, protoc-gen-go.
- Hard gate: before any commit touching `core-rs/`, `gateway-go/`, or `proto/`, run `make check` (includes `proto-check`, `rust-test`, `go-test`) and it MUST pass.
- Hard gate: do not commit or push with failing build or test checks.

### Multi-Language Build (Makefile)

| Command             | Description                                                  |
| ------------------- | ------------------------------------------------------------ |
| `make all`          | Build Rust + Go (release)                                    |
| `make rust`         | Build Rust core library (release, minimal — no vega/ml)      |
| `make rust-vega`    | Build Rust core + Vega search (FTS-only, no ML)              |
| `make rust-dgx`     | Build Rust core + Vega + ML + CUDA (DGX Spark production)    |
| `make rust-test`    | Run Rust tests (`cargo test`)                                |
| `make go`           | Build Go gateway                                             |
| `make go-test`      | Run Go tests (`go test ./...`)                               |
| `make go-run`       | Run Go gateway locally                                       |
| `make go-dev`       | Run Go gateway in dev mode (auto-restart on SIGUSR1)         |
| `make gateway-dgx`  | Build DGX Spark production gateway (vega + ml + cuda)        |
| `make test`         | Run Rust + Go tests                                          |
| `make check`        | Full check: proto-check + rust-test + go-test + ts           |
| `make clean`        | Clean Rust + Go build artifacts                              |
| `make proto`        | Generate protobuf code (Go + Rust, parallel)                 |
| `make proto-go`     | Generate Go protobuf structs only                            |
| `make proto-rust`   | Generate Rust protobuf structs only                          |
| `make proto-check`  | Generate + verify no uncommitted diffs                       |
| `make proto-lint`   | Lint proto files only (buf lint)                             |
| `make proto-watch`  | Watch proto files and regenerate on change                   |

### Documentation

| Command                    | Description                |
| -------------------------- | -------------------------- |
| `pnpm docs:dev`            | Run Mintlify local preview |
| `pnpm docs:spellcheck`     | Spell check docs           |
| `pnpm docs:spellcheck:fix` | Auto-fix doc spelling      |

## Coding Style & Naming Conventions

- Languages: Go (gateway-go), Rust (core-rs, cli-rs).
- Go: follow standard `gofmt`/`go vet` conventions. Run `go vet ./...` before commits.
- Rust: follow `cargo fmt`/`cargo clippy` conventions. Run `cargo clippy --workspace` before commits.
- Add brief code comments for tricky or non-obvious logic.
- Keep files concise; extract helpers instead of “V2” copies.
- Aim to keep files under ~700 LOC; guideline only (not a hard guardrail). Split/refactor when it improves clarity or testability.
- Naming: use **Deneb** for product/app/docs headings; use `deneb` for CLI command, package/binary, paths, and config keys.
- Written English: use American spelling and grammar in code, comments, docs, and UI strings (e.g. “color” not “colour”, “behavior” not “behaviour”, “analyze” not “analyse”).

## Release / Advisory Workflows

- Use `$deneb-release-maintainer` at `.agents/skills/deneb-release-maintainer/SKILL.md` for release naming, version coordination, release auth, and changelog-backed release-note workflows.
- Use `$deneb-ghsa-maintainer` at `.agents/skills/deneb-ghsa-maintainer/SKILL.md` for GHSA advisory inspection, patch/publish flow, private-fork checks, and GHSA API validation.
- Release and publish remain explicit-approval actions even when using the skill.

## Testing Guidelines

- Rust tests: `cargo test --workspace` (or `make rust-test`). Tests are inline `#[cfg(test)]`.
- Go tests: `go test ./...` (or `make go-test`). Tests are `*_test.go` colocated with source.
- Run `make test` before pushing when you touch logic.
- Agents MUST NOT modify baseline, inventory, ignore, snapshot, or expected-failure files to silence failing checks without explicit approval in this chat.
- Changelog: user-facing changes only; no internal/meta notes (version alignment, appcast reminders, release process).
- Changelog placement: in the active version block, append new entries to the end of the target section (`### Changes` or `### Fixes`); do not insert new entries at the top of a section.
- Pure test additions/fixes generally do **not** need a changelog entry unless they alter user-facing behavior or the user asks for one.

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
- Skill notes go in `tools.md` or `CLAUDE.md`.
- If you need local-only `.agents` ignores, use `.git/info/exclude` instead of repo `.gitignore`.
- Signal: "update fly" => `fly ssh console -a flawd-bot -C "bash -lc 'cd /data/clawd/deneb && git pull --rebase origin main'"` then `fly machines restart e825232f34d058 -a flawd-bot`.
- Status output: `status --all` = read-only/pasteable, `status --deep` = probes.
- Gateway currently runs only as the menubar app; there is no separate LaunchAgent/helper label installed. Restart via the Deneb Mac app or `scripts/restart-mac.sh`; to verify/kill use `launchctl print gui/$UID | grep deneb` rather than assuming a fixed label. **When debugging on macOS, start/stop the gateway via the app, not ad-hoc tmux sessions; kill any temporary tunnels before handoff.**
- macOS logs: use `./scripts/clawlog.sh` to query unified logs for the Deneb subsystem; it supports follow/tail/category filters and expects passwordless sudo for `/usr/bin/log`.
- If shared guardrails are available locally, review them; otherwise follow this repo's guidance.
- SwiftUI state management (iOS/macOS): prefer the `Observation` framework (`@Observable`, `@Bindable`) over `ObservableObject`/`@StateObject`; don’t introduce new `ObservableObject` unless required for compatibility, and migrate existing usages when touching related code.
- Connection providers: when adding a new connection, update every UI surface and docs (macOS app, web UI, mobile if applicable, onboarding/overview docs) and add matching status + configuration forms so provider lists and settings stay in sync.
- Version locations: `package.json`, `core-rs/core/Cargo.toml`.
- When asked to open a “session” file, open the Pi session logs under `~/.deneb/agents/<agentId>/sessions/*.jsonl` (use the `agent=<id>` value in the Runtime line of the system prompt; newest unless a specific ID is given), not the default `sessions.json`. If logs are needed from another machine, SSH via Tailscale and read the same path there.
- Do not rebuild the macOS app over SSH; rebuilds must be run directly on the Mac.

## Collaboration / Safety Notes

- When working on a GitHub Issue or PR, print the full URL at the end of the task.
- When answering questions, respond with high-confidence answers only: verify in code; do not guess.
- Patching dependencies requires explicit approval; do not do this by default.
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
- Bug investigations: read source code and all related local code before concluding; aim for high-confidence root cause.
- Code style: add brief comments for tricky logic; keep files under ~500 LOC when feasible (split/refactor as needed).
- Never send streaming/partial replies to external messaging surfaces (WhatsApp, Telegram); only final replies should be delivered there. Streaming/tool events may still go to internal UIs/control channel.
- For manual `deneb message send` messages that include `!`, use the heredoc pattern noted below to avoid the Bash tool’s escaping.
- Release guardrails: do not change version numbers without operator’s explicit consent.

## DGX Spark Production Build

- `make gateway-dgx` — Full production binary: Go gateway + Rust core (Vega FTS + semantic search + CUDA GGUF inference).
- CUDA 없는 환경: `make rust-vega` (FTS-only 모드).
- 환경 변수: `VEGA_MODEL_EMBEDDER`, `VEGA_MODEL_RERANKER`, `VEGA_MODEL_EXPANDER` (GGUF 경로).
- 모델 자동 감지: `~/.deneb/models/*.gguf` (`gateway-go/internal/vega/autodetect.go`).
