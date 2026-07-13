#!/usr/bin/env python3
"""Record the OUTCOME of one L4 coding-dispatch session onto its marker.

RSI graduation-ladder evidence (roadmap P5): raising the daily dispatch cap
requires "N dispatches with 0 deploy-watch rollbacks and >=50% land rate" —
but until now nothing measured land rate: the marker recorded only that a
dispatch HAPPENED. This module folds the session's observable outcome back
into the marker (the dispatch ledger), so the ladder evidence accumulates
from the very first dispatch.

Decision table — deterministic, from facts the shell gathers (a PR probe and
the worktree's commit state), never from parsing session text:

  pr-state MERGED            -> landed     (the contract's success terminal)
  ahead > 0 or pr-state OPEN -> attempted  (work exists / PR in flight —
                                            checked BEFORE rc so a timeout
                                            after opening a PR stays reprobeable)
  rc 124 (timeout(1))        -> timeout    (session hit the wall clock)
  rc != 0                    -> failed     (session died)
  otherwise                  -> declined   (clean exit, no commits)

When --ahead is '' (unknown) and --pr-state is empty, refuse to guess: leave
the marker without an outcome rather than recording a false declined.

Pick-lane companion: blocks_redispatch() — landed/attempted block redispatch;
declined/failed/timeout may retry; outcome-less markers block only until the
session abandon age (default = SESSION_TIMEOUT).

The marker is rewritten atomically (tmp+rename); all fields are additive so
older markers without outcomes stay readable. Exit 0 even on unreadable
markers or write failures — outcome accounting must never break the lane.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path

TIMEOUT_RC = 124  # coreutils timeout(1) convention

BLOCKING_OUTCOMES = frozenset({"landed", "attempted"})
REDISPATCH_OUTCOMES = frozenset({"declined", "failed", "timeout"})
DEFAULT_ABANDON_AFTER_SEC = 7200
ACTIVE_LEDGER_PHASES = frozenset({"started", "pr_opened", "merged", "deployed", "watch_passed"})
RETRYABLE_LEDGER_PHASES = frozenset({"failed", "rolled_back"})


def blocks_redispatch(
    marker_path: str | Path,
    *,
    now_sec: float | None = None,
    abandon_after_sec: int = DEFAULT_ABANDON_AFTER_SEC,
    authoritative_phase: str = "",
) -> bool:
    """True when the pick lane must skip this candidate because of its marker."""
    phase = authoritative_phase.strip().lower()
    if phase in ACTIVE_LEDGER_PHASES:
        return True
    if phase == "rolled_back":
        return False
    path = Path(marker_path)
    if not path.is_file():
        return False
    try:
        raw = path.read_text(encoding="utf-8", errors="replace")
        marker = json.loads(raw)
    except (OSError, json.JSONDecodeError, TypeError, ValueError):
        return True
    if not isinstance(marker, dict):
        return True
    outcome_raw = marker.get("outcome")
    outcome = outcome_raw.strip() if isinstance(outcome_raw, str) else ""
    if outcome in BLOCKING_OUTCOMES:
        return True
    if outcome in REDISPATCH_OUTCOMES:
        return False
    if now_sec is None:
        now_sec = time.time()
    try:
        age = now_sec - path.stat().st_mtime
    except OSError:
        return True
    return age < max(0, abandon_after_sec)


def decide(rc: int, ahead: int, pr_state: str) -> str:
    """Map observables to an outcome. Caller must not pass unknown ahead as 0."""
    if pr_state == "MERGED":
        return "landed"
    # OPEN / unpushed commits win over session rc: a timeout after opening a PR
    # must stay reprobeable (upgrade-only scans attempted markers).
    if ahead > 0 or pr_state == "OPEN":
        return "attempted"
    if rc == TIMEOUT_RC:
        return "timeout"
    if rc != 0:
        return "failed"
    return "declined"


def write_marker(path: Path, marker: dict, *, mtime_sec: float | None = None) -> bool:
    """Atomic rewrite. Returns False on I/O failure (never raises)."""
    try:
        tmp = path.with_suffix(".json.tmp")
        tmp.write_text(json.dumps(marker, ensure_ascii=False) + "\n", encoding="utf-8")
        tmp.replace(path)
        if mtime_sec is not None:
            try:
                os_utime = __import__("os").utime
                os_utime(path, (mtime_sec, mtime_sec))
            except OSError:
                pass
        return True
    except OSError as e:
        print(f"dispatch outcome: marker write failed ({e}) — skipped", file=sys.stderr)
        return False


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--marker", required=True)
    ap.add_argument("--rc", type=int, required=True)
    ap.add_argument("--elapsed", type=int, default=0)
    ap.add_argument(
        "--ahead",
        default="",
        help="commits ahead of origin/main ('' = unknown — do not guess declined)",
    )
    ap.add_argument(
        "--pr-state",
        default="",
        help="gh PR state: MERGED|OPEN|CLOSED|'' (unknown)",
    )
    ap.add_argument(
        "--upgrade-only",
        action="store_true",
        help="reprobe mode: upgrade to landed on MERGED; preserve marker mtime "
        "so daily-cap accounting is not burned by late merges",
    )
    ap.add_argument(
        "--preserve-mtime",
        action="store_true",
        help="keep the marker's prior mtime after rewrite (used by reprobe)",
    )
    args = ap.parse_args()

    path = Path(args.marker)
    try:
        marker = json.loads(path.read_text(encoding="utf-8", errors="replace"))
    except (OSError, json.JSONDecodeError) as e:
        print(f"dispatch outcome: marker unreadable ({e}) — skipped", file=sys.stderr)
        return 0
    if not isinstance(marker, dict):
        print("dispatch outcome: marker not an object — skipped", file=sys.stderr)
        return 0

    prior_mtime = None
    if args.preserve_mtime or args.upgrade_only:
        try:
            prior_mtime = path.stat().st_mtime
        except OSError:
            prior_mtime = None

    pr_state = args.pr_state.strip().upper()
    if args.upgrade_only:
        if pr_state != "MERGED":
            print(marker.get("outcome", "") if isinstance(marker.get("outcome"), str) else "")
            return 0
        marker["outcome"] = "landed"
        marker["outcomePrState"] = "MERGED"
        marker["outcomeAt"] = int(time.time() * 1000)
        write_marker(path, marker, mtime_sec=prior_mtime)
        print("landed")
        return 0

    ahead_raw = args.ahead.strip()
    if ahead_raw == "" and not pr_state:
        # Unknown facts — do not invent declined (bot review #3609).
        print("unknown", file=sys.stderr)
        print("")
        return 0
    try:
        ahead = int(ahead_raw) if ahead_raw != "" else 0
    except ValueError:
        ahead = 0

    outcome = decide(args.rc, ahead, pr_state)
    marker["outcome"] = outcome
    marker["outcomeRc"] = args.rc
    marker["outcomeElapsedSec"] = args.elapsed
    marker["outcomeAt"] = int(time.time() * 1000)
    if pr_state:
        marker["outcomePrState"] = pr_state

    write_marker(path, marker, mtime_sec=prior_mtime if args.preserve_mtime else None)
    print(outcome)
    return 0


if __name__ == "__main__":
    sys.exit(main())
