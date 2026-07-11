# Portable Deneb-Briefcase cases

Each child directory is a self-contained, synthetic casepack. Files inside a
casepack must all be declared by its signed manifest.json; keep explanatory
documentation at this parent level so the fail-closed loader does not treat it
as an undeclared input.

## `budget-supersession-v1`

This P2 smoke case contains one primary timeline record, one durable-memory
snapshot, and a sealed three-role evaluation fixture:

- `wiki-budget-v1` is marked `memory: true`. It carries the older approved value
  and is visible only to the `memory-assisted` arm.
- `mail-budget-v2` is primary evidence. It is released to both arms at the
  `budget-update` episode and supersedes the old wiki value.
- `sealed-grader-plan` is grader-only and visible to neither arm.
- `sealed-supervisor-plan` supplies three hidden checkpoints: the initial
  attempt plus the signed budget of two follow-ups.
- `sealed-user-simulator-plan` supplies two generic follow-up messages. Neither
  message contains the hidden budget answer or rubric wording.

Expected visible source IDs are:

| Point | `raw-primary` | `memory-assisted` |
|---|---|---|
| At `frozenNow` | none | `wiki-budget-v1` |
| After `budget-update` | `mail-budget-v2` | `wiki-budget-v1`, `mail-budget-v2` |

The two arms use the same signed manifest, seed, event timing, primary mail, and
grader. `raw-primary` withholds every source explicitly marked `memory: true`
and disables recall preflight; `memory-assisted` includes those sources under
their original access timing. The comparison must use separate disposable run
roots and otherwise identical model and budget settings.

Both arms expose the same bounded case-local Wiki record adapter. Only
`memory-assisted` indexes the marked source into a RunRoot-local production Wiki
search store used by Deneb's automatic recall dependency; `raw-primary` has
neither that page nor recall preflight. Production Wiki status, index, and
mutation actions are not exposed to either arm.

This case is a contract smoke test, not evidence that memory improves the final
answer: the revised primary mail is sufficient for both arms to answer `120`.
Its purpose is to prove that durable memory is present in exactly one arm while
supersession and sealed grading remain identical.

Validate the signed case from `gateway-go`:

```bash
go run ./cmd/deneb-briefcase validate \
  --case ../bench/briefcase/portable/budget-supersession-v1

go run ./cmd/deneb-briefcase manifest-digest \
  --manifest ../bench/briefcase/portable/budget-supersession-v1/manifest.json

go run ./cmd/deneb-briefcase supervisor-plan-digest \
  --plan ../bench/briefcase/portable/budget-supersession-v1/sealed/supervisor-plan.json
```

The standalone `run` command defaults to `memory-assisted`. For a paired run,
invoke it once with `--arm raw-primary` and once with
`--arm memory-assisted`, using separate output files and otherwise identical
settings. The equivalent library values are `ArmRawPrimary` and
`ArmMemoryAssisted`. `--skip-recall` alone does not filter `memory: true`
sources and is not a valid raw-primary run.

The three-role path uses `loop` with both sealed plans:

```bash
go run ./cmd/deneb-briefcase loop \
  --case ../bench/briefcase/portable/budget-supersession-v1 \
  --base-url http://127.0.0.1:8000/v1 \
  --model test-model \
  --arm memory-assisted \
  --supervisor-plan ../bench/briefcase/portable/budget-supersession-v1/sealed/supervisor-plan.json \
  --user-plan ../bench/briefcase/portable/budget-supersession-v1/sealed/user-simulator-plan.json
```

The loop result contains a grader-private `supervisorAudit`; do not feed the
result JSON back to the executor. Only the typed coarse handoff and sanitized
follow-up text cross the information firewall.
