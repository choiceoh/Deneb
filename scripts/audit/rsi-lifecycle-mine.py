#!/usr/bin/env python3
"""rsi-lifecycle-mine.py — mine patterns from the genesis lifecycle log.

The genesis subsystem writes every evolve/reject/rollback/confirm event to
``skill_genesis_log.jsonl`` (append-only JSONL under ``~/.deneb/data/``).
``miniapp.rsi.status`` classifies loop layers as LIVE/DATA-GATED/STARVED;
this tool goes one level deeper — it mines the EVENT STREAM for quality
patterns that the layer status cannot surface:

  - **Thrash**: skills that evolve → rollback repeatedly (the loop is spinning,
    not learning — roadmap: "memoryless repetition collapse").
  - **Stuck-reject**: skills rejected 3+ times without a single evolve (the
    evolver keeps proposing the same bad edit — anchor distillation failure?).
  - **Confirm gap**: evolves that never get confirmed within the watch window
    (evolve_watch_expired without a matching evolve_confirmed).
  - **Rollback rate by skill**: which skills' evolves are most fragile.
  - **Tool-gap clustering**: which tools are most frequently named as gaps.

These are the signals an operator needs to decide whether the loop is genuinely
improving or just churning — and they are invisible in the layer-status view.

Usage:
  rsi-lifecycle-mine.py                         # mine ~/.deneb/data
  rsi-lifecycle-mine.py --data-dir /path/to     # custom data directory
  rsi-lifecycle-mine.py --days 14               # look back N days (default: 7)
  rsi-lifecycle-mine.py --json                  # machine-readable

No gateway required — reads the JSONL ledgers directly. Fail-open: missing or
empty files produce a "no data" result, never an error.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from collections import Counter, defaultdict
from typing import Any


def read_jsonl(path: str) -> list[dict[str, Any]]:
    """Load an append-only JSONL ledger, tolerating partial/corrupt lines."""
    out: list[dict[str, Any]] = []
    if not os.path.isfile(path):
        return out
    with open(path, encoding="utf-8", errors="replace") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                out.append(json.loads(line))
            except ValueError:
                continue  # partial/corrupt line — skip
    return out


DAY_MS = 86_400_000


def mine(events: list[dict[str, Any]], window_ms: int) -> dict[str, Any]:
    """Mine quality patterns from lifecycle events within the time window."""
    now_ms = max((e.get("createdAt", 0) for e in events), default=0)
    if now_ms == 0:
        now_ms = 1  # avoid empty-window division; events with no timestamp get excluded
    cutoff = now_ms - window_ms

    recent = [e for e in events if e.get("createdAt", 0) >= cutoff]

    # ── Aggregate counts by type ──────────────────────────────────────────
    by_type: Counter[str] = Counter()
    for e in recent:
        by_type[e.get("type", "unknown")] += 1

    evolved = by_type.get("evolved", 0)
    rejected = by_type.get("evolve_rejected", 0)
    rolled_back = by_type.get("evolve_rolled_back", 0)
    confirmed = by_type.get("evolve_confirmed", 0)
    watch_expired = by_type.get("evolve_watch_expired", 0)
    genesis_new = by_type.get("genesis", 0)
    tool_gap = by_type.get("evolve_tool_gap_paired", 0)
    cross_reg = by_type.get("cross_skill_regression", 0)

    # ── Per-skill event sequences ─────────────────────────────────────────
    skill_events: dict[str, list[dict[str, Any]]] = defaultdict(list)
    for e in recent:
        skill = e.get("skillName") or e.get("skill") or ""
        if skill:
            skill_events[skill].append(e)
    # Sort each skill's events by timestamp.
    for events_list in skill_events.values():
        events_list.sort(key=lambda x: x.get("createdAt", 0))

    # ── Pattern: thrash (evolve → rollback cycles) ────────────────────────
    thrashers: list[dict[str, Any]] = []
    for skill, evs in skill_events.items():
        cycles = 0
        pending_evolve = False
        for e in evs:
            if e.get("type") == "evolved":
                pending_evolve = True
            elif e.get("type") == "evolve_rolled_back" and pending_evolve:
                cycles += 1
                pending_evolve = False
        if cycles >= 2:
            thrashers.append({"skill": skill, "cycles": cycles,
                              "events": len(evs)})

    # ── Pattern: stuck-reject (3+ rejects, no evolve) ─────────────────────
    stuck_reject: list[dict[str, Any]] = []
    for skill, evs in skill_events.items():
        rejects = sum(1 for e in evs if e.get("type") == "evolve_rejected")
        evolves = sum(1 for e in evs if e.get("type") == "evolved")
        if rejects >= 3 and evolves == 0:
            # Most common rejection reason.
            reasons = Counter(e.get("reason", "?") for e in evs if e.get("type") == "evolve_rejected")
            top_reason = reasons.most_common(1)[0][0] if reasons else "?"
            stuck_reject.append({"skill": skill, "rejects": rejects,
                                 "top_reason": top_reason[:80]})

    # ── Pattern: confirm gap (evolved but never confirmed/expired) ────────
    confirm_gap: list[str] = []
    for skill, evs in skill_events.items():
        has_evolve = any(e.get("type") == "evolved" for e in evs)
        has_confirm = any(e.get("type") == "evolve_confirmed" for e in evs)
        has_rollback = any(e.get("type") == "evolve_rolled_back" for e in evs)
        if has_evolve and not has_confirm and not has_rollback:
            confirm_gap.append(skill)

    # ── Pattern: rollback rate by skill ───────────────────────────────────
    rollback_rate: list[dict[str, Any]] = []
    for skill, evs in skill_events.items():
        ev = sum(1 for e in evs if e.get("type") == "evolved")
        rb = sum(1 for e in evs if e.get("type") == "evolve_rolled_back")
        if ev + rb >= 2 and ev > 0:
            rate = rb / (ev + rb)
            if rate > 0.3:
                rollback_rate.append({"skill": skill, "rate": round(rate, 2),
                                      "evolved": ev, "rolled_back": rb})

    # ── Pattern: tool-gap clustering ──────────────────────────────────────
    tool_gaps: Counter[str] = Counter()
    for e in recent:
        if e.get("type") == "evolve_tool_gap_paired":
            tool = e.get("tool") or e.get("toolName") or "?"
            tool_gaps[tool] += 1

    # ── Pattern: rejection reason frequency ───────────────────────────────
    reject_reasons: Counter[str] = Counter()
    for e in recent:
        if e.get("type") == "evolve_rejected":
            reason = (e.get("reason") or "?")[:60]
            reject_reasons[reason] += 1

    # ── Derived health signals ────────────────────────────────────────────
    confirm_rate = confirmed / evolved if evolved > 0 else 0
    rollback_rate_overall = rolled_back / evolved if evolved > 0 else 0
    accept_rate = evolved / (evolved + rejected) if (evolved + rejected) > 0 else 0

    return {
        "window_days": window_ms // DAY_MS,
        "total_events": len(recent),
        "counts": {
            "evolved": evolved,
            "rejected": rejected,
            "rolled_back": rolled_back,
            "confirmed": confirmed,
            "watch_expired": watch_expired,
            "genesis_new": genesis_new,
            "tool_gap_paired": tool_gap,
            "cross_skill_regression": cross_reg,
        },
        "rates": {
            "confirm_rate": round(confirm_rate, 3),
            "rollback_rate": round(rollback_rate_overall, 3),
            "accept_rate": round(accept_rate, 3),
        },
        "patterns": {
            "thrashers": sorted(thrashers, key=lambda x: -x["cycles"]),
            "stuck_reject": sorted(stuck_reject, key=lambda x: -x["rejects"]),
            "confirm_gap": sorted(confirm_gap),
            "high_rollback_rate": sorted(rollback_rate, key=lambda x: -x["rate"]),
        },
        "tool_gaps": tool_gaps.most_common(10),
        "reject_reasons": reject_reasons.most_common(10),
    }


def print_human(result: dict[str, Any]) -> None:
    days = result["window_days"]
    counts = result["counts"]
    rates = result["rates"]
    patterns = result["patterns"]

    print(f"RSI Lifecycle Mining — {days}d window, {result['total_events']} events")
    print("─" * 60)

    # Summary.
    print(f"  evolved={counts['evolved']}  rejected={counts['rejected']}  "
          f"rolled_back={counts['rolled_back']}  confirmed={counts['confirmed']}")
    print(f"  rates: accept={rates['accept_rate']}  confirm={rates['confirm_rate']}  "
          f"rollback={rates['rollback_rate']}")
    if counts["genesis_new"]:
        print(f"  new skills: {counts['genesis_new']}  tool_gap_paired: {counts['tool_gap_paired']}")
    print()

    # Thrashers.
    if patterns["thrashers"]:
        print(f"  ⚠️  THRASHERS ({len(patterns['thrashers'])} skills — evolve→rollback cycles ≥2):")
        for t in patterns["thrashers"][:5]:
            print(f"      {t['skill']}: {t['cycles']} cycles ({t['events']} events)")
        print()
    else:
        print("  ✅ thrash: none (no skill is spinning)")
        print()

    # Stuck-reject.
    if patterns["stuck_reject"]:
        print(f"  ⚠️  STUCK-REJECT ({len(patterns['stuck_reject'])} skills — 3+ rejects, 0 evolves):")
        for s in patterns["stuck_reject"][:5]:
            print(f"      {s['skill']}: {s['rejects']} rejects — {s['top_reason']}")
        print()
    else:
        print("  ✅ stuck-reject: none")
        print()

    # Confirm gap.
    if patterns["confirm_gap"]:
        print(f"  ⚠️  CONFIRM-GAP ({len(patterns['confirm_gap'])} skills — evolved but never confirmed):")
        for s in patterns["confirm_gap"][:5]:
            print(f"      {s}")
        print()
    else:
        print("  ✅ confirm-gap: none")
        print()

    # High rollback rate.
    if patterns["high_rollback_rate"]:
        print(f"  ⚠️  HIGH-ROLLBACK-RATE ({len(patterns['high_rollback_rate'])} skills — >30% rollback):")
        for r in patterns["high_rollback_rate"][:5]:
            print(f"      {r['skill']}: {r['rate']:.0%} ({r['rolled_back']}/{r['evolved']+r['rolled_back']})")
        print()
    else:
        print("  ✅ high-rollback-rate: none")
        print()

    # Tool gaps.
    if result["tool_gaps"]:
        print("  TOOL-GAP clustering:")
        for tool, cnt in result["tool_gaps"][:5]:
            print(f"      {tool}: {cnt}×")
        print()

    # Reject reasons.
    if result["reject_reasons"]:
        print("  REJECT reasons (top 5):")
        for reason, cnt in result["reject_reasons"][:5]:
            print(f"      {cnt}× {reason}")

    print("─" * 60)

    # Overall assessment.
    issues = (len(patterns["thrashers"]) + len(patterns["stuck_reject"])
              + len(patterns["confirm_gap"]) + len(patterns["high_rollback_rate"]))
    if issues == 0:
        print("종합: HEALTHY — no quality anomalies detected in the event stream.")
    else:
        print(f"종합: {issues} anomaly cluster(s) detected — investigate patterns above.")


def main() -> int:
    ap = argparse.ArgumentParser(description="RSI lifecycle log pattern miner")
    ap.add_argument("--data-dir", default=os.path.expanduser("~/.deneb/data"),
                    help="genesis data directory (default: ~/.deneb/data)")
    ap.add_argument("--days", type=int, default=7, help="look-back window in days (default: 7)")
    ap.add_argument("--json", action="store_true", help="machine-readable JSON output")
    args = ap.parse_args()

    log_path = os.path.join(args.data_dir, "skill_genesis_log.jsonl")
    events = read_jsonl(log_path)

    if not events:
        msg = f"no lifecycle events in {log_path}"
        if args.json:
            print(json.dumps({"error": msg, "total_events": 0}))
        else:
            print(f"ℹ️  {msg}")
            print("    Run on the production gateway host (~/.deneb/data/) where genesis is active.")
        return 1

    window_ms = args.days * DAY_MS
    result = mine(events, window_ms)

    if args.json:
        # Counter objects aren't JSON-serializable; convert.
        result["tool_gaps"] = [list(x) for x in result["tool_gaps"]]
        result["reject_reasons"] = [list(x) for x in result["reject_reasons"]]
        print(json.dumps(result, ensure_ascii=False, indent=2))
    else:
        print_human(result)

    return 0


if __name__ == "__main__":
    sys.exit(main())
