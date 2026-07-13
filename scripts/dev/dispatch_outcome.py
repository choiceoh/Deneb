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
  rc 124 (timeout(1))        -> timeout    (session hit the wall clock)
  rc != 0                    -> failed     (session died)
  ahead > 0 or pr-state OPEN -> attempted  (work exists but is not landed —
                                            reprobed on later dispatch ticks,
                                            MERGED upgrades it to landed)
  otherwise                  -> declined   (clean exit, no commits: the agent
                                            exercised the contract's "do not
                                            land" clause)

Pick-lane companion: blocks_redispatch() — landed/attempted block redispatch;
declined/failed/timeout may retry; outcome-less markers block only until the
session abandon age (default = SESSION_TIMEOUT) so a crash before accounting
cannot starve the L4 drain forever.

The marker is rewritten atomically (tmp+rename); all fields are additive so
older markers without outcomes stay readable. Exit 0 even on unreadable
markers — outcome accounting must never break the dispatch lane.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path

TIMEOUT_RC = 124  # coreutils timeout(1) convention

# Outcomes that permanently (or until upgraded) consume a candidate slot.
# attempted stays blocked so an in-flight PR is not double-dispatched; later
# ticks may upgrade it to landed via --upgrade-only.
BLOCKING_OUTCOMES = frozenset({"landed", "attempted"})
# Terminal failures / clean declines: the candidate may be retried on a later
# tick (subject to the daily cap). Observed 2026-07-13: treating any marker
# file as permanent skip left declined/failed/timeout slots dead forever.
REDISPATCH_OUTCOMES = frozenset({"declined", "failed", "timeout"})
# Marker is written BEFORE the Claude session starts. A crash/kill before
# outcome accounting leaves no "outcome" field — block while the session
# wall-clock could still be running, then release so the L4 drain continues.
DEFAULT_ABANDON_AFTER_SEC = 7200  # matches coding-dispatch SESSION_TIMEOUT default


def blocks_redispatch(
    marker_path: str | Path,
    *,
    now_sec: float | None = None,
    abandon_after_sec: int = DEFAULT_ABANDON_AFTER_SEC,
) -> bool:
    """True when the pick lane must skip this candidate because of its marker.

    Decision table (load-bearing for L4 drain):
      no marker file                         -> False (pick it)
      outcome landed|attempted               -> True
      outcome declined|failed|timeout        -> False (retry allowed)
      missing/empty outcome, age < abandon   -> True  (likely in flight)
      missing/empty outcome, age >= abandon  -> False (abandoned session)
      unreadable marker                      -> True  (do not thrash)
    """
    path = Path(marker_path)
    if not path.is_file():
        return False
    try:
        marker = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return True
    outcome = (marker.get("outcome") or "").strip()
    if outcome in BLOCKING_OUTCOMES:
        return True
    if outcome in REDISPATCH_OUTCOMES:
        return False
    # No recorded outcome yet — age gate against the marker mtime (written at
    # dispatch start). Prefer mtime over createdAt: the marker body is a copy of
    # the candidate record whose createdAt is the queue timestamp, not dispatch.
    if now_sec is None:
        now_sec = time.time()
    try:
        age = now_sec - path.stat().st_mtime
    except OSError:
        return True
    return age < max(0, abandon_after_sec)


def decide(rc: int, ahead: int, pr_state: str) -> str:
    if pr_state == "MERGED":
        return "landed"
    if rc == TIMEOUT_RC:
        return "timeout"
    if rc != 0:
        return "failed"
    if ahead > 0 or pr_state == "OPEN":
        return "attempted"
    return "declined"


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--marker", required=True)
    ap.add_argument("--rc", type=int, required=True)
    ap.add_argument("--elapsed", type=int, default=0)
    ap.add_argument(
        "--ahead",
        default="",
        help="commits ahead of origin/main in the dispatch worktree ('' = unknown)",
    )
    ap.add_argument(
        "--pr-state",
        default="",
        help="gh PR state for the dispatch branch: MERGED|OPEN|CLOSED|'' (unknown)",
    )
    ap.add_argument(
        "--upgrade-only",
        action="store_true",
        help="reprobe mode: upgrade a non-terminal outcome to landed when the "
        "PR merged later; every other field is preserved, non-MERGED is a no-op",
    )
    args = ap.parse_args()

    path = Path(args.marker)
    try:
        marker = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as e:
        print(f"dispatch outcome: marker unreadable ({e}) — skipped", file=sys.stderr)
        return 0

    pr_state = args.pr_state.strip().upper()
    if args.upgrade_only:
        if pr_state != "MERGED":
            print(marker.get("outcome", ""))
            return 0
        marker["outcome"] = "landed"
        marker["outcomePrState"] = "MERGED"
        marker["outcomeAt"] = int(time.time() * 1000)
        write_marker(path, marker)
        print("landed")
        return 0

    try:
        ahead = int(args.ahead)
    except ValueError:
        ahead = 0
    outcome = decide(args.rc, ahead, pr_state)

    marker["outcome"] = outcome
    marker["outcomeRc"] = args.rc
    marker["outcomeElapsedSec"] = args.elapsed
    marker["outcomeAt"] = int(time.time() * 1000)
    if pr_state:
        marker["outcomePrState"] = pr_state

    write_marker(path, marker)
    print(outcome)
    return 0


def write_marker(path: Path, marker: dict) -> None:
    tmp = path.with_suffix(".json.tmp")
    tmp.write_text(json.dumps(marker, ensure_ascii=False) + "\n", encoding="utf-8")
    tmp.replace(path)


if __name__ == "__main__":
    sys.exit(main())
