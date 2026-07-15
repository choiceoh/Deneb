# Dense embed index (corpus-agnostic)

Owns an in-memory dense-vector index with content-hash caching: embed a corpus
once, refresh changed items in the background, and answer cosine-similarity
queries so callers can blend semantic hits with lexical (BM25) indexes.

## Entry points

- `index.go` — `New`, `Index`, `Embedder`, `Item`, `Supplier`, `Hit`
- `Index.RefreshAsync`, `Index.Warm`, `Index.Search`, `Index.SearchBatch`,
  `Index.SearchVec`, `Index.Enabled`, `Index.Close`
- `identity.go` — `Identity`, `IdentityOf` (embedder identity for cache keys)

## Dependency direction and invariants

- **Dependency / boundary**: leaf domain package — imports only `pkg/*`. Must
  not import `ai/embedding` concretely; callers pass an `Embedder` interface
  (`Embed` + `IsHealthy`). Do not pull chat or wiki stores in here.
- **Invariant**: no embedder / unhealthy server / embed error → zero hits
  (silent degrade so lexical fallback works). Refresh is lazy and off the
  query deadline; only items whose content hash changed are re-embedded.
- On-disk cache is keyed by embedder identity + item hashes — never serve
  vectors from a mismatched embedder identity.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/domain/embedindex
```
