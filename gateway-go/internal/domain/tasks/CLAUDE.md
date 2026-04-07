# Background Task Control Plane (`internal/tasks/`)

Unified execution ledger tracking all background work across runtimes.

## Architecture

```
types.go        - Core types: TaskRecord, FlowRecord, TaskRuntime, TaskStatus
store.go        - SQLite persistence (tasks.db), CRUD for tasks/flows/events
registry.go     - In-memory registry with secondary indexes, flow operations
executor.go     - Task lifecycle transitions (create, start, complete, fail, cancel, block)
flow.go         - Flow lifecycle (create, link tasks, resume blocked)
audit.go        - Health checks: stale tasks, orphans, timestamp consistency
maintenance.go  - Periodic sweep: orphan recovery, cleanup, retention pruning
```

## Key Concepts

- **TaskRuntime**: `subagent`, `acp`, `cli`, `cron` — which subsystem owns the task
- **TaskStatus**: `queued → running → {succeeded|failed|timed_out|cancelled|lost|blocked}`
- **FlowRecord**: Groups related tasks; auto-transitions based on child task states
- **Orphan recovery**: `StartMaintenanceLoop()` marks active tasks as `lost` when their backing session disappears (5min grace period)
- **Retention**: Terminal tasks auto-pruned after 7 days

## RPC Methods

| Method | Description |
|--------|-------------|
| `task.status` | Aggregate statistics |
| `task.list` | List tasks (filter by runtime/status/owner/flow/active) |
| `task.get` | Get task by taskId or runId |
| `task.events` | Audit trail for a task |
| `task.cancel` | Cancel an active task |
| `task.audit` | Run health audit (stale, lost, delivery failures) |
| `flow.list` | List all flows |
| `flow.show` | Flow details + linked tasks |
| `flow.cancel` | Cancel flow + all active tasks |

## Adding a New Runtime Integration

1. Use `CreateQueuedTask()` or `CreateRunningTask()` with the appropriate `TaskRuntime`
2. Call `StartTask()` / `RecordProgress()` / `CompleteTask()` / `FailTask()` as the task progresses
3. Set `ChildSessionKey` so orphan recovery can detect lost backing sessions
4. Optionally create a `FlowRecord` and link tasks via `FlowID` for multi-step workflows
