# Tool presets (allowed / deferred tool sets)

Owns named tool presets that bound which tools a session or spawn may load.
Presets are pure policy data — registration and execution live elsewhere.

## Entry points

- `preset.go` — `Preset`, `AllowedTools`, `PreloadedDeferredTools`, `IsValid`,
  `KnownPresets`, `SpawnPresets`

## Dependency direction and invariants

- **Dependency / boundary**: leaf package with no internal imports. Chat
  tools, heartbeat, and session spawn consume presets; this package must not
  import tool implementations or the chat pipeline.
- **Invariant**: `AllowedTools` is the single allowlist source for a preset.
  `PreloadedDeferredTools` only returns names already allowed. Unknown preset
  strings fail `IsValid` — callers must not invent ad-hoc allowlists.
- Spawn presets are a strict subset of known presets (`SpawnPresets`).

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/pipeline/toolpreset
```
