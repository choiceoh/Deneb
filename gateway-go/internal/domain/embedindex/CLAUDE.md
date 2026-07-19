# Dense embed index (corpus-agnostic)

Owns an in-memory dense-vector index with content-hash caching: embed a corpus
once, refresh changed items in the background, and answer cosine-similarity
queries so callers can blend semantic hits with lexical (BM25) indexes.

## Entry points

- `index.go` — `New`, `Index`, `Embedder`, `Item`, `Supplier`, `Hit`
- `Index.RefreshAsync`, `Index.RefreshIfStale`, `Index.Warm`, `Index.Search`,
  `Index.SearchBatch`, `Index.SearchVec`, `Index.Status`, `Index.Close`
- `identity.go` — `Identity`, `IdentityOf` (embedder identity for cache keys)
- `calibration.go` — model-specific semantic admission profiles and explicit
  operator overrides for each retrieval surface

## Dependency direction and invariants

- **Dependency / boundary**: leaf domain package — imports only `pkg/*`. Must
  not import `ai/embedding` concretely; callers pass an `Embedder` interface
  (`Embed` + `IsHealthy`). Do not pull chat or wiki stores in here.
- **Invariant**: no embedder / unhealthy server / embed error → zero hits
  (silent degrade so lexical fallback works). Refresh is lazy and off the
  query deadline; only items whose content hash changed are re-embedded.
- Concurrent refresh requests coalesce into one worker plus one latest-state
  follow-up scan. Never hold the index mutex while calling an embedder or
  calculating the full cosine scan.
- On-disk cache is keyed by embedder identity + item hashes — never serve
  vectors from a mismatched embedder identity.
- A real unknown embedder fingerprint must not admit semantic-only results
  until calibrated; dense rank may still reinforce an existing lexical hit.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/domain/embedindex
```
