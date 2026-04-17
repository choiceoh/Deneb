# Handlers

> 170 nodes · cohesion 0.02

## Key Concepts

- **DispatchFromConfig()** (20 connections) — `gateway-go/internal/pipeline/autoreply/dispatch_config.go`
- **commands.go** (15 connections) — `gateway-go/internal/pipeline/autoreply/handlers/commands.go`
- **HandleDirectives()** (13 connections) — `gateway-go/internal/pipeline/autoreply/directives/directive_handling.go`
- **abort_cutoff.go** (13 connections) — `gateway-go/internal/pipeline/autoreply/session/abort_cutoff.go`
- **commands_handlers.go** (11 connections) — `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers.go`
- **directive_handling.go** (10 connections) — `gateway-go/internal/pipeline/autoreply/directives/directive_handling.go`
- **status.go** (10 connections) — `gateway-go/internal/pipeline/autoreply/handlers/status.go`
- **handleModelCommand()** (9 connections) — `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers_model.go`
- **NewCommandRegistry()** (9 connections) — `gateway-go/internal/pipeline/autoreply/handlers/commands.go`
- **commands_handlers_model_test.go** (9 connections) — `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers_model_test.go`
- **session_full.go** (9 connections) — `gateway-go/internal/pipeline/autoreply/session/session_full.go`
- **handleVerboseCommand()** (8 connections) — `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers_model.go`
- **CommandRegistry** (8 connections) — `gateway-go/internal/pipeline/autoreply/handlers/commands.go`
- **NewInboundProcessor()** (8 connections) — `gateway-go/internal/runtime/server/inbound.go`
- **resolveModelDirective()** (7 connections) — `gateway-go/internal/pipeline/autoreply/directives/directive_handling.go`
- **commands_test.go** (7 connections) — `gateway-go/internal/pipeline/autoreply/handlers/commands_test.go`
- **model_selection.go** (7 connections) — `gateway-go/internal/pipeline/autoreply/model/model_selection.go`
- **CommandRouter** (7 connections) — `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers.go`
- **TestAbortCutoffLifecycle()** (6 connections) — `gateway-go/internal/pipeline/autoreply/session/abort_cutoff_test.go`
- **NewCommandRouter()** (6 connections) — `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers.go`
- **TestCommandRouter_DispatchModel()** (6 connections) — `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers_test.go`
- **TestCommandRouter_UnknownCommand()** (6 connections) — `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers_test.go`
- **TestDispatchFromConfig_CommandRouting()** (6 connections) — `gateway-go/internal/pipeline/autoreply/dispatch_config_test.go`
- **.NormalizeCommandBody()** (6 connections) — `gateway-go/internal/pipeline/autoreply/handlers/commands.go`
- **FormatProviderModelRef()** (6 connections) — `gateway-go/internal/pipeline/autoreply/model/model_selection.go`
- *... and 145 more nodes in this community*

## Relationships

- No strong cross-community connections detected

## Source Files

- `gateway-go/internal/pipeline/autoreply/directives/directive_handling.go`
- `gateway-go/internal/pipeline/autoreply/directives/directive_handling_test.go`
- `gateway-go/internal/pipeline/autoreply/dispatch_config.go`
- `gateway-go/internal/pipeline/autoreply/dispatch_config_test.go`
- `gateway-go/internal/pipeline/autoreply/handlers/commands.go`
- `gateway-go/internal/pipeline/autoreply/handlers/commands_data.go`
- `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers.go`
- `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers_model.go`
- `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers_model_test.go`
- `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers_session.go`
- `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers_session_test.go`
- `gateway-go/internal/pipeline/autoreply/handlers/commands_handlers_test.go`
- `gateway-go/internal/pipeline/autoreply/handlers/commands_test.go`
- `gateway-go/internal/pipeline/autoreply/handlers/status.go`
- `gateway-go/internal/pipeline/autoreply/model/model_selection.go`
- `gateway-go/internal/pipeline/autoreply/model/model_selection_full.go`
- `gateway-go/internal/pipeline/autoreply/session/abort.go`
- `gateway-go/internal/pipeline/autoreply/session/abort_cutoff.go`
- `gateway-go/internal/pipeline/autoreply/session/abort_cutoff_test.go`
- `gateway-go/internal/pipeline/autoreply/session/session_full.go`

## Audit Trail

- EXTRACTED: 367 (61%)
- INFERRED: 237 (39%)
- AMBIGUOUS: 0 (0%)

---

*Part of the graphify knowledge wiki. See [[index]] to navigate.*