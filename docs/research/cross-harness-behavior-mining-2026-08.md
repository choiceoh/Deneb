# Cross-Harness Behavior Mining: First Corpus Read (2026-08)

Point-in-time research note. Numbers below are the 2026-08-28 srv4 corpus;
re-run the miner for current data.

## Context

The fleet runs several coding harnesses against this repository (Claude Code,
Codex CLI, Cursor, ZCode, Trae). The RSI backlog idea "cross-harness behavior
mining" (from the LangChain harness-talk review) is: observe how different
harnesses behave on the same codebase, find winner behaviors, and port them
into our own harness configuration and prompts. It had three missing pieces —
a fixed-task runner, trajectory normalization, and the mining loop.

The numbat review (2026-08-28, memory `numbat-review-adoption`) closed the
normalization gap: numbat (github.com/perplexityai/numbat, Apache-2.0, pinned
v0.2.0 / record schema 0.3.0) parses Claude Code, Codex, and Cursor on-disk
session artifacts into one event vocabulary, retroactively and read-only. Its
security-rule layer was rejected for standing use (about 90% of findings on
our corpus are normal fleet-ops work); the trajectory normalizer is what we
adopt.

`scripts/audit/harness-behavior-miner.py` is the mining loop MVP on top of it:

```
numbat timeline --format json  (per artifact, shellout, version-pinned)
  → merge session groups        (subagent artifacts reunite via session_id)
  → deterministic metrics       (verification discipline, tool mix, retries,
                                 scripting habits, context discipline)
  → episodes-ledger join        (Claude Code → landed-commit outcome)
  → per-harness contrast report (aggregation only; no LLM scoring)
```

Doctrine matches rsi-bench: the tool never scores with an LLM; interpretation
lives in documents like this one.

## Corpus (srv4, 2026-08-28)

| harness | artifacts | parsed | sessions | events | notes |
|---|---|---|---|---|---|
| Claude Code | 76 | 100% | 28 | 47,378 | subagent + workflow transcripts merged |
| Codex CLI | 3 | 100% | 3 | 670 | see finding 1 |
| Cursor | 249 | 100% | 232 | 41,271 | no per-event timestamps |

Full-corpus wall time is about 2 minutes on GB10 arm64; the run is cheap
enough to be on-demand (no daemon, no hooks, no cron).

## Findings

**1. The corpus asymmetry is itself the top finding.** Codex has 3 sessions
(July, manual) because the fleet's main Codex consumer — the RSI L4
coding-dispatch lane — runs `codex exec --ephemeral`
(`scripts/dev/coding_dispatch_executor.py`), which persists no session
artifacts. The single biggest Codex behavior source is invisible to mining.
Follow-up: persist dispatch sessions (drop `--ephemeral`, or point
`CODEX_HOME` at a mining-visible location) — one-line change, but it touches
the L4 lane, so it ships separately with operator awareness.

**2. Session shapes are three different animals.** Medians: Claude Code 560
events / 192 execs / 14.5 user prompts per session (long multi-task grinds;
durations are wall-span including idle); Codex 302 events / 116 execs / 2
prompts / 14 min (short single-task runs); Cursor 92.5 events / 14 execs / 1
prompt across 232 sessions (many small interactive touches). Cross-harness
rate comparisons must be read against these shapes, not as like-for-like.

**3. Verification discipline, the first porting candidate.** Share of writing
sessions with a build/test gate after the last write: Codex 100% (n=3, weak),
Cursor 70.9%, Claude Code 45.0%. Claude Code is the laggard, but the
denominator is polluted: its `file.write` events include out-of-repo writes
(memory files, scratchpads) where no gate is expected. Before acting on this,
v2 should scope the metric to writes under the session's `project_path`. The
direction is still consistent with the landed-commit contrast below.

**4. Tool-mapping asymmetry bounds cross-harness reads.** Claude Code does its
reading through Bash (`grep` 21.5%, `head` 17.1% of its command mix — only
547 `file.read` events), while Cursor uses native read tools (9,897
`file.read`, reads/write 11.4 vs Claude's 0.75). numbat maps each harness's
native tools into the closed vocabulary at different depths. *Within*-harness
trends over time are trustworthy; *absolute* cross-harness tool-mix ratios are
not. This is a property of the substrate to keep in mind on every future read.

**5. Context discipline is visibly prompt-driven.** `| head` / `| tail` limit
pipes per 100 execs: Claude Code 65.5, Cursor 57.3, Codex 4.6. The two
harnesses that load this repo's CLAUDE.md guidance ("noisy output → tail -N")
do it; Codex (July sessions, AGENTS.md era) essentially never did. Evidence
that harness instructions actually shape command style — the mechanism
behavior mining wants to exploit in reverse.

**6. Landed-commit contrast (Claude Code, episodes join n=11, directional
only).** Sessions that landed commits vs sessions that did not: verify
command 85.7% vs 25.0%, verify-after-last-write 50% vs 0%, median execs 203
vs 101.5, median duration 384 vs 77 min, reads/write 0.31 vs 7.08, identical
retries 6.2 vs 0 per 100 execs. Caveat: no-commit does not mean failure — the
high-read no-commit sessions look like intended analysis/review work. The
join covers 11/28 sessions because the episodes ledger is younger than the
transcript corpus; coverage grows on its own. Outcome mining needs
task-intent labels before the contrast can name winners.

## What this enables next (in order)

1. **Repo-scoped write metric** (v2 of finding 3) — cheap, sharpens the one
   actionable discipline delta.
2. **Codex dispatch persistence** (finding 1) — unlocks the volume source;
   separate PR touching the L4 lane.
3. **ZCode reader** — ZCode is outside numbat's coverage but its rollout
   store (`model-io-sess_*.jsonl` under the ZCode CLI state dir) is model-IO
   level, richer than events; a custom reader is session-sized work if ZCode
   inclusion is wanted. Trae stays unsupported on both sides. <!-- docref:ignore -->
4. **Fixed-task runner** — the remaining piece of the original backlog item:
   same task set through Claude Code + Codex (+ our gateway agent), compare
   trajectories on shared ground instead of organic corpus shapes.

## How to run

```bash
GOBIN=~/go/bin go install github.com/perplexityai/numbat/cmd/numbat@v0.2.0
```

```bash
python3 scripts/audit/harness-behavior-miner.py --out /tmp/mine-out
```

Outputs `report.md`, `sessions.jsonl` (per-session metrics), and
`parse-failures.txt` (dropped artifacts, never silent). The miner warns when
the numbat binary's record schema drifts off 0.3.0. Treat outputs as
sensitive as the transcripts they summarize: keep them local.
