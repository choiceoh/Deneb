#!/usr/bin/env python3
"""Find abandoned L4 dispatch markers that are safe to reclaim.

The authoritative gateway ledger and GitHub PR state win over marker age. This
helper never deletes anything; it emits tab-separated reclaim candidates only
after every probe succeeds, leaving the shell lane to record ``failed`` before
removing a clean local worktree and marker.
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path


RECLAIMABLE_PHASES = frozenset({"started", "failed"})
ACTIVE_PR_STATES = frozenset({"OPEN", "MERGED"})
SAFE_IDENTITY = re.compile(r"[A-Za-z0-9._-]+")


@dataclass(frozen=True)
class ReclaimFacts:
    outcome: str = ""
    old_enough: bool = False
    attempt_matches: bool = False
    ledger_phase: str = ""
    pr_probe_ok: bool = False
    pr_state: str = ""
    git_probe_ok: bool = False
    dirty: bool = False
    ahead: int = 0
    remote_branch: bool = False


def decide_reclaim(facts: ReclaimFacts) -> tuple[bool, str]:
    if facts.outcome:
        return False, "marker already has an outcome"
    if not facts.old_enough:
        return False, "marker is still within the session timeout"
    if not facts.attempt_matches:
        return False, "marker does not match the authoritative attempt"
    if facts.ledger_phase not in RECLAIMABLE_PHASES:
        return False, f"authoritative phase {facts.ledger_phase or 'missing'} is not reclaimable"
    if not facts.pr_probe_ok:
        return False, "GitHub PR state is unavailable"
    if facts.pr_state in ACTIVE_PR_STATES:
        return False, f"PR is {facts.pr_state}"
    if not facts.git_probe_ok:
        return False, "git worktree/branch state is unavailable"
    if facts.dirty or facts.ahead > 0:
        return False, "local work is dirty or ahead of origin/main"
    if facts.remote_branch and facts.ledger_phase == "started":
        return False, "started attempt still has a remote branch"
    return True, "failed or abandoned clean attempt has no active PR"


def run(argv: list[str], cwd: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(argv, cwd=cwd, text=True, capture_output=True, check=False)


def pr_state(prod: Path, branch: str) -> tuple[bool, str]:
    proc = run([
        "gh", "pr", "list", "--head", branch, "--state", "all", "--limit", "1", "--json", "state",
    ], prod)
    if proc.returncode != 0:
        return False, ""
    try:
        rows = json.loads(proc.stdout or "[]")
    except json.JSONDecodeError:
        return False, ""
    if not isinstance(rows, list):
        return False, ""
    state = str(rows[0].get("state") or "").upper() if rows else ""
    return True, state


def git_facts(prod: Path, worktree: Path, branch: str) -> tuple[bool, bool, int, bool]:
    dirty = False
    ahead = 0
    if worktree.is_dir():
        status = run(["git", "status", "--porcelain"], worktree)
        count = run(["git", "rev-list", "--count", "origin/main..HEAD"], worktree)
        if status.returncode != 0 or count.returncode != 0:
            return False, False, 0, False
        dirty = bool(status.stdout.strip())
        try:
            ahead = int(count.stdout.strip())
        except ValueError:
            return False, False, 0, False
    else:
        ref = f"refs/heads/{branch}"
        exists = run(["git", "show-ref", "--verify", "--quiet", ref], prod)
        if exists.returncode == 0:
            count = run(["git", "rev-list", "--count", f"origin/main..{branch}"], prod)
            if count.returncode != 0:
                return False, False, 0, False
            try:
                ahead = int(count.stdout.strip())
            except ValueError:
                return False, False, 0, False
        elif exists.returncode != 1:
            return False, False, 0, False

    remote = run(["git", "ls-remote", "--exit-code", "--heads", "origin", f"refs/heads/{branch}"], prod)
    if remote.returncode not in (0, 2):
        return False, False, 0, False
    return True, dirty, ahead, remote.returncode == 0


def load_object(path: Path) -> dict:
    try:
        value = json.loads(path.read_text(encoding="utf-8", errors="replace"))
    except (OSError, json.JSONDecodeError):
        return {}
    return value if isinstance(value, dict) else {}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--dispatch-dir", required=True)
    parser.add_argument("--ledger-json", required=True)
    parser.add_argument("--prod-dir", required=True)
    parser.add_argument("--worktree-root", required=True)
    parser.add_argument("--abandon-after", type=int, required=True)
    parser.add_argument("--now", type=float, default=None)
    args = parser.parse_args(argv)

    dispatch_dir = Path(args.dispatch_dir)
    prod = Path(args.prod_dir)
    worktree_root = Path(args.worktree_root)
    now = args.now if args.now is not None else time.time()
    try:
        rows = json.loads(Path(args.ledger_json).read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        print(f"reclaim scan: ledger unavailable: {exc}", file=sys.stderr)
        return 1
    if not isinstance(rows, list):
        print("reclaim scan: ledger is not a list", file=sys.stderr)
        return 1
    ledger = {str(row.get("id") or ""): row for row in rows if isinstance(row, dict)}

    for marker_path in sorted(dispatch_dir.glob("*.json")):
        marker = load_object(marker_path)
        cid = str(marker.get("id") or marker_path.stem).strip()
        attempt = str(marker.get("attemptId") or "").strip()
        row = ledger.get(cid, {})
        branch = str(marker.get("branch") or row.get("branch") or f"dispatch/{cid}").strip()
        try:
            old_enough = now - marker_path.stat().st_mtime >= max(0, args.abandon_after)
        except OSError:
            continue
        outcome = str(marker.get("outcome") or "").strip()
        attempt_matches = bool(attempt) and attempt == str(row.get("attemptId") or "").strip()
        phase = str(row.get("dispatchPhase") or "").strip()
        if (
            not SAFE_IDENTITY.fullmatch(cid)
            or not SAFE_IDENTITY.fullmatch(attempt)
            or branch not in {f"dispatch/{attempt}", f"dispatch/{cid}"}
        ):
            print(f"reclaim scan: preserved {cid or '(missing id)'}: unsafe attempt identity", file=sys.stderr)
            continue
        preliminary = ReclaimFacts(
            outcome=outcome,
            old_enough=old_enough,
            attempt_matches=attempt_matches,
            ledger_phase=phase,
        )
        if outcome or not old_enough:
            continue
        if not attempt_matches or phase not in RECLAIMABLE_PHASES:
            _, reason = decide_reclaim(preliminary)
            print(f"reclaim scan: preserved {cid}: {reason}", file=sys.stderr)
            continue
        pr_ok, state = pr_state(prod, branch)
        worktree = worktree_root / f"dispatch-{attempt}"
        if not worktree.exists():
            worktree = worktree_root / f"dispatch-{cid}"  # legacy static path
        git_ok, dirty, ahead, remote = git_facts(prod, worktree, branch)
        facts = ReclaimFacts(
            outcome=outcome,
            old_enough=old_enough,
            attempt_matches=attempt_matches,
            ledger_phase=phase,
            pr_probe_ok=pr_ok,
            pr_state=state,
            git_probe_ok=git_ok,
            dirty=dirty,
            ahead=ahead,
            remote_branch=remote,
        )
        safe, reason = decide_reclaim(facts)
        if safe:
            print("\t".join((cid, attempt, facts.ledger_phase, branch)))
        elif old_enough and not facts.outcome:
            print(f"reclaim scan: preserved {cid}: {reason}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
