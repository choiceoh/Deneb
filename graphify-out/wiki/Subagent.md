# Subagent

> 65 nodes · cohesion 0.06

## Key Concepts

- **HandleSubagentsCommand()** (19 connections) — `gateway-go/internal/pipeline/autoreply/subagent/dispatch.go`
- **commands_shared.go** (17 connections) — `gateway-go/internal/pipeline/autoreply/subagent/commands_shared.go`
- **StopWithText()** (12 connections) — `gateway-go/internal/pipeline/autoreply/subagent/commands_shared.go`
- **actions_lifecycle.go** (12 connections) — `gateway-go/internal/pipeline/autoreply/subagent/actions_lifecycle.go`
- **HandleSubagentsInfoAction()** (11 connections) — `gateway-go/internal/pipeline/autoreply/subagent/actions_list.go`
- **FormatRunLabel()** (11 connections) — `gateway-go/internal/pipeline/autoreply/subagent/commands_shared.go`
- **BuildSubagentList()** (10 connections) — `gateway-go/internal/pipeline/autoreply/subagent/actions_list.go`
- **ResolveSubagentTarget()** (10 connections) — `gateway-go/internal/pipeline/autoreply/subagent/commands_shared.go`
- **HandleSubagentsAgentsAction()** (7 connections) — `gateway-go/internal/pipeline/autoreply/subagent/actions_focus.go`
- **HandleSubagentsLogAction()** (7 connections) — `gateway-go/internal/pipeline/autoreply/subagent/actions_lifecycle.go`
- **HandleSubagentsSendAction()** (7 connections) — `gateway-go/internal/pipeline/autoreply/subagent/actions_lifecycle.go`
- **HandleSubagentsListAction()** (7 connections) — `gateway-go/internal/pipeline/autoreply/subagent/actions_list.go`
- **actions_list.go** (7 connections) — `gateway-go/internal/pipeline/autoreply/subagent/actions_list.go`
- **HandleSubagentsKillAction()** (6 connections) — `gateway-go/internal/pipeline/autoreply/subagent/actions_list.go`
- **actions_focus.go** (6 connections) — `gateway-go/internal/pipeline/autoreply/subagent/actions_focus.go`
- **dispatch_test.go** (6 connections) — `gateway-go/internal/pipeline/autoreply/subagent/dispatch_test.go`
- **HandleSubagentsFocusAction()** (5 connections) — `gateway-go/internal/pipeline/autoreply/subagent/actions_focus.go`
- **HandleSubagentsSpawnAction()** (5 connections) — `gateway-go/internal/pipeline/autoreply/subagent/actions_lifecycle.go`
- **FormatRunStatus()** (5 connections) — `gateway-go/internal/pipeline/autoreply/subagent/commands_shared.go`
- **SortSubagentRuns()** (5 connections) — `gateway-go/internal/pipeline/autoreply/subagent/commands_shared.go`
- **BuildSubagentRunListEntries()** (5 connections) — `gateway-go/internal/pipeline/autoreply/subagent/run_utils.go`
- **FormatSubagentInfo()** (5 connections) — `gateway-go/internal/pipeline/autoreply/subagent/run_utils.go`
- **FormatTimestampWithAge()** (4 connections) — `gateway-go/internal/pipeline/autoreply/session/abort_cutoff.go`
- **HandleSubagentsUnfocusAction()** (4 connections) — `gateway-go/internal/pipeline/autoreply/subagent/actions_focus.go`
- **HandleSubagentsHelpAction()** (4 connections) — `gateway-go/internal/pipeline/autoreply/subagent/actions_list.go`
- *... and 40 more nodes in this community*

## Relationships

- No strong cross-community connections detected

## Source Files

- `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers_agents.go`
- `gateway-go/internal/pipeline/autoreply/session/abort_cutoff.go`
- `gateway-go/internal/pipeline/autoreply/subagent/actions_focus.go`
- `gateway-go/internal/pipeline/autoreply/subagent/actions_lifecycle.go`
- `gateway-go/internal/pipeline/autoreply/subagent/actions_list.go`
- `gateway-go/internal/pipeline/autoreply/subagent/commands_shared.go`
- `gateway-go/internal/pipeline/autoreply/subagent/dispatch.go`
- `gateway-go/internal/pipeline/autoreply/subagent/dispatch_test.go`
- `gateway-go/internal/pipeline/autoreply/subagent/run_utils.go`

## Audit Trail

- EXTRACTED: 125 (46%)
- INFERRED: 147 (54%)
- AMBIGUOUS: 0 (0%)

---

*Part of the graphify knowledge wiki. See [[index]] to navigate.*