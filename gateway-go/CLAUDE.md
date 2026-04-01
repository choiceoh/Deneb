# Go Gateway Module

Go HTTP/WS gateway server — the primary Deneb runtime.

## Build & Test

| Command | Description |
|---------|-------------|
| `make go` | Build (CGo, requires `make rust` first) |
| `make go-pure` | Build without Rust (`CGO_ENABLED=0 -tags no_ffi`) |
| `make go-dev` | Dev mode with auto-restart on SIGUSR1 |
| `make go-test` | Run tests with `-race` |
| `make go-test-pure` | Run tests without FFI |
| `make go-vet` | Run `go vet` |
| `make go-fmt` | Check formatting |

## Directory Map

| Directory | Purpose |
|-----------|---------|
| `cmd/gateway/` | Entry point (`main.go`), `--port`/`--bind` flags, graceful shutdown |
| `internal/server/` | HTTP server: `/health`, `/api/v1/rpc`, OpenAI APIs, hooks |
| `internal/rpc/` | Registry-based RPC dispatcher, 130+ methods |
| `internal/session/` | Session lifecycle state machine (`IDLE → RUNNING → DONE/FAILED/KILLED/TIMEOUT`) |
| `internal/chat/` | System prompt, tool registration, context files, slash commands |
| `internal/ffi/` | CGo bindings to Rust core (8 modules) |
| `internal/auth/` | Token auth, allowlists, credentials |
| `internal/llm/` | LLM client, sampling parameters, multimodal types |
| `internal/vega/` | Vega search integration, model auto-detection |
| `internal/telegram/` | Telegram channel plugin (primary deployment target) |
| `pkg/protocol/` | Hand-written JSON wire types + generated protobuf types in `gen/` |

## FFI Pattern

Rust core is linked as a static library via CGo. Each FFI module has two files:

- `*_cgo.go` — CGo implementation (build tag: `!no_ffi && cgo`)
- `*_noffi.go` — Pure-Go fallback (build tag: `no_ffi || !cgo`)

Modules: `core`, `memory`, `markdown`, `parsing`, `context_engine`, `compaction`, `vega`

FFI error codes in `internal/ffi/ffi_error_codes_gen.go` are generated from `core-rs/core/src/ffi_utils.rs` via `make ffi-gen`.
Protocol error codes (`ErrorCode` enum) are defined in `proto/gateway.proto` and auto-generated into `core-rs/core/src/protocol/error_codes.rs` via `make proto-error-codes-gen`.

## Common Tasks

### Adding a New RPC Method
1. Define the method in `internal/rpc/methods.go`
2. Register in `internal/rpc/register.go`
3. Add handler in `internal/rpc/handler/`
4. Follow existing patterns for request/response types

### Adding a New Agent Tool
1. Add schema to `internal/chat/toolreg/tool_schemas.yaml`, run `make tool-schemas`
2. Implement handler in `internal/chat/tools/<name>.go`
3. Register in `internal/chat/toolreg/core.go` (appropriate Register*Tools function)

### Working with Generated Files

Several files in this module are machine-generated. **Never edit them by hand.**

| File | Source | Command |
|------|--------|---------|
| `internal/chat/toolreg/tool_schemas_gen.go` | `internal/chat/toolreg/tool_schemas.yaml` | `make tool-schemas` |
| `internal/autoreply/thinking/model_caps_gen.go` | `internal/autoreply/thinking/model_caps.yaml` | `make model-caps` |
| `internal/ffi/ffi_error_codes_gen.go` | `core-rs/core/src/ffi_utils.rs` | `make ffi-gen` |
| `pkg/protocol/gen/*.pb.go` | `proto/*.proto` | `make proto` |

To modify a generated file: edit the source or generator, run the `make` target, commit both together. CI will reject any PR where the generated output diverges from its source.

### Modifying System Prompt
- Assembly: `internal/chat/system_prompt.go`
- Context files: `internal/chat/context_files.go` (loads CLAUDE.md, SOUL.md, etc.)
- Silent replies: `internal/chat/silent_reply.go` (NO_REPLY token)
- Slash commands: `internal/chat/slash_commands.go` (/reset, /status, /kill, /model, /think)

### Changing Wire Types
- Hand-written types: `pkg/protocol/`
- Generated types: `pkg/protocol/gen/` (from `make proto`)
- **Must pass**: `pkg/protocol/consistency_test.go` (bidirectional sync check)

---

## Telegram Coding Channel — Architecture & Design Direction

Telegram is the **coding-specialized agent I/O channel**. Unlike Telegram (conversation-focused), Telegram is purpose-built for vibe coding: the user describes what they want in natural Korean, and the agent does all the coding autonomously.

### Critical Context: The User is a Vibe Coder

The sole user **does not read or write code**. All development is done through natural language instructions to the AI agent. This is the single most important design constraint for the Telegram channel:

- **Never show raw code, diffs, or source files** in Telegram messages. The user cannot read them.
- **Always explain in Korean** what was changed and why, in non-technical terms.
- **Automate verification** — the user cannot manually run builds or tests. The system must do it automatically and report results visually.
- **One-click workflows** — use Telegram buttons for next steps (commit, push, fix, etc.) instead of requiring the user to type commands.

### File Map

| File | Purpose |
|------|---------|
| `internal/telegram/bot.go` | Gateway WebSocket connection, heartbeating, event dispatch |
| `internal/telegram/client.go` | Telegram REST API client (send/edit messages, files, interactions) |
| `internal/telegram/config.go` | Channel config (bot token, guild, allowed channels, workspaces) |
| `internal/telegram/plugin.go` | `channel.Plugin` implementation, lifecycle, slash command registration |
| `internal/telegram/types.go` | Telegram API types (Message, Embed, Component, Interaction, etc.) |
| `internal/telegram/components.go` | Button builders: context-aware action buttons per outcome type |
| `internal/telegram/embed_format.go` | Embed builders: progress, test results, errors, dashboard, help |
| `internal/telegram/format.go` | Reply formatter: code block collapsing, chunking, file extraction |
| `internal/telegram/progress.go` | ProgressTracker: edits a single embed in-place for real-time tool status |
| `internal/telegram/reply_analysis.go` | Reply outcome classifier + Korean error translation for vibe coders |
| `internal/telegram/slash_commands.go` | Application command registration (vibe-coder commands only) |
| `internal/telegram/thread_namer.go` | Auto thread naming via local sglang LLM |
| `internal/telegram/send.go` | SendText helper with auto-chunking |
| `internal/server/inbound_telegram.go` | Inbound message processing, quick commands, workspace context injection |
| `internal/server/server_chat.go` | Reply pipeline: formatting → buttons → error translation → auto-verify |
| `internal/chat/prompt/system_prompt.go` | `BuildCodingSystemPrompt()` — vibe coder agent instructions |

### Reply Pipeline (agent response → Telegram message)

The reply pipeline is a decorator chain in `server_chat.go:wireTelegramChatHandler()`:

1. **ProgressTracker finalize** — marks all tool steps as done, sends final progress embed
2. **Dedup** — 10-second cache prevents duplicate sends
3. **FormatReply** — extracts large code blocks (≥200 chars) as file attachments, collapses remaining code blocks into Korean summaries like `_(go 코드, 42줄)_`
4. **AnalyzeReply** — classifies the reply outcome (code change, test pass/fail, build fail, commit, error, general)
5. **ContextButtons** — selects appropriate buttons per outcome (commit→push, error→fix, test pass→commit+push, etc.)
6. **Send** — chunks text, attaches buttons to last chunk
7. **sendVibeCoderFollowUps** — post-reply follow-ups:
   - Error Korean translation embed (when errors/failures detected)
   - Auto build/test verification embed (when code changes detected)

### Quick Commands (Telegram-only)

Only 4 commands exist. All developer-focused commands (file, grep, run, blame, stash, etc.) were **intentionally removed** because the user is a vibe coder:

| Command | Action |
|---------|--------|
| `/dashboard` (aliases: `/d`, `/status`, `/ws`) | Visual project health panel (branch, changes, build, test) |
| `/commit [message]` | Stage all + commit, show push/new-task buttons |
| `/push` | Push current branch to remote |
| `/help` | Show vibe-coder-friendly help (no developer commands listed) |

### Button Interaction Flow

Telegram buttons embed `action:sessionKey` in their `custom_id`. When clicked:

1. `HandleTelegramInteraction` parses the action
2. Most actions (test, commit, fix, revert, details) dispatch an agent message via `chat.send`
3. `push` runs git push inline for instant feedback
4. `new` clears the session and starts fresh

### Progress Tracking

Tool names are automatically translated to Korean in the progress embed:
- `exec` → 명령어 실행, `write` → 파일 작성, `edit` → 파일 수정, `grep` → 코드 검색, etc.
- Mapping is in `progress.go:toolNameKorean`

### System Prompt (`BuildCodingSystemPrompt`)

The coding system prompt explicitly instructs the agent:
- Never show raw code or diffs
- Always respond in Korean
- After code changes, provide structured summary: 📝 변경 요약 → 🔨 빌드 → 🧪 테스트
- Translate error messages to non-technical Korean
- Recommend choices clearly ("보통은 A가 좋습니다")

### Design Principles for Future Work

1. **Zero code exposure** — if the user sees raw code in Telegram, something is wrong. Fix the formatter or system prompt.
2. **Korean first** — all user-facing text, embeds, buttons, and error explanations must be in Korean.
3. **Automate everything** — the user cannot verify anything manually. Build, test, lint, and commit verification must happen automatically.
4. **Buttons over commands** — prefer one-click buttons for next steps. Typing commands is a fallback.
5. **Explain, don't show** — "로그인 검증 로직을 추가했습니다" is correct. Showing the code is not.
6. **Visual dashboards** — use embeds with fields, colors, and emojis for status. Never dump raw terminal output.
7. **Narrow scope** — Telegram is for coding tasks only. Conversation, casual chat, and general Q&A belong on Telegram. Don't add features that blur this boundary.
8. **Do not re-add developer commands** — commands like /file, /grep, /run, /blame, /stash, /checkout were intentionally removed. The agent handles all code operations through natural language.

### Adding New Features Checklist

When extending the Telegram channel:
- [ ] Does the feature respect the vibe coder constraint? (no code shown, Korean explanations)
- [ ] Are follow-up actions provided as buttons?
- [ ] Is error handling translated to Korean?
- [ ] Is the feature automated (no manual verification required)?
- [ ] Does it use embeds for visual presentation (not raw text)?
- [ ] Is the tool name mapped to Korean in `progress.go:toolNameKorean`?
