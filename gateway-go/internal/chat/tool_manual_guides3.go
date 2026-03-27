package chat

const memoryGuide = `Deneb memory is plain Markdown in the agent workspace. Files on disk are the source of truth.

## Memory Files
Two-layer layout:
- memory/YYYY-MM-DD.md: daily log (append-only). Today + yesterday loaded at session start.
- MEMORY.md: curated long-term memory (optional). Only loaded in main private session.

Location: agents.defaults.workspace (default ~/.deneb/workspace)

## Memory Tools
- memory_search: keyword/semantic search over MEMORY.md + memory/*.md
- memory_get: read specific file/line range. Degrades gracefully if file missing.

## When to Write
- Decisions, preferences, durable facts -> MEMORY.md
- Day-to-day notes, running context -> memory/YYYY-MM-DD.md

## Auto Memory Flush (Pre-Compaction)
Silent agentic turn before compaction to store durable notes.
Config: agents.defaults.compaction.memoryFlush (enabled by default, softThresholdTokens: 4000)

## Vector Memory Search
- Hybrid: BM25 + vector weighted fusion
- MMR re-ranking for diversity (optional)
- Temporal decay for recency boost (optional, halfLife=30 days)
- Index: ~/.deneb/memory/<agentId>.sqlite
- Providers: local, openai, gemini, voyage, mistral, ollama

## Key Files
- docs/concepts/memory.md
- gateway-go/internal/chat/tool_memory.go`

const sessionsGuide = `Sessions represent individual conversations with lifecycle management.

## Session Keys
- Direct: agent:<agentId>:<mainKey> (default "main")
- Groups: agent:<agentId>:<channel>:group:<id>
- Cron: cron:<job.id>, Webhooks: hook:<uuid>

## DM Scope (session.dmScope)
- main (default): all DMs share one session
- per-peer, per-channel-peer, per-account-channel-peer: isolation options

## Lifecycle State Machine
IDLE -> RUNNING -> DONE / FAILED / KILLED / TIMEOUT
Runs serialized per session key (lane-based queuing).

## Reset Policy
- Daily reset: 4:00 AM local time (default)
- Idle reset: optional sliding window
- Manual: /new or /reset commands

## Session Tools
sessions_list, sessions_history, sessions_search, sessions_restore, sessions_send, sessions_spawn

## Storage
- Store: ~/.deneb/agents/<agentId>/sessions/sessions.json
- Transcripts: <SessionId>.jsonl

## Key Files
- docs/concepts/session.md
- gateway-go/internal/session/`

const architectureGuide = `Deneb: multi-language gateway with three cooperating runtimes.

## Three Runtimes
1. Go Gateway (primary): HTTP/WS, RPC (130+ methods), sessions, auth, cron
2. Rust Core (CGo FFI): protocol, security, media (21 formats), markdown, memory search (SIMD), context engine, compaction
3. Node.js Plugin Host (subprocess): channels, skills, providers via TypeScript SDK

## IPC Boundaries
- Go <-> Rust: CGo FFI (zero overhead, in-process)
- Go <-> Node.js: Unix socket + frame protocol
- CLI <-> Gateway: WebSocket
- Proto schemas: shared cross-language types

## Rust Crates (core-rs/)
- deneb-core: 30+ FFI exports. Modules: protocol, security, media, memory_search, markdown, context_engine, compaction, parsing
- deneb-vega: SQLite FTS5 search
- deneb-ml: GGUF inference (llama-cpp-2, optional cuda)
- deneb-agent-runtime: agent lifecycle, model selection
Feature flags: default -> vega -> ml -> cuda -> dgx

## Hardware Profiles
- DGX Spark: 10 concurrency, 8 embedding batch, CUDA
- Desktop GPU: 8 concurrency, 6 batch, CUDA
- CPU-only: 4 concurrency, 2 batch, software

## Gateway Internal (gateway-go/internal/)
server/, rpc/, session/, channel/, chat/, auth/, ffi/, vega/

## Key Files
- docs/concepts/architecture.md
- gateway-go/cmd/gateway/main.go
- core-rs/core/src/lib.rs`

const channelsGuide = `Channels are messaging surface plugins connecting external platforms to Deneb.

## Channel Plugin Registry
- gateway-go/internal/channel/: Plugin interface with Meta + Capabilities
- Lifecycle manager: concurrent start/stop/health-check

## Primary Channel: Telegram
- 4096-char message limit, MarkdownV2 parse mode
- Inline keyboards, 50 MB file upload limit
- Forum topics: isolated sessions per thread
- Grammy framework

## Channel Routing
1. Channel plugin receives inbound message
2. Session key resolved (dmScope + chat type)
3. Agent run triggered via RPC
4. Response delivered back through same channel

## Supported Channels
Telegram (primary), Discord, Slack, WhatsApp (Baileys), Signal, iMessage, WebChat, extensions

## Design Philosophy
- Telegram-only optimization over cross-channel compatibility
- Single operator, single user
- Korean language first

## Key Files
- gateway-go/internal/channel/registry.go
- docs/channels/telegram.md, channel-routing.md, groups.md`

const telegramGuide = `Telegram is Deneb's primary and most optimized channel, using the grammY framework with native Bot API support.

## Bot Setup
- Token: channels.telegram.botToken in config or TELEGRAM_BOT_TOKEN env
- BotFather: /newbot to create, /setprivacy for group visibility, /setjoingroups for admin control
- No "deneb channels login" needed; set token then start gateway

## Access Control
- DM Policy: pairing (default), allowlist, open, disabled
- Group Policy: open, allowlist (default), disabled
- User IDs: numeric (e.g. 8734062810); telegram:/tg: prefixes auto-normalized
- Group IDs: negative chat IDs (e.g. -1001234567890)
- groupAllowFrom filters senders per group; falls back to allowFrom

## Message Constraints
- 4096-char message limit (MarkdownV2 parse mode)
- File upload: 50 MB max for media
- Inline keyboards for interactive buttons/callbacks
- Reactions tracked per message (ACK)

## Forum Topics
- Forum topics embed :topic:<topicId> in group keys
- Each topic gets a separate session for isolation
- Non-forum groups → group chat session; forum groups → :topic:1 (general topic)
- MessageThreadId and IsForum in group message metadata

## Polling vs Webhook
- Long polling (default): /getUpdates
- Webhook (optional): POST delivery for lower latency

## Privacy Mode
- Default: bot only sees /commands in groups unless admin or privacy disabled
- /setprivacy toggle requires bot removal/re-add to take effect
- Admin status required for always-on group behavior

## Key Files
- docs/channels/telegram.md
- gateway-go/internal/channel/registry.go`

const skillsGuide = `Skills are modular capability packages that extend the agent. Each skill is a directory with a SKILL.md file.

## Skill Sources (override order, later wins)
1. extra (extra-dirs)
2. bundled (~/.deneb/bundled-skills)
3. managed (~/.deneb/skills)
4. agents-personal (~/.agents/skills)
5. agents-project (workspace/.agents/skills)
6. workspace (workspace/skills)

## Discovery
- Scans for SKILL.md one level deep (dir/SKILL.md or dir/*/SKILL.md)
- Max 300 candidates per root, 200 loaded per source
- SKILL.md capped at 256KB
- Frontmatter parsed for name, description, metadata

## Eligibility Checks
- Explicit disable: config.skills.entries[key].enabled = false
- Bundled allowlist: config.skills.allowBundled restricts bundled skills
- Runtime: platform (OS), binary availability, env vars, config file existence
- metadata.always: true bypasses all requirements

## Prompt Injection (gateway-go/internal/skills/prompt.go)
BuildSkillsPrompt() injects skills into system prompt:
- Full format: name + description + location in <skill> XML tags
- Compact fallback: name + path only (when full exceeds budget)
- Budget: max 150 skills, 30,000 chars total
- Home directory compressed to ~/ for token savings

## Matching Logic
- Agent reads skill list from system prompt <available_skills> block
- Pattern-matches user task to skill name/description
- Uses read tool to load SKILL.md content at <location> path
- No explicit invocation API; agent decides autonomously

## Skill Metadata
- always: bool (skip eligibility), emoji: string, homepage: string
- os: ["darwin","linux","windows"]
- requires: {bins, anyBins, env, config}
- install: [{method: "brew"|"npm"|"uv", package: "..."}]

## Key Files
- gateway-go/internal/skills/catalog.go, discovery.go, prompt.go, eligibility.go
- docs/concepts/skills.md`

const pilotGuide = `Pilot is a fast local AI (sglang) that orchestrates tool execution in a single round-trip, avoiding sequential tool calls.

## Execution Pipeline
1. sglang health check (cached 30s TTL, /v1/models probe)
2. Source execution (parallel): all unconditional sources via ToolRegistry, 30s per-source timeout
3. Post-processing (optional): filter_lines, head, tail, unique, sort
4. LLM analysis (local sglang): gathered data + task
5. Chaining (optional): if chain=true, LLM can request follow-up tool calls
6. Output formatting: text/json/list + length hints (brief/normal/detailed)

## Shortcuts (auto-expanded to sources)
- file: read file, files: read multiple files
- exec: run command (15s timeout)
- grep: search pattern (optional path)
- find: find files by pattern (optional path)
- url: fetch URL, http: GET via http tool
- kv_key: KV store get, memory: search memory

## Conditional Sources
- only_if: execute only if named source succeeded
- skip_if: skip if named source succeeded
- Enables dynamic tool call graphs within one pilot call

## Thinking Mode (auto-enabled)
- Triggers: source count >= 3 or analysis keywords (analyze, compare, review, debug, diagnose)
- Korean keywords: 분석, 비교, 리뷰, 디버그, 문제, 원인, 검토
- Max tokens: 4096 normal, 6144 with thinking, 1024 brief

## Post-Processing
- filter_lines: regex filter (max 50 results)
- head/tail: first/last N lines
- unique: deduplicate, sort: sort lines

## Fallback
- sglang unavailable: returns raw tool results directly
- LLM call fails: graceful degradation with raw results

## Limits
- Pilot timeout: 2 minutes total
- Per-source: 30s, Max sources: 10
- Max input chars: 24,000 (auto-truncate)

## Key Files
- gateway-go/internal/chat/tools_pilot.go`

const cronGuide = `Cron manages scheduled jobs that trigger agent turns at configured intervals.

## Job Definition
- ID, AgentID, Name, Description, Enabled, DeleteAfterRun
- Schedule: cron expression or interval
- Payload: message (agent prompt) or systemEvent
- SessionTarget: main | isolated | current
- WakeMode: next-heartbeat | now (immediate retry)

## Session Key Patterns
- Agent turn: ResolveCronAgentSessionKey(agentID, jobID)
- Single run: ResolveCronRunSessionKey(agentID, jobID, runAtMs)
- Custom sessionKey: preserves transcript across cycles
- Session reaper: cleans up old sessions (24h default retention)

## Delivery Modes
- none: no delivery (silent execution)
- announce: send via announcement channel
- webhook: POST to webhook URL
- BestEffort: continue even if delivery fails

## Failure Handling
- FailureAlert: {after: N, channel, to, cooldownMs, mode}
- Alerts after N consecutive failures
- Cooldown prevents alert spam
- Error kinds: delivery-target, timeout, agent error

## Execution States
- ok: job completed successfully
- error: job failed (with error message)
- skipped: job skipped (e.g. disabled during execution)

## Isolated Agent Execution
- Each job runs as an isolated agent turn with configurable timeout (default 5min)
- Supports model override, thinking mode, fallback providers
- DeliveryTarget for result routing

## Service Lifecycle
- Start: loads jobs, schedules enabled ones, arms timer
- Error backoff: min 2s gap between refires
- Max timer delay: 60s
- Events: job_started, job_finished, job_failed, job_added, job_removed

## Key Files
- gateway-go/internal/cron/service.go, types.go, scheduler.go, isolated_agent.go
- docs/automation/cron.md`

const autonomousGuide = `Autonomous mode runs goal-driven agent cycles without user interaction.

## Why Autonomous?
Normal agent operation is reactive: the agent only runs when the user sends a message.
This means sub-agent results delivered via session_send, scheduled checks, and deferred
follow-ups sit unprocessed until the user speaks. Autonomous mode solves this by giving
the agent its own turn — each cycle checks pending work (e.g. sub-agent callbacks,
monitoring alerts) and acts on it proactively, enabling true "I'll let you know when
it's done" behavior.

## Goals
- Max 20 goals, each with: description, priority (high/medium/low), status (active/completed/paused)
- NoteHistory: last 3 progress notes (newest first)
- CycleCount: times worked, LastWorkedAtMs: last cycle timestamp
- Completed goals auto-purge after 7 days

## Cycle Execution
- Session key: autonomous:cycle (shared transcript)
- Decision prompt includes: active goals, last cycle summary, recently changed goals
- Agent selects highest-priority feasible goal, executes tools, reports progress
- Output: goal_update JSON block with {id, status, note}

## Status Transitions
- active → completed: fully achieved, verified
- active → paused: blocked by external dep, permission, or impossibility
- paused → active: user reactivates (paused reason cleared)

## Stale Goal Detection
- IsStale(): CycleCount >= 5 and last 3 notes share same 50-char prefix
- Flagged with ⚠️ 반복 정체 in prompt
- Auto-pause after 10 stale cycles

## Starvation Detection
- Goal flagged if never worked (CycleCount == 0) while others have
- Flagged if idle > 3 cycle intervals (~30 min)
- Marked with ⚠️ 장기 미작업 in prompt

## Enable/Disable
- enabled=true (default): cycles run on schedule
- enabled=false: timer paused, manual cycle_run still works
- State persisted in CycleState

## Memory Consolidation
- Optional Dreamer integration: post-cycle memory consolidation
- Events: dreaming_started, dreaming_completed, dreaming_failed

## Service Status
- Running, Enabled, CycleRunning, ActiveGoals, TotalGoals
- LastCycleAt, ConsecutiveErr, TotalCycles, SuccessRate, AvgDurationMs

## Key Files
- gateway-go/internal/autonomous/goal.go, cycle.go, service.go
- docs/automation/autonomous.md`
