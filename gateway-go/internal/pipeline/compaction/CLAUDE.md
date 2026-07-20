# Compaction pipeline change map

This package owns context-reduction algorithms and structural repair. It
receives messages and summarizer/embedder ports, returns a compacted result,
and never owns durable session state.

## Entry points

- `polaris.go`: `Config`, `NewConfig`, `Result`, and `Compact` select the
  reduction tier and enforce the context budget.
- `bootstrap.go` handles deterministic first compaction; `llm.go` performs
  summary compaction through `Summarizer`.
- `embedding.go` provides relevance/diversity selection with recency fallback.
- `protected.go` (protected-tool helpers) and `micro.go` shrink historical
  results without breaking tool pairs or protected payloads (fetch_tools
  schemas, skills read/list results); byte-identical older duplicates of a
  protected call are deduped to the newest copy.
- `restore.go` removes thinking blocks and recovers recent file-read context.
- `context_fence.go`: `FormatContextFence` marks untrusted recovered context.

## Dependency direction and invariants

- Compaction may consume `llm.Message` and neutral `chatport` contracts, but
  must not import the chat handler, Polaris store implementation, or runtime.
- The returned history must keep tool-use/result pairs balanced and preserve
  protected schema/spillover references.
- Token budgets use the configured estimator and trim oldest eligible content
  first; CJK bounds are rune-safe rather than byte slicing.
- LLM failure, expired context, or unusable summary must retain a deterministic
  raw fallback instead of dropping conversation content.
- Context fences remain balanced through every trim and re-compaction path.

## Focused verification

Use `polaris_test.go`, `llm_chunk_test.go`,
`truncate_old_tool_results_test.go`, and `restore_test.go` for their matching
stages.

`cd gateway-go && go test -count=1 ./internal/pipeline/compaction`
