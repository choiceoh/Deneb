# Propus doctrine and lifecycle summaries

Owns the product contract and pure summary builders for the Propus
self-improvement loop. Handlers and tools project these values — they must not
rebuild doctrine or overview state ad hoc.

## Entry points

- `doctrine.go` — `PropusDoctrine`, `PropusDoctrineSpec`, `PropusDoctrinePaper`
- `system.go` — `BuildPropusSystemIdentity`, `BuildPropusOverview`,
  `BuildPropusLifecycleSummary`, `EvaluatePropusDoctrineCoverage`,
  `PropusLifecycleCounts`
- Key types: `PropusLifecycleSummary`, `PropusLifecycleSummaryInput`,
  `PropusOverview`, `PropusSystemIdentity`

## Dependency direction and invariants

- **Dependency / boundary**: pure domain leaf — no imports of runtime RPC,
  chat, or tracker I/O. Upstream packages (`propusview`, `skillsrpc`) convert
  genesis tracker types then call these builders.
- **Invariant**: `PropusDoctrine()` is the single source for name/codename/
  principles/quality gates. `BuildPropusLifecycleSummary` is the only place
  that derives native lifecycle summary state/cues — UI must render that
  model, not invent a second interpretation of the event feed.
- Scope is only `"global"` or `"skill"` (`PropusScopeGlobal` /
  `PropusScopeSkill`); unknown scopes normalize to global.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/domain/skills/genesis/propus
```
