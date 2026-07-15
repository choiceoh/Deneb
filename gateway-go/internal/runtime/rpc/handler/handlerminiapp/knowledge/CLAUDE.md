# Miniapp knowledge RPC (memory / notebook / people / search)

Owns authenticated miniapp RPC surfaces for memory pages, notebooks, people,
unified search, and topic documents. Wire DTOs for shared mail/memory helpers
may be consumed by sibling handler packages.

## Entry points

- `memory.go` — `MemoryMethods`, `MemoryDeps`, `MemorySearcher`
- `memory_browse.go` / `memory_write.go` — browse + write handlers
- `notebook.go` — `NotebookMethods`, `NotebookDeps`
- `people.go` — `PeopleMethods`, `PeopleDeps`, `PeopleClient`
- `search.go` — `SearchMethods`, `SearchDeps`, `SearchAllResult`
- `topicdocs.go` — `TopicDocsMethods`, `TopicDocsDeps`
- `sender.go` — `ParseSender` (shared mail/people sender parse)

## Dependency direction and invariants

- **Dependency / boundary**: depends on domain ports (`contacts`, `notebook`,
  `wikiport`), `platform/gmail` for people mail joins, and `minibind`/`rpcutil`
  for auth binding. Must not import `runtime/server` or chat tool packages.
  Handler Deps are wired only from `method_registry*.go`.
- **Invariant**: every method runs through authenticated minibind; nil deps
  omit registration rather than panicking. Memory writes go through the wiki
  port — never write wiki files directly from this package.
- `ParseSender` is the shared sender parse for mail analysis and people
  surfaces; do not fork email/display splitting elsewhere.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/runtime/rpc/handler/handlerminiapp/knowledge
```
