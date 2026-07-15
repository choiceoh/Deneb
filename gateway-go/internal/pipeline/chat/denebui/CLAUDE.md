# deneb-ui card contract map

Owns label-HTML v2 parse/validate/compose for chat cards. Grammar SOT is
`docs/research/deneb-ui-html.md` — keep the three renderers in sync when the
wire format changes (see `docs/agent-rules/denebui.md`).

## Entry points

- `denebui.go` — `Validate`, `ExtractFences`, `HasFence`, `Issue`
- `html.go` — `ParseHTML`, `IsHTMLBody` (mini tokenizer; never DOMParser)
- `collapsed.go` — `CollapsedReportFence` (server-assembled collapsed cards)
- `composition.go` — `CompositionAdvisories`
- `card_health.go` — `ReportCardHealth`, `LooksStructuredWithoutCard`
- `finalize.go` — `NormalizeFinalReply` (pre-delivery validity boundary)
- `plaintext.go` — `PlainText`, `ReplaceFences`

## Dependency direction and invariants

- **Dependency / boundary**: leaf under chat — must not import the chat
  parent, `toolreg`, or `runtime/server`. Callers pass plain strings only.
- **Invariant**: `Validate` is the single source of truth for fence
  acceptance; never use HTML5 parsers (foster-parenting); never lowercase
  whole buffers for index math; legacy JSON fences are display-only strict
  parse — no repair heuristics, no new JSON authorship.
- Must keep parse order: extract fence → `ParseHTML` → `Validate` → optional
  collapse. Interactive nodes stay placeholder during streaming on clients.

## Local change scope

Card grammar and health signals stay in `denebui/`.

- May co-change: proactive fence consumers, prompt communication section,
  and Android/Andromeda deneb-ui renderers when the wire contract changes.
- Do not touch: tool execution, recall preflight, or wiki Store.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/pipeline/chat/denebui
```
