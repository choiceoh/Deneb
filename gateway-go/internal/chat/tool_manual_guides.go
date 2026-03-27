package chat

// Guide content constants for the polaris tool's "guides" action.
// Each guide is a curated summary of a Deneb subsystem, written for AI agents.

const auroraGuide = `Aurora is the context engine that controls how Deneb assembles model context.

## Lifecycle
Every model run triggers four lifecycle points:
1. **Ingest** — store/index new messages
2. **Assemble** — build ordered message set within token budget, return optional systemPromptAddition
3. **Compact** — summarize older history when context is full (or /compact)
4. **After turn** — persist state, trigger background work

## Engine Selection
- Default: "legacy" engine (pass-through assembly, built-in summarization)
- Plugin engines: selected via plugins.slots.contextEngine in deneb.json
- Only one engine active per run

## ownsCompaction
- true: engine owns all compaction (Deneb disables built-in auto-compaction)
- false/unset: Deneb's built-in auto-compaction may still run; engine's compact() handles /compact and overflow

## Two Plugin Patterns
- **Owning mode**: implement custom compaction, set ownsCompaction: true
- **Delegating mode**: set ownsCompaction: false, call delegateCompactionToRuntime() in compact()

## AssembleResult
- messages: ordered messages for the model (required)
- estimatedTokens: engine's token estimate for threshold decisions (required — drives compaction decisions)
- systemPromptAddition: prepended to system prompt (optional)

## Aurora Tools (agent-callable)
- **aurora_grep**: search messages and summaries by keyword. Returns matching message IDs + snippets.
- **aurora_describe**: inspect a message's lineage — parents, children, summaries, depth.
- **aurora_expand_query**: deep recall (~120s). Expands a natural-language query into context-relevant messages. Expensive — use only when normal search is insufficient.

## Token Budget Constants
- Context threshold: 0.75 (compact when usage exceeds 75% of context window)
- Fresh tail: 32 messages (always kept intact, never compacted)
- Three-tier resolution order: env var > plugin config > hardcoded defaults

## Rust Implementation (core-rs/core/src/context_engine/)
- assembler.rs: DAG-aware token budgeting state machine
- retrieval.rs: message retrieval with lineage tracking
- mod.rs: handle-based FFI pattern — aurora_new() → handle → aurora_start(handle) → aurora_step(handle, response) → aurora_drop(handle)

## Go Integration
- gateway-go/internal/chat/compaction.go: context overflow handling via Aurora sweep
- gateway-go/internal/server/server.go: Aurora store initialization
- gateway-go/internal/aurora/: Aurora desktop RPC channel handlers

## Key Files
- docs/concepts/context-engine.md
- core-rs/core/src/context_engine/mod.rs, assembler.rs, retrieval.rs
- gateway-go/internal/chat/compaction.go
- gateway-go/internal/aurora/`

const vegaGuide = `Vega is Deneb's project search engine providing BM25 + semantic hybrid search over indexed content.

## Search Modes
- **bm25**: SQLite FTS5 full-text search (exact token matching)
- **semantic**: embedding-based vector similarity (meaning-based)
- **hybrid**: weighted fusion of BM25 + semantic scores (best of both)

## Query Routing (query_analyzer.rs)
Natural language queries are analyzed and routed to the best search mode:
- Short exact terms → BM25
- Conceptual/semantic questions → semantic
- Mixed or complex queries → hybrid fusion
- Fusion: BM25 + semantic score weighted merge, MMR re-ranking for diversity

## Architecture
Rust workspace crate (core-rs/vega/) with Go bindings (gateway-go/internal/vega/).

### Rust Side (core-rs/vega/)
- search/fts_search.rs: SQLite FTS5 query builder
- search/semantic.rs: embedding-based vector search
- search/fusion.rs: score fusion and reranking (BM25 + semantic)
- search/query_analyzer.rs: natural language query routing
- db/: schema, importer, parser, classifier (mail/project categorization)
- commands/: 20+ handlers (health, changelog, dashboard, brief, weekly, urgent, contacts, search, import)
- ai.rs: LLM-based command expansion

### Go Side (gateway-go/internal/vega/)
- types.go: Backend interface, SearchOpts, SearchResult
- autodetect.go: probe default ports (localhost:30001/v1, 30002/v1), env var overrides
- enhanced_backend.go: full Vega with embedding support
- rust_backend.go: FFI wrapper to Rust crate
- embed_server.go: SGLang embedder server lifecycle
- sglang_embedder.go: SGLang embedding endpoint integration
- llm_expander.go: LLM query expansion

## Embedding Backends
- SGLang server (default on DGX Spark): auto-detected at localhost:30001/v1, 30002/v1
- Local deneb-ml (GGUF models via llama-cpp-2)
- No-op fallback (BM25 only, used when no embedding backend available)

## Environment Variables
- VEGA_MODEL_EMBEDDER: path to embedding GGUF model
- VEGA_MODEL_RERANKER: path to reranker GGUF model
- VEGA_MODEL_EXPANDER: path to query expansion GGUF model

## Model Auto-detection
~/.deneb/models/*.gguf scanned at startup (autodetect.go). Filenames pattern-matched to role (embedder/reranker/expander).

## Build Variants
- make rust: minimal (no Vega)
- make rust-vega: FTS-only (no ML/CUDA)
- make rust-dgx: full production (Vega + semantic + CUDA)

## Key Files
- core-rs/vega/src/search/
- gateway-go/internal/vega/
- docs/concepts/architecture.md (Vega section)`

const agentLoopGuide = `The agent loop is the core execution cycle: intake → context assembly → model inference → tool execution → streaming → persistence.

## Entry Points
- Gateway RPC: agent and agent.wait methods
- CLI: agent command (WebSocket to gateway)

## Execution Flow
1. agent RPC validates params, resolves session, returns {runId, acceptedAt}
2. Resolve model + thinking/verbose defaults, load skills snapshot
3. Serialize via per-session + global queues (prevents races)
4. Persist user message to transcript + Aurora store
5. Spawn proactive context (parallel, min 50 chars trigger)
6. Build system prompt (deferred format for Anthropic cache_control)
7. Run knowledge prefetch + context assembly in parallel
8. Resolve model & LLM client from provider config
9. LLM call → parse tool_use blocks → execute tools in parallel → feed results back
10. Repeat until end_turn or limits hit
11. Emit lifecycle end/error event, persist result

## AgentConfig Defaults
- MaxTurns: 25
- Timeout: 10 minutes (wall-time)
- MaxTokens: 8192 (max output tokens per LLM call)
- defaultModel: "zai/glm-5-turbo"
- maxCompactionRetries: 2 (retry with compacted context on overflow)

## Go Implementation (gateway-go/internal/chat/)
- agent.go: AgentConfig, RunAgent(), consumeStream(), StreamHooks (OnTextDelta, OnThinking, OnToolStart)
- run.go: RunParams, runDeps (sessions, llmClient, transcript, tools, aurora, vega, memory, etc.)

## Queueing
- Runs serialized per session key (session lane) + optional global lane
- Prevents tool/session races and keeps history consistent
- Channel queue modes: collect, steer, followup

## Status Emojis (Telegram reactions)
Queued: 👀, Thinking: 🤔, Tool/Coding: 🔥, Web: ⚡, Done: 👍, Error: 😱, StallSoft: 🥱, StallHard: 😨, Compacting: 🤔

## Typing Signaler
- Interval: 5000ms (matches Telegram's 5s typing action TTL)
- Mode: TypingModeInstant (sends immediately on run start)

## Event Streams
Three streams emitted during a run:
- lifecycle: phase start/end/error
- assistant: text deltas from model
- tool: tool start/update/end events

## Hook Points
### Internal Hooks (Gateway)
- agent:bootstrap: modify bootstrap files before system prompt

### Plugin Hooks
- before_model_resolve: override provider/model
- before_prompt_build: inject prependContext, systemPrompt additions
- before_tool_call / after_tool_call: intercept tool params/results
- agent_end: inspect final message list
- before_compaction / after_compaction: observe compaction
- message_received / message_sending / message_sent

## Streaming
- Assistant deltas streamed as events
- Block streaming: partial replies on text_end or message_end
- NO_REPLY (__SILENT_REPLY__) token filtered from outgoing payloads

## Tool Execution
- Tools execute in parallel goroutines within each turn (WaitGroup)
- TurnContext enables cross-tool result sharing via $ref
- 30s timeout for $ref resolution (refWaitTimeout)
- Post-processors: OutputTrimmer (64K), ErrorEnricher, GrepSummarizer (200 lines), FindSummarizer (500 entries)

## Key Files
- docs/concepts/agent-loop.md
- gateway-go/internal/chat/agent.go, run.go
- gateway-go/internal/chat/tools.go (ToolRegistry, Execute)`
