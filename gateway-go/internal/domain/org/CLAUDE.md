# Org chart (조직도) domain map

Owns the operator group org tree and derived dashboard lane / classification
rules. The editable tree is the master source; classification rulesets are
derived from it.

## Entry points

- `loader.go` — `Load`, `LoadFromFile`, `ResolvePath`, `LoadRules`,
  `LoadLanes`
- `org.go` — `OrgTree`, `OrgNode`, `Member`, `LaneDef`, `RankOrder`
- Derived rules flow through `LoadRules` → `classification.Rules`

## Dependency direction and invariants

- **Dependency / boundary**: domain leaf for org JSON — must not import
  chat, RPC, or wiki Store. Downstream mail/workfeed classification
  consumes derived rules; do not reverse that direction.
- **Invariant**: `Load` / `LoadFromFile` are the single source for the live
  tree; lane tags on nodes must stay consistent with `LoadLanes`; never
  hand-edit derived classification rules in callers — regenerate via
  `LoadRules`.
- Tests use FAKE org data only (`loader_test.go` / `org_test.go`).

## Local change scope

Org schema and loaders stay in `domain/org`.

- May co-change: classification consumers and dashboard lane UI contracts
  when `LaneDef` changes.
- Do not touch: proactive delivery or tool wiring.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/domain/org
```
