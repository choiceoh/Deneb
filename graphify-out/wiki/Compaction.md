# Compaction

> 126 nodes · cohesion 0.04

## Key Concepts

- **NewTextMessage()** (65 connections) — `gateway-go/internal/ai/llm/types.go`
- **RunAgent()** (33 connections) — `gateway-go/internal/agentsys/agent/executor.go`
- **executor_integration_test.go** (25 connections) — `gateway-go/internal/agentsys/agent/executor_integration_test.go`
- **Compact()** (18 connections) — `gateway-go/internal/pipeline/compaction/polaris.go`
- **EstimateTokens()** (17 connections) — `gateway-go/internal/pipeline/compaction/polaris.go`
- **buildTextTurnEvents()** (15 connections) — `gateway-go/internal/agentsys/agent/executor_integration_test.go`
- **EmbeddingCompact()** (14 connections) — `gateway-go/internal/pipeline/compaction/embedding.go`
- **consumeStreamInto()** (14 connections) — `gateway-go/internal/agentsys/agent/executor.go`
- **buildToolUseTurnEventsWithNames()** (14 connections) — `gateway-go/internal/agentsys/agent/executor_integration_test.go`
- **EstimateMessagesTokens()** (13 connections) — `gateway-go/internal/pipeline/compaction/polaris.go`
- **NormalizeMessages()** (11 connections) — `gateway-go/internal/ai/llm/normalize.go`
- **mmrSelect()** (10 connections) — `gateway-go/internal/pipeline/compaction/embedding.go`
- **EmergencyCompact()** (10 connections) — `gateway-go/internal/pipeline/compaction/emergency.go`
- **TestRunAgent_StreamHooks_Called()** (10 connections) — `gateway-go/internal/agentsys/agent/executor_integration_test.go`
- **embedding_test.go** (10 connections) — `gateway-go/internal/pipeline/compaction/embedding_test.go`
- **RecencyCompact()** (10 connections) — `gateway-go/internal/pipeline/compaction/recency.go`
- **TestRunAgent_OnTurn_Callback()** (9 connections) — `gateway-go/internal/agentsys/agent/executor_integration_test.go`
- **TestRunAgent_SingleToolCall()** (9 connections) — `gateway-go/internal/agentsys/agent/executor_integration_test.go`
- **executor.go** (9 connections) — `gateway-go/internal/agentsys/agent/executor.go`
- **polaris.go** (9 connections) — `gateway-go/internal/pipeline/compaction/polaris.go`
- **polaris_test.go** (9 connections) — `gateway-go/internal/pipeline/compaction/polaris_test.go`
- **LLMCompact()** (9 connections) — `gateway-go/internal/pipeline/compaction/llm.go`
- **serializeMessages()** (9 connections) — `gateway-go/internal/pipeline/compaction/llm.go`
- **MicroCompact()** (9 connections) — `gateway-go/internal/pipeline/compaction/micro.go`
- **TestEmergencyCompact_EvictsOldestSummarizesNonEvicted()** (9 connections) — `gateway-go/internal/pipeline/compaction/polaris_test.go`
- *... and 101 more nodes in this community*

## Relationships

- No strong cross-community connections detected

## Source Files

- `gateway-go/internal/agentsys/agent/executor.go`
- `gateway-go/internal/agentsys/agent/executor_integration_test.go`
- `gateway-go/internal/agentsys/agent/executor_test.go`
- `gateway-go/internal/ai/llm/normalize.go`
- `gateway-go/internal/ai/llm/normalize_test.go`
- `gateway-go/internal/ai/llm/types.go`
- `gateway-go/internal/pipeline/compaction/bootstrap.go`
- `gateway-go/internal/pipeline/compaction/embedding.go`
- `gateway-go/internal/pipeline/compaction/embedding_test.go`
- `gateway-go/internal/pipeline/compaction/emergency.go`
- `gateway-go/internal/pipeline/compaction/llm.go`
- `gateway-go/internal/pipeline/compaction/micro.go`
- `gateway-go/internal/pipeline/compaction/polaris.go`
- `gateway-go/internal/pipeline/compaction/polaris_test.go`
- `gateway-go/internal/pipeline/compaction/recency.go`
- `gateway-go/internal/pipeline/compaction/restore.go`
- `gateway-go/internal/pipeline/polaris/assemble.go`
- `gateway-go/internal/pipeline/polaris/condense.go`
- `gateway-go/internal/pipeline/polaris/engine.go`

## Audit Trail

- EXTRACTED: 370 (47%)
- INFERRED: 417 (53%)
- AMBIGUOUS: 0 (0%)

---

*Part of the graphify knowledge wiki. See [[index]] to navigate.*