# Routine briefings (morning / evening / weekly)

Owns collection and server-side rendering for recurring business briefings
shared by cron, interactive commands, and deferred tools. Collection and render
live together so every caller shares one contract.

## Entry points

- `morning_letter.go` — `ToolMorningLetter`, `CollectMorningLetterData`, `MorningLetterOpts`
- `morning_card.go` — fixed deneb-ui renderer for model-filled semantic slots or facts-only fallback
- `evening_letter.go` — `ToolEveningLetter`, `EveningLetterOpts`
- `weekly_report.go` — `CollectWeeklyReportData`, `BuildWeeklyReportPDF`,
  `BuildWeeklyReportImage`, `WeeklyReportOpts`
- `weekly_card.go` — `RenderWeeklyReportCard` (deneb-ui card projection)
- `letter_common.go` — shared letter assembly helpers

## Dependency direction and invariants

- **Dependency / boundary**: may use `toolport`, market/wiki ports, calendar,
  localcal, and mailarchive. Must not import the parent `tools` package or chat
  run lifecycle.
- **Invariant**: briefing format, facts, market values, and escaping are
  deterministic given the same opts + clock. A scheduled morning letter may
  use one no-tool model turn for semantic JSON slots; model failure must retain
  the same facts-only card. PDF/image rendering must degrade to text fallback
  when Chromium is unavailable (`VisualRenderReady` / `ChromiumBinary`). Do
  not invent a second weekly data path outside `CollectWeeklyReportData`.
- Cron and chat tool callers must share these constructors — do not duplicate
  briefing assembly in `runtime/server`.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/routine
```
