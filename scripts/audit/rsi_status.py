"""Recursive self-improvement (RSI) loop-status surface.

The RSI subsystem is four stacked loops (L1 skill evolution, L2 meta-evolution,
L3 verifier co-evolution, L4 source self-edit). Each writes an append-only
ledger under ``~/.deneb/data/``. Once the machinery is built, the interesting
question is no longer "does it compile" but "is each loop actually TURNING, or
is it built-but-waiting?" — a distinction the ledgers answer but which nobody
reads by hand twice the same way.

This module classifies each layer into one honest state:

  LIVE        the loop is producing AND consuming — it is turning.
  DATA-GATED  built and running, but the fuel it needs has not accumulated yet
              (e.g. the judge catches every planted defect so there are no
              labeled misses for L3 to learn from). Time fixes this, not code.
  STARVED     built, but its INPUT source is empty — an upstream/wiring gap that
              CODE fixes (e.g. L4 dispatch wants code-scope candidates but no
              source produces them).
  FROZEN      a self-brake deliberately halted the loop (L2 auto-adopt freeze).
  IDLE        no recent activity at all — the lane may be unscheduled.

Honesty (mirrors runtime_health's fault accounting): DATA-GATED is NOT a defect
— it is the correct state of a young loop with no data. Reporting it as a
problem would push toward manufacturing fake fuel. STARVED, by contrast, is an
actionable wiring gap. The two look similar in a naive "0 events" count; the
whole point of this surface is to tell them apart.

stdlib-only and importable for deterministic tests; the CLI is
``scripts/audit/rsi-status.py``.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
from dataclasses import dataclass, field
from typing import Any, TextIO

DEFAULT_DATA_DIR = os.path.expanduser("~/.deneb/data")
WINDOW_DAYS = 7
DAY_MS = 24 * 60 * 60 * 1000

# Degradation classes the judge-accuracy lane replays. The BLATANT ones a
# competent judge always catches (so 0 misses there means nothing); the SUBTLE
# ones are the ones that actually produce labeled misses (P3 fuel).
BLATANT_CLASSES = frozenset({"section-drop", "fake-tool", "truncation", "overfit"})
SUBTLE_CLASSES = frozenset({"imperative-drop", "safety-drop"})

# L4 dispatch supply contract (must match scripts/dev/coding-dispatch.sh): a
# candidate is dispatchable only if it is a proposed, code-scope correction from
# an evidence-bearing source. health-finding graduated 2026-07-12 (first batch
# reviewed clean); runtime-error stays staged until its own batch review.
L4_SOURCES = ("evolve-tool-gap", "self-harness", "health-finding")

LIVE, DATA_GATED, STARVED, FROZEN, IDLE = "LIVE", "DATA-GATED", "STARVED", "FROZEN", "IDLE"


@dataclass
class LayerStatus:
    key: str
    title: str
    state: str
    metrics: dict = field(default_factory=dict)
    diagnosis: str = ""


def read_jsonl(path: str) -> list[dict]:
    """Load an append-only JSONL ledger, tolerating partial/corrupt lines."""
    out: list[dict] = []
    try:
        with open(path, encoding="utf-8", errors="replace") as handle:
            for line in handle:
                line = line.strip()
                if not line:
                    continue
                try:
                    out.append(json.loads(line))
                except json.JSONDecodeError:
                    continue
    except OSError:
        return []
    return out


def _within(created_ms: Any, cutoff_ms: int) -> bool:
    return isinstance(created_ms, (int, float)) and created_ms >= cutoff_ms


def assess_l1(events: list[dict], now_ms: int) -> LayerStatus:
    """Skill evolution — genesis-log lifecycle events in the 7d window.

    The ledger keys each record by ``type`` (evolved, evolve_rejected,
    evolution_proposal, genesis). Post-evolve confirmations/rollbacks, when they
    occur, ride the usage rollback watch (tracker_usage), not this log — so
    "turning" is measured by committed evolves and new skills, not confirmations.
    """
    cutoff = now_ms - WINDOW_DAYS * DAY_MS
    type_bucket = {
        "evolved": "evolved",
        "genesis": "genesis",
        "evolution_proposal": "proposal",
        "evolve_rejected": "rejected",
    }
    counts = {"evolved": 0, "genesis": 0, "proposal": 0, "rejected": 0}
    for e in events:
        if not _within(e.get("createdAt"), cutoff):
            continue
        bucket = type_bucket.get(e.get("type") or "")
        if bucket:
            counts[bucket] += 1
    metrics = dict(counts)
    committed = counts["evolved"] + counts["genesis"]
    if sum(counts.values()) == 0:
        return LayerStatus("L1", "skill evolution", IDLE, metrics,
                           "no genesis-log events in 7d — evolver/genesis lanes idle")
    if committed > 0:
        return LayerStatus("L1", "skill evolution", LIVE, metrics,
                           f"{counts['evolved']} evolved / {counts['genesis']} new skills / "
                           f"{counts['proposal']} proposals / {counts['rejected']} rejected (7d)")
    return LayerStatus("L1", "skill evolution", DATA_GATED, metrics,
                       f"{counts['proposal']} proposals but 0 committed — candidates not clearing the gate")


def assess_l2(revisions: list[dict], frozen: bool, now_ms: int) -> LayerStatus:
    """Meta-evolution — weekly slow-loop revisions + auto-adopt freeze state."""
    cutoff = now_ms - 2 * WINDOW_DAYS * DAY_MS  # slow loop: 2-week look-back
    cycles = proposed = adopted = reverted = 0
    last = ""
    for r in revisions:
        if not _within(r.get("createdAt"), cutoff):
            continue
        action = r.get("action") or ""
        if action in ("auto_adopted", "adopted"):
            adopted += 1
        elif action in ("auto_reverted", "operator_reverted"):
            reverted += 1
        elif action == "":
            cycles += 1
            if r.get("proposed"):
                proposed += 1
        if not last:
            last = f"{r.get('epoch', '?')}/{os.path.basename(r.get('artifact', '?'))}"
    metrics = {"cycles": cycles, "proposed": proposed, "adopted": adopted, "reverted": reverted}
    if frozen:
        return LayerStatus("L2", "meta-evolution", FROZEN, metrics,
                           "drift self-brake engaged — auto-adopt frozen to propose-only")
    if cycles == 0 and adopted == 0:
        return LayerStatus("L2", "meta-evolution", IDLE, metrics,
                           "no slow-loop cycles in 14d — awaiting the weekly cadence")
    return LayerStatus("L2", "meta-evolution", LIVE, metrics,
                       f"{cycles} cycles / {proposed} proposed / {adopted} adopted / "
                       f"{reverted} reverted (14d), last {last}")


def assess_l3(runs: list[dict], now_ms: int) -> LayerStatus:
    """Verifier co-evolution (P3) — judge-accuracy lane misses are the fuel."""
    cutoff = now_ms - WINDOW_DAYS * DAY_MS
    recent = [r for r in runs if _within(r.get("createdAt"), cutoff)]
    if not recent:
        return LayerStatus("L3", "verifier co-evolution", IDLE, {"runs": 0},
                           "judge-accuracy lane has not run in 7d")
    misses = 0
    false_rejects = 0
    classes_seen: set[str] = set()
    for r in recent:
        misses += len(r.get("misses") or [])
        false_rejects += len(r.get("falseRejects") or [])
        classes_seen.update((r.get("byClass") or {}).keys())
    subtle_deployed = bool(classes_seen & SUBTLE_CLASSES)
    metrics = {
        "runs": len(recent),
        "misses": misses,
        "false_rejects": false_rejects,
        "subtle_probes_deployed": subtle_deployed,
    }
    if misses > 0 or false_rejects > 0:
        return LayerStatus("L3", "verifier co-evolution", LIVE, metrics,
                           f"{misses} judge misses + {false_rejects} false-rejects over {len(recent)} runs "
                           "— P3 fuel accumulating for evaluator-epoch grounding")
    if not subtle_deployed:
        return LayerStatus("L3", "verifier co-evolution", DATA_GATED, metrics,
                           f"{len(recent)} runs, judge caught every BLATANT defect and subtle probes "
                           "are not in the ledger yet — awaiting the subtle-degradation deploy")
    return LayerStatus("L3", "verifier co-evolution", DATA_GATED, metrics,
                       f"{len(recent)} runs with subtle probes but 0 misses — judge is currently strong; "
                       "fuel appears when it slips")


def _merge_candidates(rows: list[dict]) -> tuple[dict[str, dict], dict[str, str]]:
    """Fold status-delta rows onto full candidate records (coding-dispatch.sh
    logic): a review row is {id,status,...}, not a full record, so status is
    merged rather than replacing the candidate."""
    cand: dict[str, dict] = {}
    status: dict[str, str] = {}
    for rec in rows:
        rid = rec.get("id") or ""
        if not rid:
            continue
        if rec.get("type") == "self_correction_candidate":
            cand[rid] = rec
        if rec.get("status"):
            status[rid] = rec["status"]
    return cand, status


def assess_l4(rows: list[dict], dispatch_total: int, dispatch_today: int) -> LayerStatus:
    """Source self-edit — the coding-dispatch supply of code-scope candidates."""
    cand, status = _merge_candidates(rows)
    by_scope: dict[str, int] = {}
    dispatchable = 0
    staged = 0
    staged_sources: dict[str, int] = {}
    for rid, rec in cand.items():
        st = status.get(rid, rec.get("status") or "proposed")
        scope = rec.get("scope") or "?"
        by_scope[scope] = by_scope.get(scope, 0) + 1
        src = rec.get("source") or ""
        # proposed = unreviewed backlog; accepted = review-endorsed, awaiting
        # implementation — both are live dispatch supply (the heartbeat review
        # lane accepts candidates it cannot implement itself).
        if scope == "code" and st in ("proposed", "accepted"):
            if src.startswith(L4_SOURCES):
                dispatchable += 1
            else:
                # Proposed code candidate from a source not yet in the dispatch
                # allowlist (runtime-error, health-finding, …): staged L4 supply
                # awaiting review/graduation — real fuel, NOT a wiring gap.
                staged += 1
                prefix = src.split(":", 1)[0] if src else "(no source)"
                staged_sources[prefix] = staged_sources.get(prefix, 0) + 1
    metrics = {
        "candidates": len(cand),
        "by_scope": by_scope,
        "dispatchable": dispatchable,
        "staged": staged,
        "staged_sources": staged_sources,
        "dispatched_total": dispatch_total,
        "dispatched_today": dispatch_today,
    }
    if dispatchable > 0 or dispatch_today > 0:
        return LayerStatus("L4", "source self-edit", LIVE, metrics,
                           f"{dispatchable} dispatchable code candidates, {dispatch_today} dispatched today")
    if len(cand) == 0:
        return LayerStatus("L4", "source self-edit", IDLE, metrics,
                           "no self-correction candidates — capture funnel idle")
    if staged > 0:
        staged_summary = ", ".join(f"{k}:{v}" for k, v in sorted(staged_sources.items()))
        return LayerStatus("L4", "source self-edit", STARVED, metrics,
                           f"{staged} code candidates staged from non-dispatch sources ({staged_summary}) "
                           "— propose-only supply awaiting allowlist graduation")
    scope_summary = ", ".join(f"{k}:{v}" for k, v in sorted(by_scope.items()))
    return LayerStatus("L4", "source self-edit", STARVED, metrics,
                       f"{len(cand)} candidates ({scope_summary}) but 0 are code-scope from "
                       f"{'/'.join(L4_SOURCES)} — no source produces code candidates (wiring gap)")


def assess(data_dir: str, now_ms: int) -> list[LayerStatus]:
    """Read every ledger under data_dir and classify all four layers."""
    genesis = read_jsonl(os.path.join(data_dir, "skill_genesis_log.jsonl"))
    revisions = read_jsonl(os.path.join(data_dir, "meta_evolution_log.jsonl"))
    judge = read_jsonl(os.path.join(data_dir, "judge_accuracy_log.jsonl"))
    candidates = read_jsonl(os.path.join(data_dir, "self_correction_candidates.jsonl"))
    frozen = os.path.exists(os.path.join(data_dir, "auto_adopt_freeze.json"))

    dispatch_dir = os.path.join(data_dir, "coding_dispatch")
    dispatch_total = dispatch_today = 0
    today_cutoff = now_ms - (now_ms % DAY_MS)
    try:
        for name in os.listdir(dispatch_dir):
            if not name.endswith(".json"):
                continue
            dispatch_total += 1
            try:
                if os.path.getmtime(os.path.join(dispatch_dir, name)) * 1000 >= today_cutoff:
                    dispatch_today += 1
            except OSError:
                continue
    except OSError:
        pass

    return [
        assess_l1(genesis, now_ms),
        assess_l2(revisions, frozen, now_ms),
        assess_l3(judge, now_ms),
        assess_l4(candidates, dispatch_total, dispatch_today),
    ]


# A layer is "turning" for the headline count when it is not merely idle/starved.
def turning(layers: list[LayerStatus]) -> int:
    return sum(1 for layer in layers if layer.state in (LIVE, FROZEN))


_STATE_GLYPH = {LIVE: "●", DATA_GATED: "◐", STARVED: "○", FROZEN: "❄", IDLE: "·"}


def print_summary(layers: list[LayerStatus], stream: TextIO) -> None:
    print(f"\nRSI loop status  ({turning(layers)}/{len(layers)} turning)", file=stream)
    print("─" * 72, file=stream)
    for layer in layers:
        glyph = _STATE_GLYPH.get(layer.state, "?")
        print(f"  {glyph} {layer.key}  {layer.title:<22} {layer.state}", file=stream)
        print(f"      · {layer.diagnosis}", file=stream)
    print("─" * 72, file=stream)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Deneb RSI loop-status surface.")
    parser.add_argument("--json", action="store_true")
    parser.add_argument("--data-dir", default=DEFAULT_DATA_DIR)
    parser.add_argument("--now-ms", type=int, default=None, help="override clock (tests)")
    return parser


def main(argv: list[str] | None = None, *, stdout: TextIO | None = None, stderr: TextIO | None = None) -> int:
    args = _parser().parse_args(argv)
    out = stdout if stdout is not None else sys.stdout
    err = stderr if stderr is not None else sys.stderr
    now_ms = args.now_ms if args.now_ms is not None else int(time.time() * 1000)
    layers = assess(args.data_dir, now_ms)

    if args.json:
        print(json.dumps(
            {"turning": turning(layers), "layers": [
                {"key": layer.key, "title": layer.title, "state": layer.state,
                 "metrics": layer.metrics, "diagnosis": layer.diagnosis}
                for layer in layers]},
            ensure_ascii=False, indent=1), file=out)
    else:
        print_summary(layers, err)

    print("DENEB_RSI_STATUS " + " ".join(f"{layer.key}={layer.state}" for layer in layers), file=out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
