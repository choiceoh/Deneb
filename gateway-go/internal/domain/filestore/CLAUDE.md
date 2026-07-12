# Filestore domain change map

This package owns Deneb's virtual file namespace, its local-disk adapter, and
the semantic index used by file recall. Chat tools, mail archiving, and mini-app
RPC consume the `Store` contract without interpreting host filesystem paths.

## Entry points and ownership

- `filestore.go`: `Store`, `Entry`, `FormatEntries`, and `HumanSize` define the
  backend-neutral contract and user-facing file metadata.
- `local.go`: `LocalStore` and `NewLocalStore` own virtual-to-host path
  resolution plus list, search, read, write, move, and delete behavior.
- `semindex.go`: `SemanticIndex`, `NewSemanticIndex`, `Reindex`, and `Search`
  own persisted chunks, content hashes, incremental refresh, and vector lookup.
- `semhybrid.go`: `SemanticIndex.HybridSearch` combines lexical and semantic
  evidence; `lexscore.go` contains its deterministic lexical score.

## Dependency direction and invariants

- This is a domain package and must never import `runtime`, RPC handlers, or
  `pipeline/chat`. Callers supply the narrow `Embedder` and `ExtractFunc` ports.
- Every virtual path must remain `/`-rooted and confined below `LocalStore.Root`.
  `Put`, `Open`, `Move`, and `Delete` must all reject escape attempts, including
  escapes introduced through a symlink or destination parent.
- Auto-rename and overwrite are distinct contracts. A new write or move must
  not silently replace an existing file unless overwrite was explicitly chosen.
- `SemanticIndex.Reindex` must remove stale chunks and persist content hashes in
  the same successful refresh. A failed embed may degrade to lexical behavior,
  but must not corrupt the last readable index.
- Hybrid search keeps exact-name matches useful below the semantic score floor
  and rejects common-token noise; do not tune one branch without both cases.

## Focused verification

Use `local_test.go` for `LocalStore` path and mutation behavior,
`semindex_test.go` for `SemanticIndex` persistence and incremental refresh, and
`semhybrid_test.go` for `HybridSearch` ranking and degradation.

`cd gateway-go && go test -count=1 ./internal/domain/filestore`

`semhybrid_live_test.go` requires an external embedder and is not part of the
deterministic package command; keep offline fakes as the default regression
proof.
