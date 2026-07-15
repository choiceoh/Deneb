# Model tuner (measure → threshold → adjust)

Owns the background per-model optimization loop: aggregate agent-log
scorecards, evaluate tuning rules, auto-apply the one safe adjustment
(output-token floor), persist `~/.deneb/model-stats.json`, and notify the
operator only when recommendations change.

## Entry points

- `tuner.go` — `NewTask`, `Task`, `Deps`, `Scorecard`, `Calibration`,
  `LoadScorecard`, `DefaultStatePath`
- `rules.go` — `Analyze`, `Fingerprint`, `Recommendation`
- `effort.go` — adaptive effort-router nudge from `AggregateEffortByModel`
- `note.go` — `Scorecard.NoteFor`, `Scorecard.AdvisoryLines`

## Dependency direction and invariants

- **Dependency / boundary**: may use `ai/llm`, `ai/modelrole`, `ai/router`,
  `core/agentlog`, `infra/config`. Must not import chat handlers or RPC
  packages — the server wires `Deps` and schedules `Task` as a periodic job.
- **Invariant**: only the safe output-token floor is auto-applied; all other
  recommendations stay advisory. Notify only when `Fingerprint` of the
  recommendation set changes. New local (vLLM) models get a one-shot
  calibration probe before taking real traffic.
- Scorecard persistence goes through `DefaultStatePath` / `StatePath` — do not
  write a second stats file from callers.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/ai/modeltuner
```
