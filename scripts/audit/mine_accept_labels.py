#!/usr/bin/env python3
"""Mine accept-side improvement labels from commit↔session provenance.

RSI P3 verifier co-evolution needs ground truth for "this change was genuinely
an improvement" (accept-side gold pairs — the open follow-up of the L1 judge
order-swap gate). This miner joins the two provenance ledgers to git/GitHub
reality and emits one labeled line per change:

  sources:
    ~/.deneb/commit-sessions.jsonl        (committer instrumentation, #4064)
    ~/.deneb/data/self_correction_candidates.jsonl  (genesis dispatch FSM,
        append-only — folded to the LAST record per candidate id)

  labels:
    accepted    merged to main, survived --min-days with no revert
    reverted    merged, then a revert referencing its PR landed on main
    pending     merged but younger than --min-days
    rejected    PR closed without merging
    in-flight   PR still open (or branch exists, no PR resolution yet)
    unknown     not resolvable (no branch/PR, or gh unavailable)

Advisory and read-only over the ledgers; network (gh) only from main().
Usage:
  scripts/audit/mine_accept_labels.py [--out ~/.deneb/accept-labels.jsonl]
      [--min-days 3] [--repo <git-checkout>]
"""

from __future__ import annotations

import argparse
import json
import pathlib
import subprocess
import time


def load_jsonl(path: pathlib.Path) -> list[dict]:
    if not path.exists():
        return []
    out = []
    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            out.append(json.loads(line))
        except json.JSONDecodeError:
            continue
    return out


def fold_candidates(records: list[dict]) -> list[dict]:
    """Append-only candidate ledger → last state per id, keeping only records
    that ever reached a commit/branch (the dispatch lane's provenance)."""
    last: dict[str, dict] = {}
    for r in records:
        cid = r.get("id", "")
        if not cid:
            continue
        merged = dict(last.get(cid, {}))
        # Later lines are partial updates; a field absent in the update must
        # not erase what an earlier line established.
        for k, v in r.items():
            if v not in (None, "", 0):
                merged[k] = v
        last[cid] = merged
    return [r for r in last.values() if r.get("branch") or r.get("commitSha")]


def decide_label(
    pr_state: str,
    merged_at_ms: int,
    reverted: bool,
    now_ms: int,
    min_days: int,
) -> str:
    """Pure labeling rule — the unit-tested contract."""
    if pr_state == "MERGED":
        if reverted:
            return "reverted"
        if now_ms - merged_at_ms >= min_days * 86_400_000:
            return "accepted"
        return "pending"
    if pr_state == "CLOSED":
        return "rejected"
    if pr_state == "OPEN":
        return "in-flight"
    return "unknown"


def gh_pr_for_branch(branch: str, cache: dict[str, dict]) -> dict:
    if branch in cache:
        return cache[branch]
    info: dict = {}
    try:
        proc = subprocess.run(
            [
                "gh", "pr", "list", "--head", branch, "--state", "all",
                "--limit", "1", "--json", "number,state,mergedAt",
            ],
            capture_output=True, text=True, timeout=30, check=True,
        )
        rows = json.loads(proc.stdout or "[]")
        if rows:
            info = rows[0]
    except (subprocess.SubprocessError, OSError, json.JSONDecodeError):
        info = {}
    cache[branch] = info
    return info


def revert_landed(repo: pathlib.Path, pr_number: int) -> bool:
    if not pr_number:
        return False
    try:
        proc = subprocess.run(
            [
                "git", "-C", str(repo), "log", "origin/main",
                "--grep", f"#{pr_number}", "--grep", "Revert", "--all-match",
                "--oneline", "-1",
            ],
            capture_output=True, text=True, timeout=30, check=True,
        )
        return bool(proc.stdout.strip())
    except (subprocess.SubprocessError, OSError):
        return False


def iso_to_ms(iso: str) -> int:
    if not iso:
        return 0
    try:
        return int(
            time.mktime(time.strptime(iso[:19], "%Y-%m-%dT%H:%M:%S")) * 1000
        )
    except ValueError:
        return 0


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    home = pathlib.Path.home()
    ap.add_argument("--commit-ledger", default=str(home / ".deneb/commit-sessions.jsonl"))
    ap.add_argument(
        "--self-correction", default=str(home / ".deneb/data/self_correction_candidates.jsonl")
    )
    ap.add_argument("--repo", default=".", help="git checkout with origin/main fetched")
    ap.add_argument("--out", default=str(home / ".deneb/accept-labels.jsonl"))
    ap.add_argument("--min-days", type=int, default=3)
    args = ap.parse_args()

    repo = pathlib.Path(args.repo)
    now_ms = int(time.time() * 1000)
    entries: list[dict] = []
    for row in load_jsonl(pathlib.Path(args.commit_ledger).expanduser()):
        entries.append(
            {
                "source": "committer",
                "session": row.get("session", ""),
                "sha": row.get("sha", ""),
                "branch": row.get("branch", ""),
                "at": row.get("at", 0),
            }
        )
    for row in fold_candidates(load_jsonl(pathlib.Path(args.self_correction).expanduser())):
        entries.append(
            {
                "source": "self-correction",
                "session": row.get("sessionKey", ""),
                "sha": row.get("commitSha", ""),
                "branch": row.get("branch", ""),
                "pr": row.get("prNumber", 0),
            }
        )

    cache: dict[str, dict] = {}
    counts: dict[str, int] = {}
    labeled = []
    for e in entries:
        pr_number = int(e.get("pr") or 0)
        pr_state, merged_ms = "", 0
        if not pr_number and e.get("branch"):
            info = gh_pr_for_branch(e["branch"], cache)
            pr_number = int(info.get("number") or 0)
            pr_state = info.get("state", "")
            merged_ms = iso_to_ms(info.get("mergedAt") or "")
        elif pr_number:
            info = gh_pr_for_branch(e["branch"], cache) if e.get("branch") else {}
            pr_state = info.get("state", "MERGED")
            merged_ms = iso_to_ms(info.get("mergedAt") or "")
        reverted = revert_landed(repo, pr_number) if pr_state == "MERGED" else False
        label = decide_label(pr_state, merged_ms, reverted, now_ms, args.min_days)
        labeled.append({**e, "pr": pr_number, "pr_state": pr_state, "label": label})
        counts[label] = counts.get(label, 0) + 1

    out = pathlib.Path(args.out).expanduser()
    with out.open("w", encoding="utf-8") as f:
        for row in labeled:
            f.write(json.dumps(row, ensure_ascii=False) + "\n")
    print(f"labeled {len(labeled)} entries -> {out}  {counts}")


if __name__ == "__main__":
    main()
