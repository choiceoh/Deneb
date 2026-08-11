# Briefcase pack contract map

Owns the on-disk briefcase pack schema: load, canonical digest, and
validation. Runtime harnesses live under `runtime/briefcase`; keep this
package a pure domain contract.

## Entry points

- `loader.go` — `LoadDir` (directory → `Pack`)
- `types.go` — `Pack`, `Manifest`, episode/artifact policy types
- `validate.go` — `Validate`, `ValidationError`
- `digest.go` — `CanonicalDigest`, `FileDigest`
- `json.go` — `RejectDuplicateJSONKeys`
- `number.go` — `ParseBoundedRational`, `BoundedRationalKey`

## Dependency direction and invariants

- **Dependency / boundary**: domain leaf — must not import
  `runtime/briefcase`, chat, or RPC handlers. Callers pass filesystem roots;
  do not open network or session stores here.
- **Invariant**: `Validate` is the single source of truth before a pack runs;
  digests must be canonical (stable key order); duplicate JSON keys are
  forbidden; never mutate manifest fields without recomputing
  `CanonicalDigest`.
- Feedback subpackage (`feedback/`) is optional simulation only — keep its
  types out of the core validate path.

## Local change scope

Schema and validation changes stay in `domain/briefcase`.

- May co-change: `runtime/briefcase` harness, `runcontract`, and eval graders
  that consume `Pack`.
- Do not touch: chat tool wiring or wiki Store APIs.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/domain/briefcase
```
