# Toolreg

> 100 nodes · cohesion 0.04

## Key Concepts

- **tool_schemas_gen.go** (23 connections) — `gateway-go/internal/pipeline/chat/toolreg/tool_schemas_gen.go`
- **ToolRegistry** (21 connections) — `gateway-go/internal/pipeline/chat/tool_suggest.go`
- **RegisterFSTools()** (20 connections) — `gateway-go/internal/pipeline/chat/toolreg/core.go`
- **.RegisterTool()** (15 connections) — `gateway-go/internal/pipeline/chat/toolreg/core_test.go`
- **core.go** (13 connections) — `gateway-go/internal/pipeline/chat/toolreg/core.go`
- **RegisterCoreTools()** (12 connections) — `gateway-go/internal/pipeline/chat/toolreg/core.go`
- **RegisterCoreTools()** (12 connections) — `gateway-go/internal/pipeline/chat/toolreg_core.go`
- **ToolFetchTools()** (11 connections) — `gateway-go/internal/pipeline/chat/tools/fetch_tools.go`
- **.PreSerialize()** (10 connections) — `gateway-go/internal/ai/llm/types.go`
- **RegisterSessionTools()** (9 connections) — `gateway-go/internal/pipeline/chat/toolreg/core.go`
- **tool_bench_test.go** (9 connections) — `gateway-go/internal/ai/llm/tool_bench_test.go`
- **.suggestToolNames()** (8 connections) — `gateway-go/internal/pipeline/chat/tool_suggest.go`
- **RegisterProcessTools()** (8 connections) — `gateway-go/internal/pipeline/chat/toolreg/core.go`
- **tool_suggest_test.go** (8 connections) — `gateway-go/internal/pipeline/chat/tool_suggest_test.go`
- **.unknownToolError()** (7 connections) — `gateway-go/internal/pipeline/chat/tool_suggest.go`
- **RegisterRoutineTools()** (7 connections) — `gateway-go/internal/pipeline/chat/toolreg/core.go`
- **RegisterWebTools()** (7 connections) — `gateway-go/internal/pipeline/chat/toolreg/core.go`
- **newTestRegistryWithNames()** (7 connections) — `gateway-go/internal/pipeline/chat/tool_suggest_test.go`
- **.FilteredLLMTools()** (6 connections) — `gateway-go/internal/pipeline/chat/tools.go`
- **sampleSchema()** (6 connections) — `gateway-go/internal/ai/llm/tool_bench_test.go`
- **dynamicMaxDistance()** (6 connections) — `gateway-go/internal/pipeline/chat/tool_suggest.go`
- **extractCompressFlag()** (6 connections) — `gateway-go/internal/pipeline/chat/tools.go`
- **.buildLLMToolsLocked()** (5 connections) — `gateway-go/internal/pipeline/chat/tools.go`
- **.DeferredLLMTools()** (5 connections) — `gateway-go/internal/pipeline/chat/tools.go`
- **RegisterChronoTools()** (5 connections) — `gateway-go/internal/pipeline/chat/toolreg/core.go`
- *... and 75 more nodes in this community*

## Relationships

- No strong cross-community connections detected

## Source Files

- `gateway-go/internal/ai/llm/tool_bench_test.go`
- `gateway-go/internal/ai/llm/types.go`
- `gateway-go/internal/pipeline/chat/tool_suggest.go`
- `gateway-go/internal/pipeline/chat/tool_suggest_test.go`
- `gateway-go/internal/pipeline/chat/toolctx/context.go`
- `gateway-go/internal/pipeline/chat/toolreg/core.go`
- `gateway-go/internal/pipeline/chat/toolreg/core_test.go`
- `gateway-go/internal/pipeline/chat/toolreg/tool_schemas_gen.go`
- `gateway-go/internal/pipeline/chat/toolreg_core.go`
- `gateway-go/internal/pipeline/chat/tools.go`
- `gateway-go/internal/pipeline/chat/tools/fetch_tools.go`
- `gateway-go/internal/pipeline/chat/tools/spillover_read.go`
- `gateway-go/internal/pipeline/chat/tools_bench_test.go`
- `gateway-go/internal/pipeline/chat/web/merged.go`

## Audit Trail

- EXTRACTED: 241 (52%)
- INFERRED: 224 (48%)
- AMBIGUOUS: 0 (0%)

---

*Part of the graphify knowledge wiki. See [[index]] to navigate.*