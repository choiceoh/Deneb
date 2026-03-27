package chat

const compactionGuide = `Compaction summarizes older conversation history to stay within model context window limits.

## What It Does
- Summarizes older messages into a compact summary entry
- Keeps recent messages intact after the compaction point
- Summary persists in session JSONL history

## Auto-Compaction (default on)
Triggers when session nears/exceeds model context window. Deneb may retry the original request with compacted context.

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
- softThresholdTokens: 4000 (triggers flush at contextWindow - reserve - threshold)
- One flush per compaction cycle
- Skipped if workspace is read-only

## Rust Implementation (core-rs/core/src/compaction/)
- mod.rs: CompactionConfig, CompactionReason, CompactionDecision
  - Context threshold: 0.75, fresh tail count: 8
  - Target tokens: leaf=600, condensed=900
  - Max rounds: 10, max iterations per phase: 50
- sweep.rs (408 lines): hierarchical summarization state machine
  - Step-based I/O for cross-language orchestration
  - Chunk selection with fresh-tail protection
- timestamp.rs: timezone-aware timestamps in summaries

## Hierarchical Summarization (Aurora Sweep)
- Depth-0 (leaf): individual message summaries
- Depth-1 (condensed): groups of leaf summaries
- Higher depths: further condensation
- Fanout control: min 8 depth-0 summaries, 4 depth-1 before condensation
- Fresh tail protection: recent messages (default 8) excluded

## Compaction vs Pruning
- Compaction: summarizes and persists in JSONL (permanent)
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

const toolsGuide = `The tool system provides the AI agent with capabilities to interact with the filesystem, execute commands, search the web, and more.

## Core Architecture

### ToolDef (gateway-go/internal/chat/tools.go)
- Name: tool name (case-sensitive)
- Description: one-line for LLM
- InputSchema: JSON Schema for input validation
- Fn: ToolFunc = func(ctx, input json.RawMessage) (string, error)

### ToolRegistry
- Thread-safe map of name → ToolDef
- Preserves registration order
- Execute(): dispatches with $ref resolution + compression + post-processing
- LLMTools(): formats for LLM API
- RegisterTool(def): register a full definition

## Tool Execution Flow
1. Look up tool in registry by name
2. Check for "compress": true flag (optional output summarization via sglang)
3. Resolve $ref references (waits up to 30s for referenced tool output)
4. Execute tool function with context and input
5. Apply post-processors (global then tool-specific)

## Tool Chaining ($ref)
Tools can reference other tools' output in the same turn:
{"$ref": "tool_use_id"} → injected as "_ref_content" in input
Common patterns: grep→pilot, exec→pilot, read→pilot, find→read

## Parallel Execution
Independent tools execute in parallel goroutines within each turn.
TurnContext (tool_turn_context.go) enables cross-tool result sharing.

## Post-Processing Pipeline (tool_postprocess.go)
Global processors (all tools):
- OutputTrimmer: caps at 64K chars (head+tail preserved)
- ErrorEnricher: adds actionable hints to common errors

Tool-specific:
- grep: GrepResultSummarizer — caps at 200 lines
- find: FindResultSummarizer — caps at 500 entries, groups by directory
- exec: ExecAnnotator — emphasizes exit code on failure

## Tool Categories (33 tools)
Filesystem: read, write, edit, apply_patch, grep, find, ls
Execution: exec, process
Speed: pilot (local sglang orchestrator)
Web: web (search + fetch + search+fetch)
Memory: memory_search, memory_get, polaris
System: nodes, cron, message, gateway
Sessions: sessions_list/history/search/restore/send/spawn, subagents, session_status
Media: image, youtube_transcript, send_file
API: http, kv, clipboard, gmail

## Compression
Any tool call accepts "compress": true. Large outputs auto-summarized by local sglang.

## Context Passing
Tools access runtime context via context.Context:
- DeliveryContext: channel/recipient info
- ReplyFunc: callback for proactive sends
- SessionKey: current session ID
- TurnContext: other tools' results in current turn

## Key Files
- gateway-go/internal/chat/tools.go (types, registry)
- gateway-go/internal/chat/tools_core.go (registration)
- gateway-go/internal/chat/tools_fs.go (filesystem tools)
- gateway-go/internal/chat/tools_pilot.go (pilot orchestrator)
- gateway-go/internal/chat/tool_postprocess.go (post-processing)`

const systemPromptGuide = `The system prompt is the instruction set injected into every LLM call. It defines the agent's identity, available tools, and behavioral rules.

## Assembly (gateway-go/internal/chat/system_prompt.go)
Two build modes:
- BuildSystemPrompt(): single string (default)
- BuildSystemPromptBlocks(): Anthropic ContentBlocks with cache_control breakpoints
  - Static block: identity + tooling + safety (rarely changes, cached)
  - Dynamic block: skills + context files + runtime (changes per request)

## Prompt Sections (in order)
**Static block:**
1. Identity: "You are a personal assistant running inside Deneb"
2. Tooling: compact categorized tool name list (descriptions in tool schemas)
3. Tool Usage: parallel calls, first-class tools, compress flag, auto-trimming
4. Pilot & Chaining: when to use pilot, $ref chaining, when NOT to use pilot
5. Safety: no self-preservation, prioritize oversight
6. Deneb CLI Quick Reference

**Dynamic block:**
7. Skills: XML available_skills block (from skills/prompt.go)
8. Memory Recall: guidance on memory_search usage
9. Polaris: system manual tool guidance
10. Workspace: working directory
11. Reply Tags: [[reply_to_current]] for native replies
12. Messaging: channel routing, sessions_send, message tool
13. Response Style: Korean default, concise for Telegram (4096 char limit)
14. Current Date & Time: local time with timezone
15. Context Files: CLAUDE.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, MEMORY.md
16. Silent Replies: NO_REPLY token for suppressing delivery
17. Runtime: agentId, host, OS, model, channel

## Context Files (context_files.go)
Load order: CLAUDE.md, SOUL.md, TOOLS.md, IDENTITY.md, USER.md, MEMORY.md
- Scans workspace + ancestors (up to 10 levels, stops at home dir)
- Max 20K chars/file, 150K total
- Mtime-based caching with 5-minute revalidation
- Truncates: head 70% + [truncated] + tail 20%

## Prompt Modes
- full (default): all sections included
- minimal: sub-agents get only AGENTS.md + TOOLS.md
- none: no system prompt

## Tool Display
- toolCategories: categorized tool name groups (File, Exec, AI, Web, Memory, System, Sessions, Media, Data)
- Tool descriptions live in ToolDef.Description (sent via tool schemas, not duplicated in prompt)

## Key Files
- gateway-go/internal/chat/system_prompt.go
- gateway-go/internal/chat/context_files.go
- gateway-go/internal/chat/run.go (SystemPromptParams construction)`
