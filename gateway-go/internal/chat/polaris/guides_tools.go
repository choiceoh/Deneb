package polaris

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
- gateway-go/internal/transcript/compressor.go

## Common Tasks
- Force compaction: /compact [optional instructions]
- Check compaction config: grep(pattern:'reserveTokensFloor\|identifierPolicy\|memoryFlush', path:'gateway-go/internal/')
- View compaction state machine: read(file_path:'core-rs/core/src/compaction/sweep.rs')

## Gotchas
- Fresh tail (8 messages) is always protected; compacting a short session may do nothing
- Memory flush runs silently before compaction; if workspace is read-only, it's skipped without error
- maxCompactionRetries=2; after that the agent run fails with context overflow`

const toolsGuide = `The tool system provides the AI agent with 42+ capabilities to interact with the filesystem, execute commands, search the web, and more.

## Core Architecture

### ToolDef (gateway-go/internal/chat/toolctx/types.go)
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

### Dependency Injection (gateway-go/internal/chat/toolctx/)
Tool dependencies are injected via typed dep structs:
- CoreToolDeps: ProcessMgr, WorkspaceDir, CronSched, Sessions, Transcript, SessionSendFn
- ProcessDeps: process manager, health checker
- SessionDeps: session manager, transcript store
- VegaDeps: Vega backend, memory store, embedder
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

## Tool Categories (42+ tools)
File: read, write, edit, grep, find
Code: multi_edit, tree, diff, analyze, test
Git: git
Exec: exec, process
AI: pilot (local sglang orchestrator), polaris (system knowledge)
Web: web (search + fetch + search+fetch), http
Memory: memory (unified fact store + file search), memory_search, vega
System: cron, message, gateway
Sessions: sessions_list/history/search/send/spawn, subagents
Media: image, youtube_transcript, send_file
Data: gmail, kv
Utilities: batch_read, search_and_read, inspect, apply_patch, health_check, agent_logs, gateway_logs

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
- gateway-go/internal/chat/toolctx/ (shared types: ToolFunc, ToolDef, ToolRegistrar, context helpers)
- gateway-go/internal/chat/toolreg/core.go (tool registration hub, wires implementations with schemas)
- gateway-go/internal/chat/toolreg/tool_schemas.yaml (JSON schema definitions, source of truth)
- gateway-go/internal/chat/tools/ (tool implementations by domain):
  - fs.go (read/write/edit), fs_search.go (grep/find), coding.go (multi_edit/diff/test)
  - exec.go (exec/process), advanced.go (batch_read/search_and_read/inspect/apply_patch)
  - memory_unified.go (unified memory), memory.go (file-based search)
  - gmail.go, http.go, kv.go, message.go, vega.go, gateway.go
  - git.go, analyze.go, health.go, send_file.go, youtube.go
  - agentlogs.go, gatewaylogs.go, morning_letter.go, test_runner.go
- gateway-go/internal/chat/tool_postprocess.go (post-processing pipeline)

## Common Tasks
- List all tool schemas: read(file_path:'gateway-go/internal/chat/toolreg/tool_schemas.yaml')
- View tool registration: read(file_path:'gateway-go/internal/chat/toolreg/core.go')
- View post-processors: read(file_path:'gateway-go/internal/chat/tool_postprocess.go')

## Gotchas
- OutputTrimmer caps at 64K chars (head+tail); large tool outputs lose middle content silently
- Tool names are case-sensitive; "Exec" won't match "exec"
- $ref waits up to 30s; if the referenced tool hasn't finished, it times out`

const systemPromptGuide = `The system prompt is the instruction set injected into every LLM call. It defines the agent's identity, available tools, and behavioral rules.

## Assembly (gateway-go/internal/chat/prompt/system_prompt.go)
Three build modes:
- BuildSystemPrompt(): single string (default, for OpenAI-compatible providers)
- BuildSystemPromptBlocks(): Anthropic ContentBlocks with cache_control (3 blocks: static + semi-static + dynamic)
- BuildCodingSystemPromptBlocks(): coding channel variant with vibe-coder optimizations

The 3-block split enables fine-grained Anthropic prompt caching:
- **Static block** (cached): rarely changes after server start
- **Semi-static block** (cached): changes only when skills are added/removed
- **Dynamic block** (per-request): changes every request

## Prompt Sections (in order)
**Static block:**
1. Identity: "You are a personal assistant running inside Deneb"
2. Tooling: compact categorized tool names — File, Code, Git, Exec, AI, Web, Memory, System, Sessions, Media, Data
3. Tool Usage: parallel calls, first-class tools, compress flag, auto-trimming, preferred CLIs (rg, fd, bat, etc.)
4. Pilot & Chaining (conditional on pilot tool): sglang guidance, $ref chaining, Korean usage hints
5. Coding: tree/analyze → edit/multi_edit → diff → test → git workflow
6. Safety: no self-preservation, prioritize oversight
7. Deneb CLI Quick Reference

**Semi-static block:**
8. Skills (mandatory): scan <available_skills>, select and read SKILL.md (max 150 skills, 30K chars)

**Dynamic block:**
9. Memory Recall: unified memory tool guidance (search/get/set/forget/status)
10. Polaris: system knowledge agent guidance (Korean + English examples)
11. Workspace: working directory path
12. Reply Tags: [[reply_to_current]] for native replies
13. Messaging: channel routing, sessions_send, message tool, NO_REPLY token
14. Response Style: Korean default, concise for Telegram (4096 char limit), emoji
15. Current Date & Time: local time with timezone
16. Context Files: workspace + ancestor directories
17. Silent Replies: NO_REPLY token for suppressing delivery
18. Runtime: agentId, host, OS, model, defaultModel, channel

## Context Files (context_files.go)
Load order: CLAUDE.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, MEMORY.md
- Scans workspace + ancestors (up to 10 levels, stops at home dir)
- Budget: max 20K chars per file, 150K chars total
- Mtime-based caching with 5-minute revalidation
- Truncation: head 70% + "[...truncated...]" + tail 20%

## Tool Display
- toolCategories: 10 groups (File, Code, Git, Exec, AI, Web, Memory, System, Sessions, Media, Data)
- Tool descriptions in JSON schemas, not duplicated in prompt text
- Only tool names listed → keeps static block small and cacheable

## Vibe Coder Variant (BuildCodingSystemPromptBlocks)
Alternate prompt for Telegram coding channel:
- Zero code exposure: never show raw code or diffs
- Korean first: all user-facing text in Korean
- Structured summary: 📝 변경 요약 → 🔨 빌드 → 🧪 테스트
- Auto-commit/push workflow via buttons

## Key Files
- gateway-go/internal/chat/prompt/system_prompt.go (assembly, all sections)
- gateway-go/internal/chat/context_files.go (file loader, budget enforcement, caching)
- gateway-go/internal/chat/run.go (SystemPromptParams construction)

## Common Tasks
- View system prompt assembly: read(file_path:'gateway-go/internal/chat/prompt/system_prompt.go')
- Check context file loading: grep(pattern:'CLAUDE.md\|SOUL.md\|MEMORY.md', path:'gateway-go/internal/chat/context_files.go')
- View skill injection: grep(pattern:'BuildSkillsPrompt\|available_skills', path:'gateway-go/internal/')

## Gotchas
- Context files have a 20K char/file and 150K total budget; exceeding triggers truncation (head 70% + tail 20%)
- Anthropic cache_control requires static block to be stable across turns; modifying it invalidates the cache
- Semi-static skills block is separate from static for finer caching granularity`
