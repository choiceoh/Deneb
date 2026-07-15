# Schedule tools (calendar / cron / todo)

Owns the agent-facing schedule tools: merged calendar read/write, availability
and work-graph queries, persistent cron jobs, and the structured todo list.
Registration stays in `toolwire`/`toolreg`; this package never imports the
parent `tools` package.

## Entry points

- `calendar.go` — `ToolCalendar` (LLM tool loop for calendar CRUD + queries)
- `calendar_glance.go` — `NewCalendarGlanceFunc`, `CalendarGlanceFunc`
- `calendar_format.go` — `CalendarGlance` (ambient system-prompt glance)
- `api.go` — `ResolveReadWindow`, `MergeEvents` (Google + local merge helpers)
- `cron_tool.go` — `ToolCron`
- `todo.go` — `ToolTodo`

## Dependency direction and invariants

- **Dependency / boundary**: depends on `tooldeps`/`toolport` ports plus
  `platform/calendar`, `platform/cron`, `platform/localcal`, `platform/localtodo`.
  Must not import `pipeline/chat` handlers or parent `tools`.
- **Invariant**: calendar reads always merge Google + local through
  `MergeEvents`; glance is frozen per calendar day (single-user shared slot).
  Cron/todo mutations go through their platform stores — do not invent a second
  persistence path in this package.
- Never register tools here; callers compose `ToolCalendar` / `ToolCron` /
  `ToolTodo` into the registry.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/schedule
```
