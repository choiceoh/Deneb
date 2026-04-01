# Claude Code Tools & Subagent System

## Tool Registry (40+ Tools)

### Core Execution Tools

| Tool | Description |
|------|-------------|
| BashTool / PowerShellTool | Shell execution with optional sandboxing |
| FileReadTool | File reading with line range support |
| FileEditTool | Surgical find-and-replace edits |
| FileWriteTool | Full file creation/overwrite |
| GlobTool | File pattern matching (native bfs optimization) |
| GrepTool | Content search (native ugrep optimization) |
| WebFetchTool | URL content fetching + AI summarization |
| WebSearchTool | Web search integration |
| BrowserTool | Playwright-based browser control |
| NotebookEditTool | Jupyter notebook editing |
| REPLTool | Interactive VM shell |
| LSPTool | Language Server Protocol: definitions, references, call hierarchy |

### Agent Coordination Tools

| Tool | Description |
|------|-------------|
| AgentTool | Spawn child agents / subagents |
| TaskCreate | Create background task |
| TaskGet / TaskList | Query task status |
| TaskUpdate | Update running task |
| TaskOutput | Get task output |
| TaskStop | Terminate task |
| TeamCreate / TeamDelete | Agent swarm management |
| SendMessage | Inter-agent messaging |
| ListPeersTool | Discover peer agents via UDS inbox |
| ScheduleCronTool | Schedule background cron work |

### Specialized Tools

| Tool | Description |
|------|-------------|
| SyntheticOutputTool | Dynamic JSON schema generation |
| EnterWorktree / ExitWorktreeTools | Git worktree isolation |
| BriefTool | Upload/summarize to claude.ai |
| CtxInspectTool | Context window inspection (gated: `CONTEXT_COLLAPSE`) |
| TerminalCaptureTool | Terminal panel capture (gated: `TERMINAL_PANEL`) |
| ConfigTool | Internal-only configuration |
| TungstenTool | Internal-only advanced features |

### Tool Design Principles

1. **Dedicated tools over shell equivalents**: Read over cat, Grep over grep, Glob over find
2. **Native optimization**: GrepTool uses ugrep, GlobTool uses bfs — faster than shell
3. **Better permission handling**: Dedicated tools handle permissions and result collection cleanly
4. **Overflow handling**: Tool results > size limit → written to disk, context gets preview + file reference

---

## Subagent System

### Subagent Types

| Type | Purpose | Prompt Tokens |
|------|---------|---------------|
| Explore | Fast codebase exploration, file search, keyword search | 494 |
| Plan | Software architect, implementation planning | 636 |
| Task (general) | Complex multi-step autonomous work | varies |

### Subagent Architecture

```
Parent Agent
├── Forked subagent A (Explore) — reuses parent cache
├── Forked subagent B (Plan) — reuses parent cache
└── Forked subagent C (Task) — independent execution
    └── Can spawn its own sub-subagents
```

Key properties:
- **Cache reuse**: Forked agents share parent's prompt cache
- **Mutable state awareness**: Each agent tracks its own mutable state
- **Context isolation**: `AsyncLocalStorage` for in-process context separation
- **Process-based teammates**: tmux/iTerm2 panes for parallel execution

### Delegation Rules

Explicit anti-patterns in prompt:
> "read actual findings, specify exactly what to do"

Prevents lazy delegation where parent spawns subagent without clear instructions.

### Task Notification System

Workers communicate via XML messages:
```xml
<task-notification>
  <task-id>abc123</task-id>
  <status>completed</status>
  <summary>Found 3 relevant files...</summary>
</task-notification>
```

Async notifications delivered to parent agent context during execution.

---

## Coordinator Mode

Enabled via `CLAUDE_CODE_COORDINATOR_MODE=1`.

### 4-Phase Pipeline

```
1. Research    → Parallel workers explore codebase
2. Synthesis   → Coordinator aggregates findings
3. Implementation → Workers implement in parallel
4. Verification  → Workers verify changes
```

### Coordinator Properties

- **Parallelism emphasis**: "launch independent workers concurrently whenever possible"
- **Shared scratchpad**: Cross-worker knowledge sharing directory (gated: `tengu_scratch`)
- **No lazy delegation**: Coordinator must specify exactly what each worker does
- **Task notifications**: XML-based async communication between workers

### Agent Swarm (Team)

Gated behind `tengu_amber_flint`:
- TeamCreate/TeamDelete for managing agent groups
- ListPeersTool for discovering peer agents via Unix Domain Socket inbox
- SendMessage for direct inter-agent communication

---

## Git Worktree Isolation

Tools: `EnterWorktreeTool`, `ExitWorktreeTool`

Subagents can work in isolated git worktrees:
1. Agent enters worktree (isolated copy of repo)
2. Makes changes without affecting main working directory
3. If changes made: worktree path + branch returned
4. If no changes: worktree auto-cleaned

Prevents subagent file operations from conflicting with parent or peer agents.
