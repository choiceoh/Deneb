# Prompt

> 66 nodes · cohesion 0.05

## Key Concepts

- **Fetch()** (14 connections) — `gateway-go/internal/platform/media/fetch.go`
- **system_prompt.go** (14 connections) — `gateway-go/internal/pipeline/chat/prompt/system_prompt.go`
- **fetch.go** (14 connections) — `gateway-go/internal/platform/media/fetch.go`
- **PromptCache** (14 connections) — `gateway-go/internal/pipeline/chat/prompt/prompt_cache.go`
- **buildPromptSections()** (13 connections) — `gateway-go/internal/pipeline/chat/prompt/system_prompt.go`
- **LoadContextFiles()** (12 connections) — `gateway-go/internal/pipeline/chat/prompt/context_files.go`
- **context_files.go** (11 connections) — `gateway-go/internal/pipeline/chat/prompt/context_files.go`
- **loadContextFilesFromDisk()** (7 connections) — `gateway-go/internal/pipeline/chat/prompt/context_files.go`
- **TestSessionSnapshotFrozen()** (7 connections) — `gateway-go/internal/pipeline/chat/prompt/context_files_test.go`
- **DetectMIME()** (6 connections) — `gateway-go/internal/platform/media/fetch.go`
- **validateURL()** (6 connections) — `gateway-go/internal/platform/media/fetch.go`
- **.Hostname()** (6 connections) — `gateway-go/internal/pipeline/chat/prompt/prompt_cache.go`
- **IdentityMethods()** (6 connections) — `gateway-go/internal/runtime/rpc/handler/system/system_identity.go`
- **ClearSessionSnapshot()** (5 connections) — `gateway-go/internal/pipeline/chat/prompt/context_files.go`
- **FormatContextFilesForPrompt()** (5 connections) — `gateway-go/internal/pipeline/chat/prompt/context_files.go`
- **TestLoadContextFiles()** (5 connections) — `gateway-go/internal/pipeline/chat/prompt/context_files_test.go`
- **isPrivateIP()** (5 connections) — `gateway-go/internal/platform/media/fetch.go`
- **SSRFSafeDialer()** (5 connections) — `gateway-go/internal/platform/media/fetch.go`
- **BuildSystemPromptBlocks()** (5 connections) — `gateway-go/internal/pipeline/chat/prompt/system_prompt.go`
- **writeCompactToolList()** (5 connections) — `gateway-go/internal/pipeline/chat/prompt/system_prompt.go`
- **collectSearchDirs()** (4 connections) — `gateway-go/internal/pipeline/chat/prompt/context_files.go`
- **ResetContextFileCacheForTest()** (4 connections) — `gateway-go/internal/pipeline/chat/prompt/context_files.go`
- **TestFormatContextFilesForPrompt()** (4 connections) — `gateway-go/internal/pipeline/chat/prompt/context_files_test.go`
- **WithSessionSnapshot()** (4 connections) — `gateway-go/internal/pipeline/chat/prompt/context_files.go`
- **parseContentDispositionFileName()** (4 connections) — `gateway-go/internal/platform/media/fetch.go`
- *... and 41 more nodes in this community*

## Relationships

- No strong cross-community connections detected

## Source Files

- `gateway-go/internal/pipeline/chat/prompt/context_files.go`
- `gateway-go/internal/pipeline/chat/prompt/context_files_test.go`
- `gateway-go/internal/pipeline/chat/prompt/prompt_cache.go`
- `gateway-go/internal/pipeline/chat/prompt/system_prompt.go`
- `gateway-go/internal/platform/media/fetch.go`
- `gateway-go/internal/platform/media/fetch_test.go`
- `gateway-go/internal/runtime/rpc/handler/system/system_identity.go`

## Audit Trail

- EXTRACTED: 157 (57%)
- INFERRED: 117 (43%)
- AMBIGUOUS: 0 (0%)

---

*Part of the graphify knowledge wiki. See [[index]] to navigate.*