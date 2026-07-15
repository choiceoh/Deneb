# Mail RPC facade (Gmail triage + analysis)

Owns the stable `handler/mail` registration surface for the gateway server.
Leaf behavior lives in `gmailops` (list/get/archive) and `analyzebind`
(analyze/QA); this package re-exports them and keeps Gmail context helpers.

## Entry points

- `facade.go` — `GmailMethods`, `GmailDeps`, `GmailAnalyzeMethods`,
  `GmailAnalyzeDeps`, `PipelineFromMailAnalysis`, `NewAnalysisStore`
- `gmail_context.go` — `GmailContextMethods`, `GmailContextDeps`
- `contracts.go` — `MemorySearcher` alias shared with knowledge handlers

## Dependency direction and invariants

- **Dependency / boundary**: parent may import `gmailops` and `analyzebind`
  leaves plus `handlerminiapp` wire aliases. Server must keep importing only
  this package — never the leaves directly.
- **Invariant**: facade aliases are straight re-exports with no adapter logic.
  Wire DTOs for mail rows stay in `handlerminiapp` (`mail_wire.go`). Auth
  binding goes through `minibind`/`rpcutil`.
- Analysis/QA must degrade when LLM is unavailable (`ErrAnalyzeNoLLM`) rather
  than panicking.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/runtime/rpc/handler/mail
```
