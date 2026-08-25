"""Branch-rot miner — worktrunk fleet sensor filing L4 recovery candidates.

Parallel-agent development leaves a trail: branches with real work sitting
ahead of main for weeks, their worktrees parked under ``.worktrees/`` with
nobody deciding whether to land or retire them (observed 2026-07-20: 25 of 27
dev-checkout worktrees ahead of main, all ~3 weeks stale). No lane watched
that decay. This miner is the worktrunk-powered proactive source: it reads
``wt list --format json`` — branch↔worktree↔dirty-state↔integration facts in
one deterministic snapshot, including wt's trees-match detection that
separates "content already in main, just delete" from "unrecovered work" —
and files each rotten branch as a propose-only, scope=code self-correction
candidate through the existing review lane.

Design decisions (mirrors ``health_finding_miner.py``, the P5-ws3 template):

  - Scripts-side miner over the miniapp RPC, NOT a gateway PeriodicTask: the
    input is a git checkout outside the serving process.
  - Single-writer queue invariant: writes go through
    ``miniapp.self_improvement_coding.record``; dedup reads use ``.list``.
    The RPC/reopen/cap edge is imported from the health miner so the miners
    cannot drift.
  - Source namespace ``branch-rot`` starts Staged (file-for-review, no
    auto-dispatch); graduation is delegated to LadderWatchTask per the ladder
    policy in docs/agent-rules/self-improvement.md.
  - Candidate identity is the branch name (``branch-rot:<branch>``): stable
    while the branch exists, which is what makes reopen semantics meaningful.
    No impactContract in v1 — the branch disappearing is the observable
    outcome and the review lane sees it directly.

Two candidate flavors, split by worktrunk's integration detection:

  - ``retire``: wt reports the branch tree content already integrated into
    the default branch (``trees_match``) — a verify-then-delete cleanup.
  - ``recover``: real unmerged work — rebase, then land or retire with a
    recorded rationale.

Harness-managed namespaces (zcode/, cursor/, codex/, claude/, dispatch/) are
excluded: their lifecycles belong to their own reapers.

stdlib-only and importable for deterministic tests; the CLI is
``scripts/audit/branch-rot-miner.py``. Sensor input can be injected with
``--wt-json`` (same pattern as the health miner's ``--report``).
"""

from __future__ import annotations

import argparse
import datetime as _dt
import json
import os
import subprocess
import sys
import time
from typing import Any, TextIO

from health_finding_miner import (
    GatewayError,
    fetch_existing,
    record_candidate,
    select_candidates,
)

SOURCE_PREFIX = "branch-rot"
DEFAULT_CHECKOUT = "~/deneb-dev"
DEFAULT_GATEWAY_URL = "http://127.0.0.1:18789"
DEFAULT_MIN_AGE_DAYS = 14
MAX_PER_RUN = 3
WT_TIMEOUT_SEC = 300

EXCLUDED_BRANCH_PREFIXES = ("zcode/", "cursor/", "codex/", "claude/", "dispatch/")
EXCLUDED_BRANCHES = ("main", "master")

# Candidate prose must never name acceptance-machinery components (the landing
# script, CI gate files, …): dispatch-time ForbiddenSurfaceMentions scans every
# text field, and one mention makes the candidate permanently undispatchable.
# That is exactly what happened to this lane — it graduated onto the dispatch
# allowlist 2026-07-20 and then landed NOTHING, because this note said
# "pr.sh" and every mined candidate was silently surface-blocked (found
# 2026-08-02). Describe the flow generically; the dispatch session prompt
# already names the concrete landing procedure.
_RISK_NOTE = (
    "브랜치 회수는 해당 낡은 브랜치와 그 워크트리만 건드립니다. main은 오직 "
    "게이트가 그린인 PR을 레포 표준 랜딩 절차로 반영할 때만 움직입니다."
)


def run_wt_list(checkout: str, wt_bin: str, full: bool, err: TextIO) -> dict[str, Any]:
    """Run ``wt list --format json`` for the checkout; raise on failure.

    ``--full`` adds the cached LLM branch summaries (worth having for review
    quality); when it fails — offline, no gh — retry plain rather than losing
    the run.
    """
    base = [wt_bin, "-C", os.path.expanduser(checkout), "list", "--format", "json"]
    cmds = ([base + ["--full", "--no-progressive"], base] if full else [base])
    last_error = "wt list produced no output"
    for cmd in cmds:
        try:
            proc = subprocess.run(
                cmd, capture_output=True, text=True, timeout=WT_TIMEOUT_SEC, check=False
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            last_error = str(exc)
            continue
        if proc.returncode != 0:
            last_error = (proc.stderr or "").strip() or f"wt exited {proc.returncode}"
            continue
        try:
            return json.loads(proc.stdout)
        except ValueError as exc:
            last_error = f"unparseable wt JSON: {exc}"
    raise RuntimeError(f"wt list failed for {checkout}: {last_error}")


def _rows(sensor: Any) -> list[dict[str, Any]]:
    if isinstance(sensor, list):
        return [r for r in sensor if isinstance(r, dict)]
    if isinstance(sensor, dict):
        for key in ("worktrees", "items", "rows"):
            if isinstance(sensor.get(key), list):
                return [r for r in sensor[key] if isinstance(r, dict)]
    return []


def _age_days(row: dict[str, Any], now_ms: int) -> float | None:
    stamp = ((row.get("head") or {}).get("committed_at")) or ""
    try:
        committed = _dt.datetime.fromisoformat(str(stamp).replace("Z", "+00:00"))
    except ValueError:
        return None
    return (now_ms / 1000.0 - committed.timestamp()) / 86400.0


def _is_integrated(row: dict[str, Any]) -> bool:
    if (row.get("display") or {}).get("state") == "integrated":
        return True
    integration = ((row.get("default_branch") or {}).get("integration")) or {}
    return integration.get("reason") == "trees_match"


def _is_dirty(row: dict[str, Any]) -> bool:
    changes = ((row.get("worktree") or {}).get("changes")) or {}
    return any(
        bool(changes.get(flag))
        for flag in ("staged", "modified", "untracked", "deleted", "renamed", "conflicted")
    )


def open_pr_branches(checkout: str, gh_bin: str, err: TextIO) -> set[str] | None:
    """Branches with an open PR (in-flight, not rot). None = unknown (gh failed)."""
    try:
        proc = subprocess.run(
            [gh_bin, "pr", "list", "--state", "open", "--json", "headRefName",
             "--limit", "200"],
            capture_output=True, text=True, timeout=60, check=False,
            cwd=os.path.expanduser(checkout),
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        print(f"gh pr list unavailable — PR filter skipped: {exc}", file=err)
        return None
    if proc.returncode != 0:
        print(
            f"gh pr list failed — PR filter skipped: {(proc.stderr or '').strip()}",
            file=err,
        )
        return None
    try:
        return {str(r.get("headRefName") or "") for r in json.loads(proc.stdout)}
    except ValueError:
        return None


def squash_landed_commit(checkout: str, branch: str, scan_cap: int = 300) -> str | None:
    """The main commit whose diff patch-id equals the branch's aggregate diff, or None.

    The wt sensor's integrated flag is trees_match, which misses SQUASH landings:
    a branch whose PR was squash-merged keeps distinct commit shas forever, so it
    kept classifying as "recover" and each one burned a full coding session just
    to discover there was nothing left to recover (2026-08-02: all three live
    branch-rot candidates declined for exactly this — #2897, #2890, filestore).
    Aggregate patch-id is the same test those sessions ran, done here for free.

    Bounded: only main commits touching the branch's own files are scanned, and
    at most scan_cap of them. Any git failure reads as "not landed" — the miner
    then files a recover candidate, which is the pre-existing (safe, just more
    expensive) behaviour.
    """

    def _git(*args: str) -> str | None:
        proc = subprocess.run(
            ["git", "-C", checkout, *args],
            capture_output=True, text=True, timeout=WT_TIMEOUT_SEC, check=False,
        )
        return proc.stdout if proc.returncode == 0 else None

    def _patch_id(diff: str) -> str | None:
        proc = subprocess.run(
            ["git", "-C", checkout, "patch-id", "--stable"],
            input=diff, capture_output=True, text=True, timeout=WT_TIMEOUT_SEC, check=False,
        )
        if proc.returncode != 0 or not proc.stdout.strip():
            return None
        return proc.stdout.split()[0]

    try:
        base = (_git("merge-base", "origin/main", branch) or "").strip()
        if not base:
            return None
        diff = _git("diff", f"{base}..{branch}")
        if diff is None:
            return None
        if not diff.strip():
            return branch  # empty diff: trivially integrated
        agg = _patch_id(diff)
        if not agg:
            return None
        files = (_git("diff", "--name-only", f"{base}..{branch}") or "").split()
        if not files:
            return None
        commits = (_git("log", "origin/main", f"--max-count={scan_cap}", "--format=%H", "--", *files) or "").split()
        for commit in commits:
            shown = _git("show", commit)
            if shown and _patch_id(shown) == agg:
                return commit
    except (subprocess.TimeoutExpired, OSError):
        return None
    return None


def rot_candidates(
    sensor: Any,
    checkout: str,
    now_ms: int,
    min_age_days: float,
    open_prs: set[str] | None,
) -> list[dict[str, Any]]:
    """Rotten-branch candidates, oldest first (uncapped — dedup runs later)."""
    out: list[tuple[float, dict[str, Any]]] = []
    for row in _rows(sensor):
        branch = str(row.get("branch") or "")
        worktree = row.get("worktree") or {}
        if not branch or branch in EXCLUDED_BRANCHES:
            continue
        if branch.startswith(EXCLUDED_BRANCH_PREFIXES):
            continue
        if worktree.get("current") or worktree.get("detached"):
            continue
        ahead = int((row.get("default_branch") or {}).get("ahead") or 0)
        if ahead <= 0:
            continue
        age = _age_days(row, now_ms)
        if age is None or age < min_age_days:
            continue
        if open_prs is not None and branch in open_prs:
            continue
        behind = int((row.get("default_branch") or {}).get("behind") or 0)
        diff = ((row.get("default_branch") or {}).get("diff")) or {}
        head = row.get("head") or {}
        path = str(worktree.get("path") or "")
        integrated = _is_integrated(row)
        dirty = _is_dirty(row)
        summary = str(row.get("summary") or "").strip()
        squash_commit = None
        if not integrated:
            squash_commit = squash_landed_commit(checkout, branch)
            if squash_commit:
                integrated = True
        flavor = "retire" if integrated else "recover"
        facts = (
            f"{branch} [{flavor}] ahead={ahead} behind={behind} "
            f"+{diff.get('added', 0)} -{diff.get('deleted', 0)} age={age:.0f}d "
            f"dirty={'yes' if dirty else 'no'} path={path or '(no worktree)'} "
            f"head={head.get('short_sha', '?')} {str(head.get('subject') or '').strip()}"
        )
        if squash_commit and squash_commit != branch:
            facts += (
                f" | already landed on origin/main as {squash_commit[:12]} "
                f"(aggregate patch-id match — squash merge)"
            )
        if summary:
            facts += f" | summary: {summary}"
        if squash_commit and squash_commit != branch:
            proposed = (
                f"이 브랜치의 전체 diff는 이미 {squash_commit[:12]} 로 origin/main 에 "
                f"반영됐습니다(patch-id 동일 · 스쿼시 머지). 필요하면 "
                f"`git -C {checkout} diff origin/main...{branch}` 로 확인한 뒤 워크트리를 "
                f"제거하고 브랜치를 삭제한다 — 회수할 소스 변경이 남아 있지 않다."
            )
        elif integrated:
            proposed = (
                f"wt는 이 브랜치의 트리 내용이 이미 origin/main 에 통합됐다고 봅니다"
                f"(trees_match). `git -C {checkout} diff origin/main...{branch}` 로 "
                f"확인해 비어 있으면 워크트리를 제거하고 브랜치를 삭제하고, 비어 있지 "
                f"않으면 회수 대상으로 다룬다."
            )
        else:
            proposed = (
                f"이 브랜치의 거취를 정한다: (1) 워크트리에서 {branch} 를 origin/main "
                f"위로 리베이스한다. (2) 아직 쓸모 있고 일관되면 → 해당 레인 게이트를 "
                f"돌리고 표준 랜딩 절차로 PR을 통해 반영한다. (3) 대체됐거나 폐기 대상이면 "
                f"→ 워크트리와 브랜치를 삭제하고 이유를 남긴다. (4) 일부만 가치 있으면 "
                f"→ 그 부분만 새 브랜치로 체리픽하고 나머지는 정리한다. "
                f"어중간한 상태로 남겨 두지 않는다."
            )
        candidate = {
            "scope": "code",
            "skillName": "branch-rot",
            "title": f"방치된 브랜치 {flavor}: {branch} (+{ahead}, {age:.0f}일)",
            "candidate": (
                f"브랜치 '{branch}' 가 열린 PR 없이 {age:.0f}일 동안 main보다 "
                f"{ahead}커밋 앞서 있습니다 — {checkout} 에서 삭고 있는 작업입니다."
            ),
            "evidence": facts,
            "reason": (
                "worktrunk fleet sensor — unrecovered branches decay; recover or "
                "retire (RSI P5 demand generation)"
            ),
            "targetFiles": [],
            "proposedChange": proposed,
            "risk": _RISK_NOTE,
            "source": f"{SOURCE_PREFIX}:{branch}",
        }
        out.append((age, candidate))
    # Oldest first: age is the rot metric.
    out.sort(key=lambda pair: (-pair[0], pair[1]["source"]))
    return [candidate for _, candidate in out]


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--checkout", default=DEFAULT_CHECKOUT,
                        help="git checkout to scan (default: %(default)s)")
    parser.add_argument("--wt-json", default=None,
                        help="read the wt list JSON from this file instead of running wt")
    parser.add_argument("--wt-bin", default="wt")
    parser.add_argument("--gh-bin", default="gh")
    parser.add_argument("--url", default=DEFAULT_GATEWAY_URL)
    parser.add_argument("--token", default=None)
    parser.add_argument("--min-age-days", type=float, default=DEFAULT_MIN_AGE_DAYS)
    parser.add_argument("--cap", type=int, default=MAX_PER_RUN)
    parser.add_argument("--no-full", action="store_true",
                        help="skip --full (no LLM summaries / CI lookups)")
    parser.add_argument("--skip-pr-check", action="store_true",
                        help="do not filter branches with open PRs")
    parser.add_argument("--dry-run", action="store_true")
    return parser


def main(argv: list[str] | None = None, stdout: TextIO | None = None,
         stderr: TextIO | None = None) -> int:
    args = _parser().parse_args(argv)
    out = stdout or sys.stdout
    err = stderr or sys.stderr

    token = args.token
    if not token:
        token_file = os.path.expanduser("~/.deneb/client_token")
        if os.path.exists(token_file):
            with open(token_file, encoding="utf-8") as handle:
                token = handle.read().strip()

    try:
        if args.wt_json:
            with open(args.wt_json, encoding="utf-8") as handle:
                sensor = json.load(handle)
        else:
            sensor = run_wt_list(args.checkout, args.wt_bin, not args.no_full, err)
    except (OSError, ValueError, RuntimeError) as exc:
        print(f"worktrunk sensor unavailable: {exc}", file=err)
        return 1

    open_prs: set[str] | None = None
    if not args.skip_pr_check:
        open_prs = open_pr_branches(args.checkout, args.gh_bin, err)

    base_url = args.url.rstrip("/")
    now_ms = int(time.time() * 1000)
    try:
        existing = fetch_existing(base_url, token)
    except GatewayError as exc:
        if not args.dry_run:
            print(f"cannot read the candidate queue — refusing to file blind: {exc}",
                  file=err)
            return 1
        print(f"gateway unreachable — DRY-RUN continues WITHOUT dedup: {exc}", file=err)
        existing = []

    candidates = rot_candidates(
        sensor, args.checkout, now_ms, args.min_age_days, open_prs)
    selected, skipped = select_candidates(
        candidates, existing, now_ms, max(args.cap, 0))

    for cand, why in skipped:
        print(f"skip {cand['source']}: {why}", file=out)
    filed = 0
    for candidate in selected:
        if args.dry_run:
            print(f"DRY-RUN would file {candidate['source']}: {candidate['title']}",
                  file=out)
            continue
        try:
            cid = record_candidate(base_url, token, candidate)
        except GatewayError as exc:
            print(f"record failed for {candidate['source']}: {exc}", file=err)
            return 1
        filed += 1
        print(f"filed {candidate['source']} → {cid}", file=out)
    print(
        f"branch-rot: {len(candidates)} rotten, {len(selected)} selected, "
        f"{filed} filed, {len(skipped)} deduped",
        file=out,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
