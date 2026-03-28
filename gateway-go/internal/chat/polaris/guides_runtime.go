package polaris

const memoryGuide = `Deneb memory is plain Markdown in the agent workspace. Files on disk are the source of truth.

## Memory Files
Two-layer layout:
- memory/YYYY-MM-DD.md: daily log (append-only). Today + yesterday loaded at session start.
- MEMORY.md: curated long-term memory (optional). Only loaded in main private session.

Location: agents.defaults.workspace (default ~/.deneb/workspace)

## Memory Tools
- memory_search: keyword/semantic search over MEMORY.md + memory/*.md. Returns ranked snippets with file + line info.
- memory_get: read specific file/line range. Params: file (required), startLine, endLine. Degrades gracefully if file missing.

## When to Write (via write tool, not memory tools)
- Decisions, preferences, durable facts → MEMORY.md (append to relevant section)
- Day-to-day notes, running context → memory/YYYY-MM-DD.md (append chronologically)
- Memory tools are read-only; use the write/edit tools to modify memory files

## Auto Memory Flush (Pre-Compaction)
Silent agentic turn before compaction to store durable notes from conversation.
Config: agents.defaults.compaction.memoryFlush:
- enabled: true (default)
- softThresholdTokens: 4000 (triggers at contextWindow - reserve - threshold)
- One flush per compaction cycle; skipped if workspace is read-only

## Vector Memory Search
- Hybrid: BM25 (keyword) + vector (semantic) weighted fusion
- MMR re-ranking: Maximal Marginal Relevance for result diversity (avoids duplicate-ish results)
- Temporal decay: recency boost with configurable halfLife (default 30 days)
- Index: ~/.deneb/memory/<agentId>.sqlite (auto-created on first search)
- Embedding providers: gemini, openai, voyage, mistral, ollama
- Provider config: agents.defaults.memory.embedding.provider + model

## Importance Extraction
On message ingest, memory system extracts importance score (0-1) to prioritize what to index. JSON parse from LLM output; failures logged but don't block.

## Key Files
- docs/concepts/memory.md
- gateway-go/internal/chat/tool_memory.go (memory_search, memory_get tools)
- gateway-go/internal/memory/ (Store, Embedder, indexing)

## Common Tasks
- Search memory: memory_search(query:'user timezone preference')
- Read today's log: read(file_path:'memory/2026-03-28.md')
- Store a decision: write(file_path:'MEMORY.md', content:'## Decision: ...', mode:'append')

## Gotchas
- memory_search is read-only; use write/edit tools to modify memory files
- MEMORY.md only loaded in main private session, not sub-agent sessions
- Auto-flush before compaction is skipped if workspace is read-only`

const sessionsGuide = `Sessions represent individual conversations with lifecycle management.

## Session Keys
- Direct DM: agent:<agentId>:<mainKey> (default "main")
- Groups: agent:<agentId>:<channel>:group:<id>
- Forum topics: agent:<agentId>:<channel>:group:<id>:topic:<topicId>
- Cron: cron:<job.id> or cron:<job.id>:<runAtMs> (isolated)
- Webhooks: hook:<uuid>
- Sub-agents: <parentKey>:<label>:<unixMs>

## DM Scope (session.dmScope)
- main (default): all DMs share one session
- per-peer: one session per peer user
- per-channel-peer: per channel + peer
- per-account-channel-peer: per account + channel + peer

## Lifecycle State Machine
IDLE → RUNNING → DONE / FAILED / KILLED / TIMEOUT
- Runs serialized per session key (lane-based queuing)
- State transitions validated (e.g., cannot go from DONE back to RUNNING)
- Proto: SessionRunStatus, SessionLifecyclePhase, SessionLifecycleEvent

## Queue Modes (channel-driven)
- collect: batch inbound messages, single agent run
- steer: inject into running agent as tool result
- followup: queue as next run after current completes

## Reset Policy
- Daily reset: 4:00 AM local time (default, agents.defaults.session.dailyResetHour)
- Idle reset: optional sliding window (agents.defaults.session.idleResetMinutes)
- Manual: /new or /reset slash commands
- Session reaper: cleans up old sessions (24h default retention)

## Session Tools (8 tools)
- sessions_list: browse active sessions (limit=50, filter by kind: main/group/cron/hook)
- sessions_history: read past messages (limit=20)
- sessions_search: full-text search across transcripts (maxResults=20, max 100)
- sessions_restore: import history from another session
- sessions_send: send message to another session
- sessions_spawn: create isolated sub-agent (task + label + model)
- subagents: list/kill/steer running sub-agents
- session_status: current session info (key, time, kind, status, model, tokens)

## Session GC
- gcInterval: 10 minutes (scan frequency for stale sessions)
- gcMaxAge: 1 hour (terminal sessions evicted after this)
- Evicts: done/failed/killed/timeout sessions older than gcMaxAge

## Storage
- Store: ~/.deneb/agents/<agentId>/sessions/sessions.json (metadata)
- Transcripts: <SessionId>.jsonl (message history, append-only)
- Event pub/sub bus for real-time session state changes
- KeyCache: 256 entries, 1s negative TTL for session key lookups

## Key Files
- docs/concepts/session.md
- gateway-go/internal/session/ (Manager, lifecycle, state machine)
- gateway-go/internal/chat/tool_sessions.go (8 session tools)

## Common Tasks
- Check current session: session_status()
- Reset session: /new or /reset
- View session state machine: grep(pattern:'IDLE.*RUNNING\|SessionRunStatus', path:'gateway-go/internal/session/')

## Gotchas
- Daily reset at 4 AM local; sessions started before reset carry over until idle timeout
- State transitions are validated; cannot go DONE → RUNNING (must create new run)
- Session GC evicts terminal sessions after 1 hour; in-memory data is lost`

const architectureGuide = `Deneb: multi-language gateway with three cooperating runtimes on DGX Spark hardware.

## Three Runtimes
1. **Go Gateway** (primary): HTTP/WS server, RPC (130+ methods), sessions, auth, cron, chat/agent loop
2. **Rust Core** (CGo FFI): protocol validation, security, media detection (21 formats), markdown parsing, memory search (SIMD cosine), context engine, compaction, parsing utilities
3. **Node.js Plugin Host** (subprocess): channels, skills, providers via TypeScript SDK

## IPC Boundaries
- Go ↔ Rust: CGo FFI (zero overhead, in-process). Build tag: !no_ffi && cgo
- Go ↔ Node.js: Unix socket + frame protocol (subprocess)
- CLI ↔ Gateway: WebSocket
- Proto schemas (proto/): shared cross-language types (gateway.proto, channel.proto, session.proto)

## Rust FFI Bridge (30+ exports)
### Error Codes
FFI_ERR_NULL_PTR=-1, INVALID_UTF8=-2, OUTPUT_TOO_SMALL=-3, INPUT_TOO_LARGE=-4, JSON=-5, OVERFLOW=-6, VALIDATION=-7, PANIC=-99

### Safety Patterns
- FFI_MAX_INPUT_LEN: 16 MB (DoS protection)
- ffi_catch(): wraps all exports to prevent Rust panics from crashing Go
- Output buffers grow automatically (ffiCallWithGrow helper)
- Handle-based resource management: u32 IDs for Rust objects across FFI

### FFI Function Groups (core-rs/core/src/lib.rs → gateway-go/internal/ffi/)
- **Protocol**: deneb_validate_frame, deneb_validate_error_code, deneb_validate_params
- **Security**: deneb_constant_time_eq, deneb_sanitize_html, deneb_is_safe_url (SSRF), deneb_validate_session_key (max 512 chars)
- **Media**: deneb_detect_mime (magic-byte detection, 21 formats)
- **Memory Search**: deneb_memory_cosine_similarity (SIMD, 2M cap), deneb_memory_bm25_rank_to_score, deneb_memory_build_fts_query, deneb_memory_merge_hybrid_results, deneb_memory_extract_keywords
- **Markdown**: deneb_markdown_to_ir (128-entry LRU cache, FNV1a64 hash), deneb_markdown_detect_fences
- **Parsing**: deneb_extract_links, deneb_html_to_markdown, deneb_base64_estimate, deneb_parse_media_tokens
- **Compaction**: deneb_compaction_evaluate, deneb_compaction_sweep_new/_start/_step/_drop
- **Context**: deneb_context_assembly_new/_start/_step, deneb_context_expand_new/_start/_step, deneb_context_engine_drop
- **Vega**: deneb_vega_execute, deneb_vega_search

### Stateful FFI Pattern (compaction, context engine)
*_new() → handle → *_start(handle) → *_step(handle, response) → *_drop(handle)
Rust yields commands (FetchMessages, Summarize), Go executes I/O, feeds responses back. Avoids callbacks across FFI.

## Rust Crates (core-rs/)
- deneb-core: 30+ FFI exports. Modules: protocol, security, media, memory_search, markdown, context_engine, compaction, parsing
- deneb-vega: SQLite FTS5 search engine with semantic search (pre-computed embeddings)
- deneb-agent-runtime: agent lifecycle, model selection
- Feature flags: default → vega

## RPC System (gateway-go/internal/rpc/)
- Dispatcher: routes methods, middleware chain, panic recovery
- Worker pool: 2× NumCPU workers (clamped [4, 64])
- Core methods: health.check, sessions.list/get/delete, channels.list/get/status/health, system.info

## Auth System (gateway-go/internal/auth/)
- Token format: hex(hmac-sha256(payload)):payload
- Roles: operator, viewer, agent, probe
- Scopes: admin, read, write, approvals, pairing

## Hardware Profiles
- DGX Spark: 10 concurrency, 8 embedding batch, CUDA, local SGLang inference
- Desktop GPU: 8 concurrency, 6 batch, CUDA
- CPU-only: 4 concurrency, 2 batch, software fallback

## Gateway Internal Subsystems (gateway-go/internal/, 40+)
Core: server/, rpc/, session/, channel/, chat/, auth/, ffi/
AI: llm/, provider/ (plugin registry, model discovery, auth), vega/, memory/, aurora/
Automation: cron/, hooks/
Infrastructure: config/, logging/, metrics/ (Prometheus /metrics endpoint), monitoring/, middleware/
Media: media/, liteparse/ (PDF/Office/CSV document parsing via lit CLI)
Tools: process/, plugin/, skills/, skill/
Persistence: transcript/ (JSONL session history), usage/ (token tracking)
Other: approval/, autoreply/ (agent execution engine), dedupe/, device/, events/ (pub/sub broadcasting), node/, secret/, telegram/, wizard/

## Key Files
- docs/concepts/architecture.md (20KB)
- gateway-go/cmd/gateway/main.go (entry, --port/--bind, graceful shutdown)
- core-rs/core/src/lib.rs (30+ FFI exports, error codes, constants)
- gateway-go/internal/ffi/ (8 *_cgo.go files + *_noffi.go fallbacks)

## Common Tasks
- Check FFI exports: grep(pattern:'//export deneb_', path:'core-rs/core/src/lib.rs')
- View RPC methods: grep(pattern:'Register.*Method', path:'gateway-go/internal/rpc/')
- Check gateway startup: read(file_path:'gateway-go/cmd/gateway/main.go')

## Gotchas
- FFI_MAX_INPUT_LEN is 16 MB; larger inputs return INPUT_TOO_LARGE without processing
- Rust panics across FFI are caught by ffi_catch() but log as FFI_ERR_PANIC (-99)
- RPC worker pool is 2x NumCPU (clamped 4-64); exceeding this queues requests`

const channelsGuide = `Channels are messaging surface plugins connecting external platforms to Deneb.

## Channel Plugin Interface
- Meta: channel name, version, capabilities declaration
- Capabilities: text, media, reactions, typing, threads, forums, inline keyboards, file upload
- Lifecycle: Start(), Stop(), HealthCheck() — concurrent orchestration via lifecycle manager
- Registry: thread-safe plugin registration, lookup by channel name

## Channel Routing Flow
1. Channel plugin receives inbound message from platform
2. Session key resolved based on dmScope + chat type (DM vs group vs forum)
3. Message enqueued into session lane (collect/steer/followup mode)
4. Agent run triggered via RPC (agent method)
5. Response delivered back through same channel + delivery context

## Primary Channel: Telegram (fully optimized)
- 4096-char message limit, MarkdownV2 parse mode
- Inline keyboards for interactive buttons/callbacks
- 50 MB file upload limit for media
- Forum topics: isolated sessions per thread (:topic:<topicId>)
- grammY framework, long polling (default) or webhook
- Status reactions: 👀🤔🔥⚡👍😱 (mapped to agent lifecycle phases)
- Typing signaler: 5s interval (matches Telegram TTL)

## Supported Channels
Telegram (primary, fully optimized), Discord, Slack, WhatsApp (Baileys), Signal, iMessage, WebChat, extensions
Note: only Telegram is the active deployment target per project philosophy

## Group Handling
- Group keys: agent:<agentId>:<channel>:group:<chatId>
- Forum keys: ...group:<chatId>:topic:<topicId>
- groupAllowFrom: per-group sender filter (falls back to allowFrom)
- Non-forum groups → shared group session; forum groups → per-topic session

## Key Files
- gateway-go/internal/channel/ (registry.go, lifecycle manager)
- docs/channels/telegram.md, channel-routing.md, groups.md
- gateway-go/internal/telegram/ (Telegram-specific implementation)

## Gotchas
- Only Telegram is the active deployment target; other channels exist but aren't optimized
- Forum topics create separate sessions; non-forum groups share one session
- groupAllowFrom falls back to allowFrom; missing both means no access control`

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
- groupAllowFrom: per-group sender filter; falls back to allowFrom

## Message Constraints
- 4096-char message limit (MarkdownV2 parse mode)
- File upload: 50 MB max for media (send_file tool)
- Inline keyboards for interactive buttons/callbacks
- Reactions tracked per message (ACK/status)

## Status Emojis (agent lifecycle → Telegram reactions)
- 👀 queued (run accepted, waiting in lane)
- 🤔 thinking (LLM inference in progress)
- 🔥 tool execution / coding
- ⚡ web search/fetch
- 👍 done (run completed successfully)
- 😱 error (run failed)
- 🥱 stall-soft (extended wait, still running)
- 😨 stall-hard (severely delayed)
- 🤔 compacting (context compaction in progress)

## Typing Indicator
- Interval: 5000ms (matches Telegram's 5s typing action TTL)
- Auto-started on run begin, auto-stopped on completion
- TypingModeInstant: sends immediately without initial delay

## Forum Topics
- Forum topics embed :topic:<topicId> in group keys
- Each topic gets a separate session for isolation
- Non-forum groups → group chat session; forum groups → :topic:1 (general topic)
- MessageThreadId and IsForum in group message metadata

## Session Key Format
- DM: agent:<agentId>:main (or per dmScope)
- Group: agent:<agentId>:telegram:group:<chatId>
- Forum topic: agent:<agentId>:telegram:group:<chatId>:topic:<topicId>

## Polling vs Webhook
- Long polling (default): /getUpdates (reliable, no server config needed)
- Webhook (optional): POST delivery for lower latency (requires HTTPS endpoint)

## Privacy Mode
- Default: bot only sees /commands in groups unless admin or privacy disabled
- /setprivacy toggle requires bot removal/re-add to take effect
- Admin status required for always-on group behavior

## Key Files
- docs/channels/telegram.md
- gateway-go/internal/telegram/ (Telegram-specific implementation)
- gateway-go/internal/channel/registry.go

## Gotchas
- 4096 char limit is hard; messages exceeding it are auto-split, potentially breaking formatting
- MarkdownV2 requires escaping special chars (_, *, [, ], etc.); raw markdown may render wrong
- /setprivacy toggle requires bot removal/re-add to take effect in existing groups
- User IDs are numeric; telegram:/tg: prefixes are auto-normalized but not always stripped in logs`

const skillsGuide = `Skills are modular capability packages that extend the agent. Each skill is a directory with a SKILL.md file.

## Skill Sources (override order, later wins)
1. extra (extra-dirs from config)
2. bundled (~/.deneb/bundled-skills)
3. managed (~/.deneb/skills)
4. agents-personal (~/.agents/skills)
5. agents-project (workspace/.agents/skills)
6. workspace (workspace/skills)

## Discovery
- Scans for SKILL.md one level deep (dir/SKILL.md or dir/*/SKILL.md)
- Max 300 candidates per root, 200 loaded per source
- SKILL.md capped at 256KB
- Frontmatter parsed: name, description, metadata (YAML)

## Eligibility Checks
- Explicit disable: config.skills.entries[key].enabled = false
- Bundled allowlist: config.skills.allowBundled restricts bundled skills
- Runtime requirements: platform (OS), binary availability (bins/anyBins), env vars, config file existence
- metadata.always: true bypasses all requirements

## Prompt Injection (gateway-go/internal/skills/prompt.go)
BuildSkillsPrompt() injects skills into system prompt <available_skills> block:
- Full format: name + description + location in <skill> XML tags
- Compact fallback: name + path only (when full exceeds budget)
- Budget: max 150 skills, 30,000 chars total
- Home directory compressed to ~/ for token savings

## How Agent Uses Skills
1. Agent reads skill list from system prompt <available_skills> block
2. Pattern-matches user task to skill name/description
3. Uses read tool to load SKILL.md content at <location> path
4. Follows instructions in SKILL.md to complete the task
5. No explicit invocation API — agent decides autonomously when to use a skill

## SKILL.md Format
Frontmatter fields:
- name (required): skill identifier
- description (required): one-line purpose
- always: bool (skip eligibility checks)
- emoji: string (display icon)
- homepage: string (docs URL)
- os: ["darwin","linux","windows"] (platform filter)
- requires: {bins: [], anyBins: [], env: [], config: []}
- install: [{method: "brew"|"npm"|"uv", package: "..."}]

Body: free-form Markdown instructions for the agent.

## Key Files
- gateway-go/internal/skills/catalog.go, discovery.go, prompt.go, eligibility.go
- docs/concepts/skills.md, docs/tools/skills.md

## Common Tasks
- List available skills: grep(pattern:'name:', path:'skills/*/SKILL.md')
- Check skill eligibility: grep(pattern:'requires:\|always:', path:'skills/*/SKILL.md')
- View skill prompt injection: read(file_path:'gateway-go/internal/skills/prompt.go')

## Gotchas
- Max 150 skills and 30K chars in system prompt; excess skills are dropped
- SKILL.md capped at 256KB; larger files are ignored
- Agent decides autonomously when to use skills; there's no explicit invocation API`

const pilotGuide = `Pilot is a fast local AI (sglang) that orchestrates tool execution in a single round-trip, avoiding sequential LLM calls.

## When to Use Pilot
- Multiple data gathering calls that can run in parallel (grep + read + exec)
- Quick analysis tasks that don't need the main model's full reasoning
- Chained lookups where result of one tool informs the next
- NOT for: complex multi-step reasoning, creative writing, or tasks needing full context

## Execution Pipeline
1. sglang health check (cached 30s TTL, GET /v1/models probe)
2. Source execution (parallel): all unconditional sources via ToolRegistry, 30s per-source timeout
3. Post-processing (optional): filter_lines, head, tail, unique, sort
4. LLM analysis (local sglang): gathered data + task prompt
5. Chaining (optional): if chain=true, LLM can request follow-up tool calls
6. Output formatting: text/json/list + length hints (brief/normal/detailed)

## SGLang Configuration
- Base URL: SGLANG_BASE_URL env (default: http://127.0.0.1:30000/v1)
- Model: SGLANG_MODEL env (default: Qwen/Qwen3.5-35B-A3B)
- API Key: SGLANG_API_KEY env (default: "local")
- Health probe: GET /v1/models, 3s timeout, re-probe every 5min if unavailable

## Shortcuts (auto-expanded to sources)
- file: read file, files: read multiple files
- exec: run command (15s timeout)
- grep: search pattern (optional path)
- find: find files by pattern (optional path)
- url: fetch URL, http: GET via http tool
- kv_key: KV store get, memory: search memory

## Conditional Sources
- only_if: execute only if named source succeeded (e.g., read file only if grep found matches)
- skip_if: skip if named source succeeded
- Enables dynamic tool call graphs within one pilot call

## Thinking Mode (auto-enabled)
- Triggers: source count >= 3 or analysis keywords
- English keywords: analyze, compare, review, debug, diagnose
- Korean keywords: 분석, 비교, 리뷰, 디버그, 문제, 원인, 검토
- Max tokens: 4096 normal, 6144 with thinking, 1024 brief

## Limits
- Pilot timeout: 2 minutes total
- Per-source: 30s timeout, Max sources: 10
- Max input chars: 24,000 (auto-truncated with head+tail preservation)

## Fallback
- sglang unavailable: returns raw tool results directly (no LLM analysis)
- LLM call fails: graceful degradation with raw results

## Key Files
- gateway-go/internal/chat/tools_pilot.go

## Common Tasks
- Quick file analysis: pilot(task:'이 파일 구조 설명', file:'path/to/file.go')
- Multi-grep analysis: pilot(task:'이 패턴 분석', grep:'pattern', path:'src/')
- Command output review: pilot(task:'결과 요약', exec:'make test')

## When to Use Pilot vs Direct Tools
- Need raw output only → direct tools (read, grep, exec)
- Need analysis/summary of output → pilot
- Complex multi-step reasoning → main model (not pilot)

## Gotchas
- Pilot timeout is 2 minutes total; complex source gathering may exceed this
- Max 10 sources per call; exceeding silently ignores extra sources
- sglang unavailable → raw results returned without LLM analysis`

const cronGuide = `Cron manages scheduled jobs that trigger agent turns at configured intervals.

## Job Definition (deneb.json: agents.defaults.cron.jobs[])
- id (string): unique job identifier
- agentId (string): target agent
- name, description: human-readable labels
- enabled (bool): active/inactive toggle
- deleteAfterRun (bool): one-shot jobs
- schedule: cron expression ("0 9 * * *") or interval ("every 5m", "every 1h")
- payload: {message: "prompt text"} or {systemEvent: "event_name"}
- sessionTarget: main | isolated | current
- wakeMode: next-heartbeat | now (immediate retry on failure)

## Cron Tool (agent-callable, 7 actions)
- status: service status (running, enabled, job count, next fire)
- list: all jobs with schedule, status, last run
- add: create new job (name, schedule, command required)
- update: modify existing job (jobId required)
- remove: delete job (jobId required)
- run: force-execute job now (jobId required)
- wake: arm next heartbeat timer

## Session Key Patterns
- Persistent: ResolveCronAgentSessionKey(agentID, jobID) — shared transcript across runs
- Isolated: ResolveCronRunSessionKey(agentID, jobID, runAtMs) — fresh session per run
- Custom sessionKey in job definition: overrides default resolution
- Session reaper: cleans up old isolated sessions (24h default retention)

## Delivery Modes
- none: no delivery (silent execution, result stored in transcript only)
- announce: send via configured announcement channel
- webhook: POST result to webhook URL
- BestEffort: continue even if delivery fails (don't block next run)

## Failure Handling
- FailureAlert: {after: N, channel, to, cooldownMs, mode}
- Alerts after N consecutive failures (default after: 3)
- Cooldown prevents alert spam (cooldownMs between alerts)
- Error kinds: delivery-target, timeout, agent error

## Isolated Agent Execution
- Each job runs as an isolated agent turn
- Configurable timeout (default 5min)
- Supports model override, thinking mode, fallback providers
- DeliveryTarget for result routing (channel + recipient)

## Service Lifecycle & Timers
- Start: loads jobs from config, schedules enabled ones, arms timer
- Error backoff: min 2s gap between refires (prevents rapid-fire on failures)
- Max timer delay: 60s (timer re-arms periodically even without jobs)
- Events: job_started, job_finished, job_failed, job_added, job_removed

## Key Files
- gateway-go/internal/cron/service.go, types.go, scheduler.go, isolated_agent.go
- gateway-go/internal/chat/tool_cron.go (cron tool implementation)
- docs/automation/cron-jobs.md (25KB, comprehensive user docs)

## Common Tasks
- List cron jobs: cron(action:'list')
- Add a daily job: cron(action:'add', name:'daily-report', schedule:'0 9 * * *', command:'generate report')
- Force-run a job: cron(action:'run', jobId:'daily-report')
- Check service status: cron(action:'status')

## Gotchas
- Error backoff min 2s between refires; rapid failures don't spam
- deleteAfterRun=true deletes the job definition after first run (one-shot)
- Failure alerts have cooldown; consecutive failures within cooldownMs are suppressed`

