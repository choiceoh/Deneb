# Tool Interception: Hermes vs Deneb Gap Analysis

**Status:** research / design note
**Audience:** future agents porting Hermes patterns into Deneb
**Decision:** Scenario C — no refactor required. Deneb's unified registry already covers the use case. Document and move on.

---

## 1. Hermes pattern (reference)

Hermes's `AIAgent._invoke_tool()` in `run_agent.py:7675-7752` routes tool calls through a fixed, ordered chain:

```
1. pre_tool_call plugin hook → may return block_message
2. todo → _todo_tool(self._todo_store)
3. session_search → _session_search(self._session_db, self.session_id)
4. memory → _memory_tool(self._memory_store)
   └─ on add/replace: notify self._memory_manager.on_memory_write(...)
5. self._memory_manager.has_tool(name) → self._memory_manager.handle_tool_call(...)
   (Honcho, Mem0, other external providers claim their own tool names here)
6. clarify → _clarify_tool(self.clarify_callback)
7. delegate_task → self._dispatch_delegate_task(function_args)
8. else → handle_function_call(name, args, ...)   [global registry]
```

Two kinds of interception are mixed in one method:

- **Agent-scoped tools** (todo, memory, clarify, session_search, delegate_task) — hard-coded branches that reach into `self._todo_store`, `self.session_id`, `self.clarify_callback`, etc. These need instance state, so they cannot live in a stateless global registry.
- **External memory provider** (`self._memory_manager`) — a pluggable object whose `has_tool(name)` claim dynamically intercepts arbitrary tool names (Honcho's `honcho_search`, Mem0's `mem0_recall`, etc.) before the registry sees them.

The plugin hook at step 1 is orthogonal — it only *blocks*, never *handles*.

## 2. Deneb dispatch flow (current)

Trace a tool call from model response to execution:

1. **Stream consumer** assembles `tool_use` content blocks as the LLM streams them
   (`gateway-go/internal/agentsys/agent/executor.go:454-509` — the `content_block_stop`
   case appends to `turnRes.toolCalls`).
2. **Post-stream dispatch** runs each tool call sequentially via `executeOneTool`
   (`executor.go:265-277`). That function:
   - fires `OnToolStart` / `OnToolEmit` hooks for streaming clients,
   - runs the `ToolLoopDetector`,
   - calls `OnBeforeToolCall(name, id, input)` — if it returns `block=true`, short-circuits with an error tool_result (`executor.go:577-591`). No one currently sets this hook.
   - invokes `tools.Execute(ctx, tc.Name, tc.Input)` — **the sole dispatch point**.
3. **`tools` is `*chat.ToolRegistry`** (`gateway-go/internal/pipeline/chat/tools.go:38`),
   built per-handler in the chat pipeline startup and handed to the agent via
   `cfg.Tools` in `buildAgentConfig` (`run_exec.go:627-639`).
4. **`ToolRegistry.Execute`** (`tools.go:84-179`) does a single map lookup, enforces
   preset filtering, handles `$ref` injection, runs the tool function, applies
   post-processing, then returns.

There is **no fork in this path**. Every tool name — fs, exec, grep, git, memory, polaris, gmail, sessions, sessions_spawn, subagents, cron, web, wiki, message, send_file, skills, fetch_tools, gateway, read_spillover, process — is a plain entry in the same map.

## 3. Deneb tool inventory — agent-scoped vs registry

The Hermes axis does not apply cleanly because **nothing in Deneb is agent-instance-scoped the way Hermes's `self._todo_store` is.** Deneb has no `AIAgent` class — the "agent" is a stateless `RunAgent` function plus a per-run `TurnContext`. All state that would be "agent-scoped" in Hermes is instead:

| Hermes concept | Deneb equivalent | Scoping mechanism |
|---|---|---|
| `self._todo_store` | (no equivalent) | — |
| `self._memory_store` | `memory` files on disk (workspace) + `polaris.Store` | Workspace path + DI |
| `self._session_db` | `polaris.Store`, `transcript.TranscriptStore` | DI via `CoreToolDeps` |
| `self.clarify_callback` | (no equivalent) | — |
| `self._memory_manager` (Honcho/Mem0 intercept) | (no equivalent) | — |
| `self._dispatch_delegate_task` | `sessions_spawn` tool | Registry + `toolctx.SessionDeps` |

**Deneb injects state through `CoreToolDeps`** at registration time (`toolreg/core.go`). The registry call `tools.ToolPolaris(store, localAI)` closes over `store`, so the executor inside the registry is still stateful, just closed-over rather than method-bound. This is the Go idiom for the same thing.

Meaningful categories today:

- **Stateless file I/O**: read, write, edit, grep (closed over `workspaceDir`)
- **Stateful session**: sessions, sessions_spawn, subagents, polaris (closed over stores)
- **External I/O**: exec, web, gmail, message, send_file
- **Runtime plugins (deferred activation via `fetch_tools`)**: gateway, process, skills, send_file, sessions, gmail, polaris, read_spillover
- **Meta**: fetch_tools (registers other tools on demand)

No tool name collides with a plugin-provided tool, and there is no external memory provider shipping its own tool surface. So the Hermes "memory_manager.has_tool()" claim mechanism has no caller in Deneb today.

## 4. Does Deneb have a pre-registry intercept hook?

Yes — `StreamHooks.OnBeforeToolCall` in `gateway-go/internal/agentsys/agent/hooks.go:14`. It is:

- Called for every tool call *before* `tools.Execute` (`executor.go:577`).
- Returns `(block bool, blockReason string)` — can only *block* (short-circuit with an error), not *handle* the call.
- Wired via `HookCompositor.SetBeforeToolCall`.
- **Currently unused.** No production code sets it — it sits as a future extension point for the plugin system described in the Hermes reference.

This is sufficient for the policy/guard use case (audit, deny-list, per-turn budgets) but does not support the "claim this tool name and execute it myself" pattern that `memory_manager.has_tool()` enables in Hermes.

## 5. Gap analysis

| Hermes capability | Deneb today | Gap? |
|---|---|---|
| Agent-scoped tools with instance state | Closures over DI-injected deps; equivalent outcome | **No gap.** |
| Plugin pre-tool-call block | `OnBeforeToolCall` hook (unused) | **No gap.** |
| External memory provider claiming tool names | No caller; no plugin surface exists today | **Latent gap — but no demand.** |
| Ordered chain with early exit | Single registry lookup; preset filtering is the only pre-execute gate | **No gap given current demand.** |
| Post-exec side effects tied to specific tools (`on_memory_write`) | Post-processors (`PostProcessRegistry`) run on name-match after execution | **No gap.** Deneb's post-processor model is cleaner. |

## 6. Decision: Scenario C

**Do not add a `ToolInterceptor` interface at this time.** Justification:

1. **Nothing to intercept.** Deneb has zero external memory providers, zero
   plugin-shipped tools that collide with core names, and zero agent-scoped tool
   branches in the critical path. Adding an interface with no implementers is
   speculative generality — exactly what the project philosophy rejects
   ("fewer moving parts, not more options").

2. **The two legitimate use cases are already covered.**
   - Pre-execute block / audit → `OnBeforeToolCall` (implemented, unused).
   - Post-execute side effects on specific tools → `PostProcessRegistry`
     (implemented, used for e.g. file-path injection).

3. **Agent-scoped state is solved differently and better.** Deneb uses
   closures over `CoreToolDeps` at registration time. This is type-safe, has
   no name-resolution order to reason about, and is the Go idiom. A runtime
   interceptor chain would re-introduce ordering bugs that the current model
   prevents by construction.

4. **If the future introduces an external memory provider** (Honcho-equivalent,
   Mem0 port, etc.), the right extension is *not* a generic interceptor chain
   but a specific adapter that registers its tools into the existing registry
   via `RegisterTool(toolctx.ToolDef{...})`. The external package already
   owns its name; no interception is needed — just registration with a closure
   over the provider's client.

## 7. Trade-off (future-facing note)

Deneb's model makes one assumption Hermes's does not: **tool names are
globally unique within a run**. If Deneb ever needs to let two providers
register conflicting names (e.g. core `memory` + plugin `memory`), the
registry's last-writer-wins behavior (`tools.go:66-74`) would silently
replace one without warning. Solutions when that day comes:

- Namespace plugin tools (`honcho:search`, `mem0:recall`).
- Have `RegisterTool` reject duplicates or warn via `slog`.
- Only then introduce an `Interceptor` if namespacing is deemed insufficient.

None of this is needed today.

## 8. Minimal follow-up

Two tiny hygiene improvements keep the extension point real rather than decorative:

1. Have `ToolRegistry.RegisterTool` emit a `slog.Warn` on silent replacement
   so that future plugin collisions are visible (not a behavior change —
   the replacement already happens silently).
2. Document in `gateway-go/CLAUDE.md` that `OnBeforeToolCall` is the
   supported plugin interception point, and that per-tool handlers should
   register via `RegisterTool` rather than a side-chain.

These are low-risk and can be done lazily when someone touches the file next.

---

**Files cited:**

- `gateway-go/internal/agentsys/agent/executor.go:265-277` (sequential tool dispatch)
- `gateway-go/internal/agentsys/agent/executor.go:533-647` (`executeOneTool`)
- `gateway-go/internal/agentsys/agent/hooks.go:12-15` (`OnBeforeToolCall`)
- `gateway-go/internal/pipeline/chat/tools.go:84-179` (`ToolRegistry.Execute`)
- `gateway-go/internal/pipeline/chat/toolreg/core.go` (all registration)
- `gateway-go/internal/pipeline/chat/toolreg_core.go` (chat-side registration + skills/wiki/fetch_tools)
- `/tmp/hermes-analysis/hermes-agent/run_agent.py:7675-7752` (Hermes reference)
