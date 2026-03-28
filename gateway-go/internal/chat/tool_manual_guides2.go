package chat

const compactionGuide = `Compaction summarizes older conversation history to stay within model context window limits.

## What It Does
- Summarizes older messages into a compact summary entry
- Keeps recent messages intact after the compaction point
- Summary persists in session JSONL history

## Auto-Compaction (default on)
Triggers when session nears/exceeds context budget (Go threshold: 85%, Rust threshold: 75%).
Deneb retries the original request with compacted context (maxCompactionRetries=2).

## Manual Compaction
/compact [instructions] — force a compaction pass with optional focus instructions.

## Configuration (deneb.json)
agents.defaults.compaction:
- model: override model for summarization (e.g. "openrouter/anthropic/claude-sonnet-4-5")
- identifierPolicy: "strict" (default, preserves opaque IDs), "off", or "custom"
- reserveTokensFloor: token reserve before compaction (default 20000)

## Pre-Compaction Memory Flush
Before compaction, Deneb runs a silent agentic turn to store durable notes to disk.
Config: agents.defaults.compaction.memoryFlush:
- enabled: true (default)
- softThresholdTokens: 4000 (triggers at contextWindow - reserve - threshold)
- One flush per compaction cycle
- Skipped if workspace is read-only

## Rust Implementation (core-rs/core/src/compaction/)
- mod.rs: CompactionConfig, CompactionReason, CompactionDecision
  - Context threshold: 0.75, fresh tail count: 8
  - Target tokens: leaf=600, condensed=900
  - Max rounds: 10, max iterations per phase: 50
- sweep.rs: hierarchical summarization state machine
  - Step-based I/O for cross-language orchestration (handle-based FFI)
  - Chunk selection with fresh-tail protection
  - compaction_new() → handle → compaction_start(handle) → compaction_step(handle, response) → compaction_drop(handle)
- timestamp.rs: timezone-aware timestamps in summaries

## Hierarchical Summarization (Aurora Sweep)
- Depth-0 (leaf): individual message summaries
- Depth-1 (condensed): groups of leaf summaries
- Higher depths: further condensation
- Fanout: min 8 depth-0 summaries before condensation, min 4 depth-1
- Fresh tail protection: recent 8 messages always excluded from compaction

## Compaction vs Pruning
- Compaction: summarizes and persists in JSONL (permanent, survives restart)
- Session pruning: trims old tool results in-memory only (temporary, per-request)

## Custom Context Engines
The active context engine owns compaction behavior:
- ownsCompaction: true → engine manages all compaction
- ownsCompaction: false → Deneb built-in auto-compaction may run

## Key Files
- docs/concepts/compaction.md
- core-rs/core/src/compaction/mod.rs, sweep.rs
- gateway-go/internal/chat/compaction.go
- gateway-go/internal/transcript/compressor.go`

const toolsGuide = `The tool system provides the AI agent with 34 capabilities to interact with the filesystem, execute commands, search the web, and more.

## Core Architecture

### ToolDef (gateway-go/internal/chat/tools.go)
- Name: tool name (case-sensitive)
- Description: one-line for LLM
- InputSchema: JSON Schema for input validation
- Fn: ToolFunc = func(ctx, input json.RawMessage) (string, error)

### ToolRegistry
- Thread-safe RWMutex, preserves registration order
- Execute(): dispatches with $ref resolution + compression + post-processing
- LLMTools(): formats for LLM API
- Names(): registration order; SortedNames(): alphabetical
- Summaries(): name → description map

### CoreToolDeps (injected at registration)
- ProcessMgr (*process.Manager): background exec sessions
- WorkspaceDir (string): agent workspace root
- CronSched (*cron.Scheduler): cron job management
- Sessions (*session.Manager): session lifecycle
- LLMClient (*llm.Client): vision/image analysis
- Transcript (TranscriptStore): history access
- SessionSendFn: cross-session message delivery
- AutonomousSvc (*autonomous.Service): goal management
All fields gracefully degrade to friendly error when nil.

## Tool Execution Flow
1. Look up tool in registry by name
2. Check for "compress": true flag (optional output summarization via sglang)
3. Resolve $ref references (waits up to 30s via refWaitTimeout)
4. Inject "_ref_content" into input JSON
5. Execute tool function with context and input
6. Apply post-processors (global then tool-specific)

## Tool Chaining ($ref)
Tools can reference other tools' output in the same turn:
{"$ref": "tool_use_id"} → injected as "_ref_content" in input
Common patterns: grep→pilot, exec→pilot, read→pilot, find→read

## Parallel Execution
Independent tools execute in parallel goroutines (WaitGroup) within each turn.
TurnContext (tool_turn_context.go) enables cross-tool result sharing.

## Post-Processing Pipeline (tool_postprocess.go)
Global processors (all tools):
- OutputTrimmer: caps at 64K chars (head+tail preserved)
- ErrorEnricher: adds actionable hints to common errors

Tool-specific:
- grep: GrepResultSummarizer — caps at 200 lines; max search results: 500
- find: FindResultSummarizer — caps at 500 entries, groups by directory; max results: 200
- exec: ExecAnnotator — emphasizes exit code on failure

## Tool Categories (34 tools)
File: read, write, edit, apply_patch, grep, find, ls
Exec: exec, process
AI: pilot (local sglang orchestrator)
Web: web (search + fetch + search+fetch), http
Memory: memory_search, memory_get, polaris
System: cron, autonomous, message, gateway
Sessions: sessions_list/history/search/restore/send/spawn, subagents, session_status
Media: image, youtube_transcript, send_file
Data: gmail, kv

## Compression
Any tool call accepts "compress": true. Large outputs auto-summarized by local sglang (SGLANG_BASE_URL, default http://127.0.0.1:30000/v1).

## Context Passing
Tools access runtime context via context.Context:
- DeliveryContext: channel/recipient info
- ReplyFunc: callback for proactive sends
- SessionKey: current session ID
- TurnContext: other tools' results in current turn
- MediaSendFn: send files to user
- TypingFn: typing indicator control

## Key Files
- gateway-go/internal/chat/tools.go (types, registry)
- gateway-go/internal/chat/tools_core.go (registration, 35 tools)
- gateway-go/internal/chat/tools_fs.go (filesystem: read/write/edit/grep/find/ls)
- gateway-go/internal/chat/tools_pilot.go (pilot orchestrator)
- gateway-go/internal/chat/tool_postprocess.go (post-processing)
- gateway-go/internal/chat/tool_web.go (web search/fetch)
- gateway-go/internal/chat/tool_http.go (HTTP API)
- gateway-go/internal/chat/tool_sessions.go (session management)
- gateway-go/internal/chat/tool_message.go (messaging)
- gateway-go/internal/chat/tool_media.go (image/youtube/send_file)
- gateway-go/internal/chat/tool_kv.go (KV store)
- gateway-go/internal/chat/tool_gmail.go (Gmail)
- gateway-go/internal/chat/tool_autonomous.go (autonomous goals)`

const systemPromptGuide = `The system prompt is the instruction set injected into every LLM call. It defines the agent's identity, available tools, and behavioral rules.

## Assembly (gateway-go/internal/chat/system_prompt.go)
Two build modes:
- BuildSystemPrompt(): single string (default, for OpenAI-compatible providers)
- BuildSystemPromptBlocks(): Anthropic ContentBlocks with cache_control breakpoints
  - Static block: identity + tooling + safety (rarely changes → "ephemeral" cache, reused across turns)
  - Dynamic block: skills + context files + runtime (changes per request → not cached)
  - This halves prompt token costs on Anthropic by caching the stable portion

## Prompt Sections (in order)
**Static block (cached on Anthropic):**
1. Identity: "You are a personal assistant running inside Deneb"
2. Tooling: compact categorized tool names — File, Exec, AI, Web, Memory, System, Sessions, Media, Data
3. Tool Usage: parallel calls, first-class tools, compress flag, auto-trimming
4. Pilot & Chaining: when to use pilot, $ref chaining patterns, when NOT to use pilot
5. Safety: no self-preservation, prioritize oversight
6. Deneb CLI Quick Reference

**Dynamic block (per-request):**
7. Skills: XML <available_skills> block (from skills/prompt.go, max 150 skills, 30K chars)
8. Memory Recall: guidance on memory_search / memory_get availability
9. Polaris: system manual tool guidance (Korean + English examples)
10. Workspace: working directory path
11. Reply Tags: [[reply_to_current]] for native replies
12. Messaging: channel routing, sessions_send, message tool
13. Response Style: Korean default, concise for Telegram (4096 char limit), bullet points
14. Current Date & Time: local time with timezone (Asia/Seoul)
15. Context Files: workspace + ancestor directories
16. Silent Replies: __SILENT_REPLY__ (NO_REPLY) token for suppressing delivery
17. Runtime: agentId, host, OS, model, channel

## Context Files (context_files.go)
Load order: CLAUDE.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, MEMORY.md
- Scans workspace + ancestors (up to 10 levels, stops at home dir)
- Budget: max 20K chars per file, 150K chars total
- Mtime-based caching with 5-minute revalidation
- Truncation: head 70% + "[...truncated...]" + tail 20%
- AGENTS.md + TOOLS.md: loaded in minimal mode (sub-agents)

## Prompt Modes
- full (default): all 17 sections included
- minimal: sub-agents get only AGENTS.md + TOOLS.md (lightweight)
- none: no system prompt (raw LLM call)

## Tool Display
- toolCategories: 9 groups (File, Exec, AI, Web, Memory, System, Sessions, Media, Data)
- Tool descriptions live in ToolDef.Description (sent via tool JSON schemas, not duplicated in prompt text)
- Only tool names listed in prompt → keeps static block small and cacheable

## Key Files
- gateway-go/internal/chat/system_prompt.go (assembly, writePolarisSection, writeMessagingSection, etc.)
- gateway-go/internal/chat/context_files.go (file loader, budget enforcement, caching)
- gateway-go/internal/chat/run.go (SystemPromptParams construction, deferred format for Anthropic)`
