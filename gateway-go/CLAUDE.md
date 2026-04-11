# Go Gateway Module

Go HTTP/WS gateway server — the primary Deneb runtime.

## Build & Test

| Command | Description |
|---------|-------------|
| `make go` | Build |
| `make go-dev` | Dev mode with auto-restart on SIGUSR1 |
| `make go-test` | Run tests with `-race` |
| `make go-vet` | Run `go vet` |
| `make go-fmt` | Check formatting |

## Directory Map

| Directory | Purpose |
|-----------|---------|
| `cmd/gateway/` | Entry point (`main.go`), `--port`/`--bind` flags, graceful shutdown |
| `internal/runtime/server/` | HTTP server: `/health`, `/api/v1/rpc`, OpenAI APIs, hooks |
| `internal/runtime/rpc/` | Registry-based RPC dispatcher, 150+ methods |
| `internal/runtime/session/` | Session lifecycle state machine (`IDLE → RUNNING → DONE/FAILED/KILLED/TIMEOUT`) |
| `internal/pipeline/chat/` | System prompt, tool registration, context files, slash commands |
| `internal/ai/llm/` | LLM client, sampling parameters, multimodal types |
| `internal/platform/telegram/` | Telegram channel plugin (primary deployment target) |
| `pkg/protocol/` | Hand-written JSON wire types |

## Common Tasks

### Adding a New RPC Method
1. Define the method in `internal/runtime/rpc/methods.go`
2. Register in `internal/runtime/rpc/register.go`
3. Add handler in `internal/runtime/rpc/handler/`
4. Follow existing patterns for request/response types

### Adding a New Agent Tool
1. Add schema to `internal/pipeline/chat/toolreg/tool_schemas.json`, run `make tool-schemas`
2. Implement handler in `internal/pipeline/chat/tools/<name>.go`
3. Register in `internal/pipeline/chat/toolreg/core.go` (appropriate Register*Tools function)

### Working with Generated Files

Several files in this module are machine-generated. **Never edit them by hand.**

| File | Source | Command |
|------|--------|---------|
| `internal/pipeline/chat/toolreg/tool_schemas_gen.go` | `internal/pipeline/chat/toolreg/tool_schemas.json` | `make tool-schemas` |
| `internal/pipeline/chat/tool_classification_gen.go` | `internal/pipeline/chat/tool_classification.json` | `make data-gen` |

To modify a generated file: edit the source or generator, run the `make` target, commit both together. CI will reject any PR where the generated output diverges from its source.

### Modifying System Prompt
- Assembly: `internal/pipeline/chat/prompt/system_prompt.go`
- Context files: `internal/pipeline/chat/prompt/context_files.go` (loads CLAUDE.md, SOUL.md, etc.)
- Silent replies: `internal/pipeline/chat/silent_reply.go` (NO_REPLY token)
- Slash commands: `internal/pipeline/chat/slash_commands.go` (/reset, /status, /kill, /model, /think)

### Changing Wire Types
- Hand-written types: `pkg/protocol/`

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
| `internal/platform/telegram/bot.go` | Gateway WebSocket connection, heartbeating, event dispatch |
| `internal/platform/telegram/client.go` | Telegram REST API client (send/edit messages, files, interactions) |
| `internal/platform/telegram/config.go` | Channel config (bot token, guild, allowed channels, workspaces) |
| `internal/platform/telegram/plugin.go` | `channel.Plugin` implementation, lifecycle, slash command registration |
| `internal/platform/telegram/types.go` | Telegram API types (Message, Embed, Component, Interaction, etc.) |
| `internal/platform/telegram/components.go` | Button builders: context-aware action buttons per outcome type |
| `internal/platform/telegram/embed_format.go` | Embed builders: test results, errors, dashboard, help |
| `internal/platform/telegram/format.go` | Reply formatter: code block collapsing, chunking, file extraction |
| `internal/platform/telegram/reply_analysis.go` | Reply outcome classifier + Korean error translation for vibe coders |
| `internal/platform/telegram/slash_commands.go` | Application command registration (vibe-coder commands only) |
| `internal/platform/telegram/send.go` | SendText helper with auto-chunking |
| `internal/runtime/server/inbound.go` | Inbound message processing, quick commands, autoreply pipeline |
| `internal/runtime/server/server_chat_telegram.go` | Reply pipeline: dedup → draft edit → send |
| `internal/pipeline/chat/prompt/system_prompt.go` | `BuildCodingSystemPrompt()` — vibe coder agent instructions |

### Reply Pipeline (agent response → Telegram message)

The reply pipeline is in `server_chat_telegram.go:wireTelegramChatHandler()`:

1. **Dedup** — 10-second cache prevents duplicate sends
2. **Draft edit** — edits streaming draft in-place with final text (prevents flicker)
3. **Send** — chunks text via `SendText`, attaches buttons to last chunk

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
