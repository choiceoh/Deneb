# Knowledge router / adapters map

Owns the narrow knowledge `Adapter`/`Writer` ports and wiki/files adapters
that chat tools use instead of importing `domain/wiki` directly.

## Entry points

- `types.go` — `Adapter`, `Writer`, `Result`, `Document`, `RecordOptions`
- `router.go` — `Router`
- `adapter_wiki.go` — `NewWikiAdapter`
- `adapter_files.go` — `NewFilesAdapter`, `FilesAdapterDeps`
- `ref.go` — `ParseRef`, `Ref`, `Layer`

## Dependency direction and invariants

- **Dependency / port boundary**: chat/tools must depend on `Adapter` here,
  not `*wiki.Store`. Wiki adapter is the only place that may import wiki
  Store for knowledge recall/write.
- **Invariant**: `ParseRef` is the single source for knowledge refs; adapters
  must degrade cleanly on nil stores; never leak raw wiki paths into tool
  results without going through `Document` shaping.
- Keep fan-in on wiki low by routing new consumers through this package.

## Local change scope

Ports and adapters stay in `domain/knowledge`.

- May co-change: wiki Store helpers the adapter calls, and chat knowledge
  tool wrappers.
- Do not touch: genesis or RPC method registry.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/domain/knowledge
```
