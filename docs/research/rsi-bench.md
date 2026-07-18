---
title: "RSI Bench"
summary: "Process + utility bench for recursive self-improvement, grounded in 2026H1 paper mappings. Distinct from Health Bench 3.0."
read_when:
  - "Implementing or reviewing RSI Bench / rsi_bench"
  - "Calibrating RSI process or utility scores, baselines, or domain weights"
  - "Wiring RSI evidence into meta epochs without LLM self-grading"
sidebarTitle: "RSI Bench"
---

# RSI Bench

> Status: **rubric 1.2.0** (2026-07-15). Sibling to Health Bench 3.0.
> Operator rule: `docs/agent-rules/rsi-bench.md`.

## North star

**100 = the improvement procedure accepts honestly (process) and those
acceptances land as operator-visible utility (utility), without human
structural rescue of the loop itself.**

Geometric mean of Process (0.55) and Utility (0.45). LLM judges never score
this bench.

## Rubric 1.2.0

### Process (0.55)

| Metric | W | Paper | Signal |
|---|---:|---|---|
| acceptor-trust | 16 | PACE, SEA | false-accept + resolved n; soft watch (≥3) capped |
| confirm-honesty | 10 | CoVerRL | confirmRate with n |
| judge-fuel | 12 | CoEvoSkills, BabelJudge | accuracy + misses + falseRejects |
| preference-collapse | 8 | BabelJudge | byCategory accuracy spread |
| swap-consistency | 10 | BabelJudge | run-to-run byClass stability (order/swap proxy) |
| probe-coverage | 6 | BabelJudge | byClass coverage breadth |
| timescale-turn | 12 | MetaSkill-Evolve | L1+L2 activity |
| ability-transfer | 12 | EvoAgentBench | validation∩evolve/genesis + diversity + opportunities |
| anti-collapse | 14 | Bilevel Autoresearch | thrash + parametric streak |

### Utility (0.45) — ratcheted

| Metric | W | Paper / P5 | Signal |
|---|---:|---|---|
| closure-land | 25 | SkillSmith, L4 | self_correction propose→land |
| operator-verdict | 20 | ANCHOR | feed-card / meta adopt |
| codebase-delta | 20 | P5-5 | health-v3 live−baseline |
| retention-proxy | 15 | CPE | confirm / soft watch keep |
| dispatch-land | 20 | SkillSmith L4 | coding_dispatch **outcome** land fidelity, RHAE-weighted (`min(1.15, (1/attempts)²)` per land; attemptId ordinal) |

**Expect-band: 25–50.** Soft watch requires `soft_confirmed ≥ 3`
(open watches ∪ evolved-in-28d skills with ≥3 real post-evolve uses).
Proxy ceilings: `swap-consistency` ≤58 (≤52 when saturated), `ability-transfer` ≤58
until literal KO/EN swap corpus / ability-graph edges land.
`--check` also requires confidence ≥ `MIN_CHECK_CONFIDENCE` (60).

### Profiles / ratchet

- `fast`: ledgers + `rsi-bench-cache.json`
- `deep` / `--refresh-cache`: live health + Health Bench 3 overall embed (**force-rewrites** health-v3 snapshot)
- `--check` ratchets **Process and Utility** (confidence gate applies)
- Combined gate: `make bench-check` (health-v3-check + rsi-bench-check)
- Daily cadence: `make bench-refresh` / `scripts/systemd/setup-bench-refresh.sh` (04:30)
- Rubric bumps: `--migrate-rubric`

## Integration

| Surface | Role |
|---|---|
| Health Fitness | Thin re-export of RSI `closure-land` / `operator-verdict` |
| MetaEvolutionTask.RSIBench | Advisory evidence from baseline JSON |
| Meta QualityBench | Consumes `health-v3-snapshot.json` live delta |
| `make rsi-bench-test` | Includes `test_rsi_bench*.py` |

## Commands

```bash
make rsi-bench
make rsi-bench-check
make bench-check
make rsi-bench-deep
make bench-refresh
make rsi-bench-test
make rsi-bench-baseline
```

## Still deferred

- Literal BabelJudge KO/EN bilingual swap corpus (English/Korean framing pairs)
- Full EvoAgentBench ability-graph edges (beyond validation∩skill coverage)
- Hard `evolve_confirmed` sparse-traffic path (6-use window; prefer soft ≥3 or stale confirm)
