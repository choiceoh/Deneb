# Fetchops (fetch_tools)

Owns deferred-tool activation: exact names, BM25 keyword search, optional
dense + rerank. Parent `runtimeops` no longer implements this surface.

## Entry points

- `fetch_tools.go` — `FetchToolsRegistry`, `ToolFetchTools`, `ToolFetchToolsWithReranker`
- `bm25.go` — lexical ranking over name + description + param names
- `fetch_tools_semantic.go` / `fetch_tools_rerank.go` — fail-open dense/rerank

Wiring stays in `toolwire/bridge.RegisterRegistryBridgeTools`.

## Dependency direction and invariants

- May import `toolport`, `toolpreset`, `embedindex`, `toolmeta`, `jsonutil`.
  Must not import parent `runtimeops` or `pipeline/chat`.
- Fail-open: embedder/reranker errors fall back to lexical/substring search.
- One query may activate at most `searchResultLimit` tools.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/fetchops
```
