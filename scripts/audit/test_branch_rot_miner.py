"""Deterministic tests for the worktrunk branch-rot miner.

Load-bearing assertions: only aged, ahead, non-harness branches become
candidates; worktrunk's trees-match integration detection splits retire vs
recover flavors; ordering is oldest-first; the shared reopen/cap edge from the
health miner applies (a live twin blocks re-filing without spending the cap);
and the CLI dry-runs end-to-end from an injected ``--wt-json`` snapshot.
"""

from __future__ import annotations

import io
import json
import os
import tempfile
import subprocess
import pathlib
import unittest

from branch_rot_miner import (
    squash_landed_commit,
    DEFAULT_MIN_AGE_DAYS,
    SOURCE_PREFIX,
    main,
    rot_candidates,
)
from health_finding_miner import select_candidates

NOW_MS = 1_784_500_000_000  # 2026-07-20ish, fixed for determinism
DAY_MS = 86_400_000


def _iso(age_days: float) -> str:
    import datetime as dt

    stamp = dt.datetime.fromtimestamp(
        (NOW_MS - age_days * DAY_MS) / 1000.0, tz=dt.timezone.utc
    )
    return stamp.strftime("%Y-%m-%dT%H:%M:%SZ")


def row(
    branch: str,
    *,
    ahead: int = 3,
    behind: int = 7,
    age_days: float = 21.0,
    integrated: bool = False,
    current: bool = False,
    dirty: bool = False,
    added: int = 120,
    deleted: int = 30,
    summary: str = "",
) -> dict:
    payload = {
        "branch": branch,
        "default_branch": {
            "ahead": ahead,
            "behind": behind,
            "diff": {"added": added, "deleted": deleted},
            "integration": {"reason": "trees_match" if integrated else "commits_ahead"},
        },
        "display": {"state": "integrated" if integrated else "active"},
        "head": {
            "committed_at": _iso(age_days),
            "short_sha": "abc1234",
            "subject": f"work on {branch}",
        },
        "worktree": {
            "current": current,
            "detached": False,
            "path": f"/repo/.worktrees/{branch}",
            "changes": {"modified": dirty},
        },
    }
    if summary:
        payload["summary"] = summary
    return payload


class RotCandidateTests(unittest.TestCase):
    def mine(self, rows: list[dict], **kwargs) -> list[dict]:
        defaults = {
            "checkout": "~/deneb-dev",
            "now_ms": NOW_MS,
            "min_age_days": float(DEFAULT_MIN_AGE_DAYS),
            "open_prs": None,
        }
        defaults.update(kwargs)
        return rot_candidates({"worktrees": rows}, **defaults)

    def test_filters_harness_main_current_fresh_and_not_ahead(self) -> None:
        rows = [
            row("zcode/abc123"),
            row("cursor/session-1"),
            row("dispatch/sc-1-2-3"),
            row("main"),
            row("operator-live", current=True),
            row("fresh-branch", age_days=3.0),
            row("behind-only", ahead=0),
            row("real-rot"),
        ]
        got = self.mine(rows)
        self.assertEqual([c["source"] for c in got], [f"{SOURCE_PREFIX}:real-rot"])

    def test_integrated_becomes_retire_and_unmerged_recover(self) -> None:
        got = self.mine([row("done-branch", integrated=True), row("live-work")])
        by_source = {c["source"]: c for c in got}
        retire = by_source[f"{SOURCE_PREFIX}:done-branch"]
        recover = by_source[f"{SOURCE_PREFIX}:live-work"]
        self.assertIn("retire", retire["title"])
        self.assertIn("trees_match", retire["proposedChange"])
        self.assertIn("recover", recover["title"])
        self.assertIn("표준 랜딩 절차", recover["proposedChange"])
        for cand in got:
            self.assertEqual(cand["scope"], "code")
            self.assertEqual(cand["targetFiles"], [])
            self.assertIn("ahead=3", cand["evidence"])

    def test_prose_never_names_the_landing_script(self) -> None:
        """Dispatch-time ForbiddenSurfaceMentions scans every text field, and one
        mention of an acceptance-machinery component makes the candidate
        permanently undispatchable. This lane graduated onto the allowlist
        2026-07-20 and then landed nothing for two weeks because the risk note
        said "pr.sh" (found 2026-08-02) — a lane that looks alive with zero
        possible output. Pin the exact regression."""
        for cand in self.mine([row("done-branch", integrated=True), row("live-work")]):
            for field in ("title", "candidate", "evidence", "reason", "proposedChange", "risk"):
                self.assertNotIn("pr.sh", str(cand.get(field) or ""),
                                 f"{cand['source']}:{field} names the landing script")

    def test_oldest_first_and_summary_in_evidence(self) -> None:
        got = self.mine([
            row("young-rot", age_days=15.0),
            row("old-rot", age_days=40.0, summary="adds retry backoff"),
        ])
        self.assertEqual(
            [c["source"] for c in got],
            [f"{SOURCE_PREFIX}:old-rot", f"{SOURCE_PREFIX}:young-rot"],
        )
        self.assertIn("summary: adds retry backoff", got[0]["evidence"])

    def test_open_pr_branches_are_in_flight_not_rot(self) -> None:
        got = self.mine(
            [row("has-pr"), row("no-pr")], open_prs={"has-pr"})
        self.assertEqual([c["source"] for c in got], [f"{SOURCE_PREFIX}:no-pr"])

    def test_live_twin_blocks_without_spending_cap(self) -> None:
        candidates = self.mine([
            row("oldest", age_days=50.0),
            row("middle", age_days=30.0),
            row("newest-rot", age_days=20.0),
        ])
        existing = [{
            "source": f"{SOURCE_PREFIX}:oldest",
            "status": "proposed",
            "createdAt": NOW_MS - DAY_MS,
            "id": "sc-1",
        }]
        selected, skipped = select_candidates(candidates, existing, NOW_MS, 2)
        self.assertEqual(
            [c["source"] for c in selected],
            [f"{SOURCE_PREFIX}:middle", f"{SOURCE_PREFIX}:newest-rot"],
        )
        self.assertEqual(len(skipped), 1)
        self.assertIn("proposed twin", skipped[0][1])


class CliTests(unittest.TestCase):
    def test_dry_run_from_injected_snapshot_without_gateway(self) -> None:
        snapshot = {"worktrees": [row("cli-rot", age_days=30.0)]}
        with tempfile.NamedTemporaryFile(
            "w", suffix=".json", delete=False, encoding="utf-8"
        ) as handle:
            json.dump(snapshot, handle)
            path = handle.name
        try:
            out, err = io.StringIO(), io.StringIO()
            rc = main(
                [
                    "--wt-json", path,
                    "--skip-pr-check",
                    "--dry-run",
                    "--url", "http://127.0.0.1:1",  # unreachable: dry-run continues
                    "--token", "t",
                ],
                stdout=out,
                stderr=err,
            )
            self.assertEqual(rc, 0)
            self.assertIn(f"DRY-RUN would file {SOURCE_PREFIX}:cli-rot", out.getvalue())
            self.assertIn("1 selected, 0 filed", out.getvalue())
        finally:
            os.unlink(path)


if __name__ == "__main__":
    unittest.main()


class SquashLandingDetectionTest(unittest.TestCase):
    """squash_landed_commit against a real throwaway repo — patch-id math cannot
    be exercised with synthetic rows, and this exact blind spot cost three full
    coding sessions on 2026-08-02 (every live candidate was already squash-landed)."""

    def _git(self, *args):
        subprocess.run(["git", "-C", self.repo, *args], check=True,
                       capture_output=True, text=True)

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.repo = self.tmp.name
        self.addCleanup(self.tmp.cleanup)
        self._git("init", "-q", "-b", "main")
        self._git("config", "user.email", "t@t")
        self._git("config", "user.name", "t")
        pathlib.Path(self.repo, "a.txt").write_text("base\n")
        self._git("add", ".")
        self._git("commit", "-qm", "base")
        # feature branch with two commits
        self._git("checkout", "-qb", "feature")
        pathlib.Path(self.repo, "a.txt").write_text("base\nchange1\n")
        self._git("commit", "-aqm", "c1")
        pathlib.Path(self.repo, "b.txt").write_text("new file\n")
        self._git("add", ".")
        self._git("commit", "-qm", "c2")
        # squash-merge onto main (single commit, different sha, same aggregate diff)
        self._git("checkout", "-q", "main")
        self._git("merge", "--squash", "-q", "feature")
        self._git("commit", "-qm", "feat: squashed landing")
        # the miner compares against origin/main
        self._git("update-ref", "refs/remotes/origin/main", "main")

    def test_detects_squash_landed_branch(self):
        got = squash_landed_commit(self.repo, "feature")
        self.assertIsNotNone(got, "a squash-landed branch must be detected")

    def test_unlanded_branch_is_not_claimed(self):
        # extend the feature past the landing: aggregate diff no longer matches
        self._git("checkout", "-q", "feature")
        pathlib.Path(self.repo, "c.txt").write_text("unlanded\n")
        self._git("add", ".")
        self._git("commit", "-qm", "c3-unlanded")
        self._git("checkout", "-q", "main")
        self.assertIsNone(squash_landed_commit(self.repo, "feature"),
                          "real unmerged work must stay a recover candidate")
