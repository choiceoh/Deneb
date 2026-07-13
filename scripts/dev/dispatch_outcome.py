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
