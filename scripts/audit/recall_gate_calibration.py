#!/usr/bin/env python3
"""Calibrate the recall episodic-silence gate from the recall-utility ledger.

arXiv 2607.14390 (Rekal) separates useful from harmful episodic injections by
the ranked block's top-1 score and top1-top2 gap (their hit/miss means: 0.91 vs
0.87, gap 0.046 vs 0.017), and silences low-confidence injections. Deneb's
doctrine (recall is the bottleneck) forbids turning on such a gate without
grounding, so the recall preflight records the signals in SHADOW mode: every
inject line in ~/.deneb/wiki/.recall-hits.jsonl carries top1/gap/cue alongside
the read/cite use events (#4050 retrieval context, #4055 observed use).

This script joins exposure to use and answers: do the signal distributions
separate used from unused turns, and what would candidate thresholds have
suppressed? Advisory, operator-run; it never modifies the ledger.

Usage:
  scripts/audit/recall_gate_calibration.py [--ledger ~/.deneb/wiki/.recall-hits.jsonl]
      [--use-window-min 30] [--min-turns 30]
"""

import argparse
import collections
import json
import pathlib
import statistics


def load_lines(path: pathlib.Path) -> list[dict]:
    lines = []
    with path.open(encoding="utf-8") as f:
        for raw in f:
            raw = raw.strip()
            if not raw:
                continue
            try:
                lines.append(json.loads(raw))
            except json.JSONDecodeError:
                continue
    return lines


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument(
        "--ledger",
        default=str(pathlib.Path.home() / ".deneb/wiki/.recall-hits.jsonl"),
    )
    ap.add_argument("--use-window-min", type=int, default=30)
    ap.add_argument(
        "--min-turns",
        type=int,
        default=30,
        help="below this many signal-bearing auto turns, refuse to sweep thresholds",
    )
    args = ap.parse_args()

    lines = load_lines(pathlib.Path(args.ledger).expanduser())
    injects = [
        ln for ln in lines if ln.get("event", "inject") in ("", "inject")
    ]
    uses = [ln for ln in lines if ln.get("event") in ("read", "cite")]

    # A "turn" is one preflight batch: same session + same write timestamp.
    turns: dict[tuple[str, int], dict] = {}
    legacy = 0
    for ln in injects:
        if "top1" not in ln:
            legacy += 1
            continue
        key = (ln.get("session", ""), int(ln.get("at", 0)))
        t = turns.setdefault(
            key,
            {
                "top1": float(ln.get("top1", 0)),
                "gap": float(ln.get("gap", 0)),
                "cue": bool(ln.get("cue", False)),
                "paths": set(),
                "used": False,
            },
        )
        t["paths"].add(ln.get("path", ""))

    # Attribute use: a read/cite of an injected path in the same session within
    # the window after the turn marks the turn as used.
    window_ms = args.use_window_min * 60 * 1000
    uses_by_session = collections.defaultdict(list)
    for u in uses:
        uses_by_session[u.get("session", "")].append(u)
    for (session, at), t in turns.items():
        for u in uses_by_session.get(session, []):
            if u.get("path", "") in t["paths"] and 0 <= int(u.get("at", 0)) - at <= window_ms:
                t["used"] = True
                break

    all_turns = list(turns.values())
    auto = [t for t in all_turns if not t["cue"]]
    cue = [t for t in all_turns if t["cue"]]
    print(f"ledger lines={len(lines)} inject={len(injects)} (legacy w/o signals={legacy}) use-events={len(uses)}")
    print(f"signal-bearing turns={len(all_turns)}  auto={len(auto)}  cue={len(cue)}")

    def dist(label: str, ts: list[dict]) -> None:
        used = [t for t in ts if t["used"]]
        unused = [t for t in ts if not t["used"]]
        print(f"\n== {label}: turns={len(ts)} used={len(used)} unused={len(unused)}")
        for name, group in (("used", used), ("unused", unused)):
            if not group:
                print(f"  {name}: (none)")
                continue
            top1s = [t["top1"] for t in group]
            gaps = [t["gap"] for t in group]
            print(
                f"  {name}: top1 mean={statistics.mean(top1s):.4f} median={statistics.median(top1s):.4f}"
                f" | gap mean={statistics.mean(gaps):.4f} median={statistics.median(gaps):.4f}"
            )

    dist("auto (non-cue) turns — the gate's target surface", auto)
    dist("cue turns — never gated, reference only", cue)

    if len(auto) < args.min_turns:
        print(
            f"\nthreshold sweep SKIPPED: only {len(auto)} signal-bearing auto turns"
            f" (< {args.min_turns}) — let the shadow ledger accumulate first."
        )
        return

    print("\n== threshold sweep (auto turns): silence when top1<T1 or gap<Tg")
    print(f"{'T1':>6} {'Tg':>7} {'silenced':>9} {'good':>5} {'harm':>5}  (good=unused suppressed, harm=used suppressed)")
    top1_grid = sorted({round(q, 2) for q in (statistics.quantiles([t["top1"] for t in auto], n=10))})
    gap_grid = sorted({round(q, 3) for q in (statistics.quantiles([t["gap"] for t in auto], n=10))})
    for t1 in top1_grid:
        for tg in gap_grid:
            silenced = [t for t in auto if t["top1"] < t1 or t["gap"] < tg]
            harm = sum(1 for t in silenced if t["used"])
            good = len(silenced) - harm
            if silenced:
                print(f"{t1:>6} {tg:>7} {len(silenced):>9} {good:>5} {harm:>5}")


if __name__ == "__main__":
    main()
