---
summary: "Context engine: Aurora context assembly, compaction, and subagent lifecycle"
read_when:
  - You want to understand how Deneb assembles model context
  - You are working on context assembly or compaction behavior
title: "Context Engine"
---

# Context Engine

A **context engine** controls how Deneb builds model context for each run.
It decides which messages to include, how to summarize older history, and how
to manage context across subagent boundaries.

Deneb uses the **Aurora** context engine, implemented natively in Rust
(`core-rs/core/src/context_engine/`) and called from Go via CGo FFI.

## How it works

Every time Deneb runs a model prompt, the context engine participates at
four lifecycle points:

1. **Ingest** — called when a new message is added to the session. The engine
   can store or index the message in its own data store.
2. **Assemble** — called before each model run. The engine returns an ordered
   set of messages (and an optional `systemPromptAddition`) that fit within
   the token budget.
3. **Compact** — called when the context window is full, or when the user runs
   `/compact`. The engine summarizes older history to free space.
4. **After turn** — called after a run completes. The engine can persist state,
   trigger background compaction, or update indexes.

### Subagent lifecycle (optional)

Deneb currently calls one subagent lifecycle hook:

- **onSubagentEnded** — clean up when a subagent session completes or is swept.

The `prepareSubagentSpawn` hook is part of the interface for future use, but
the runtime does not invoke it yet.

### System prompt addition

The `assemble` method can return a `systemPromptAddition` string. Deneb
prepends this to the system prompt for the run. This lets engines inject
dynamic recall guidance, retrieval instructions, or context-aware hints
without requiring static workspace files.

## Aurora engine

The Aurora context engine is the built-in engine:

- **Ingest**: the session manager handles message persistence directly.
- **Assemble**: the Aurora store + Rust FFI performs DAG-aware context assembly
  with token budgeting (`core-rs/core/src/context_engine/`).
- **Compact**: delegates to the built-in summarization compaction via Rust FFI,
  which creates a summary of older messages and keeps recent messages intact.
- **After turn**: no-op.

## Relationship to compaction and memory

- **Compaction** is one responsibility of the context engine. The legacy engine
  delegates to Deneb's built-in summarization. Plugin engines can implement
  any compaction strategy (DAG summaries, vector retrieval, etc.).
- **Memory plugins** (`plugins.slots.memory`) are separate from context engines.
  Memory plugins provide search/retrieval; context engines control what the
  model sees. They can work together — a context engine might use memory
  plugin data during assembly.
- **Session pruning** (trimming old tool results in-memory) still runs
  regardless of which context engine is active.

## Tips

- Use `deneb doctor` to verify your engine is loading correctly.
- If switching engines, existing sessions continue with their current history.
  The new engine takes over for future runs.
- Engine errors are logged and surfaced in diagnostics. If a plugin engine
  fails to register or the selected engine id cannot be resolved, Deneb
  does not fall back automatically; runs fail until you fix the plugin or
  switch `plugins.slots.contextEngine` back to `"legacy"`.
- For development, use `deneb plugins install -l ./my-engine` to link a
  local plugin directory without copying.

See also: [Compaction](/concepts/compaction), [Context](/concepts/context),
[Plugins](/tools/plugin), [Plugin manifest](/plugins/manifest).
