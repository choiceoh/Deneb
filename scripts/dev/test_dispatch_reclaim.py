"""Safety tests for abandoned L4 dispatch reclamation."""

from __future__ import annotations

import io
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import mock

import dispatch_reclaim
from dispatch_reclaim import ReclaimFacts, decide_reclaim


class DispatchReclaimDecisionTest(unittest.TestCase):
    def base(self, **changes) -> ReclaimFacts:
        values = ReclaimFacts(
            old_enough=True,
            attempt_matches=True,
            ledger_phase="started",
            pr_probe_ok=True,
            git_probe_ok=True,
        ).__dict__ | changes
        return ReclaimFacts(**values)

    def test_clean_abandoned_started_attempt_is_reclaimable(self):
        self.assertTrue(decide_reclaim(self.base())[0])

    def test_open_or_merged_pr_is_never_reclaimed(self):
        for state in ("OPEN", "MERGED"):
            self.assertFalse(decide_reclaim(self.base(pr_state=state))[0], state)

    def test_dirty_ahead_or_unknown_probe_fails_closed(self):
        cases = (
            self.base(dirty=True),
            self.base(ahead=1),
            self.base(pr_probe_ok=False),
            self.base(git_probe_ok=False),
            self.base(attempt_matches=False),
        )
        for facts in cases:
            self.assertFalse(decide_reclaim(facts)[0], facts)

    def test_started_remote_branch_is_preserved_but_failed_attempt_can_retry(self):
        self.assertFalse(decide_reclaim(self.base(remote_branch=True))[0])
        self.assertTrue(decide_reclaim(self.base(ledger_phase="failed", remote_branch=True))[0])

    def test_authoritative_inflight_and_terminal_success_phases_block_reclaim(self):
        for phase in ("pr_opened", "merged", "deployed", "watch_passed"):
            self.assertFalse(decide_reclaim(self.base(ledger_phase=phase))[0], phase)

    def test_cli_rejects_path_traversal_identity_before_any_probe(self):
        with TemporaryDirectory() as td:
            root = Path(td)
            dispatch_dir = root / "markers"
            dispatch_dir.mkdir()
            marker = dispatch_dir / "safe.json"
            marker.write_text(
                '{"id":"safe","attemptId":"../../escape","branch":"dispatch/../../escape"}\n'
            )
            ledger = root / "ledger.json"
            ledger.write_text(
                '[{"id":"safe","attemptId":"../../escape","dispatchPhase":"started"}]\n'
            )
            out = io.StringIO()
            with (
                mock.patch("sys.stderr"),
                mock.patch.object(dispatch_reclaim, "pr_state", side_effect=AssertionError("probe ran")),
                redirect_stdout(out),
            ):
                rc = dispatch_reclaim.main([
                    "--dispatch-dir", str(dispatch_dir),
                    "--ledger-json", str(ledger),
                    "--prod-dir", str(root),
                    "--worktree-root", str(root / "worktrees"),
                    "--abandon-after", "0",
                    "--now", str(marker.stat().st_mtime + 1),
                ])
            self.assertEqual(rc, 0)
            self.assertEqual(out.getvalue(), "")

    def test_cli_accepts_legacy_candidate_branch_after_authoritative_checks(self):
        with TemporaryDirectory() as td:
            root = Path(td)
            dispatch_dir = root / "markers"
            dispatch_dir.mkdir()
            marker = dispatch_dir / "safe.json"
            marker.write_text(
                '{"id":"safe","attemptId":"attempt-1","branch":"dispatch/safe"}\n'
            )
            ledger = root / "ledger.json"
            ledger.write_text(
                '[{"id":"safe","attemptId":"attempt-1","dispatchPhase":"started"}]\n'
            )
            out = io.StringIO()
            with (
                mock.patch.object(dispatch_reclaim, "pr_state", return_value=(True, "")),
                mock.patch.object(
                    dispatch_reclaim,
                    "git_facts",
                    return_value=(True, False, 0, False),
                ),
                redirect_stdout(out),
            ):
                rc = dispatch_reclaim.main([
                    "--dispatch-dir", str(dispatch_dir),
                    "--ledger-json", str(ledger),
                    "--prod-dir", str(root),
                    "--worktree-root", str(root / "worktrees"),
                    "--abandon-after", "0",
                    "--now", str(marker.stat().st_mtime + 1),
                ])
            self.assertEqual(rc, 0)
            self.assertEqual(out.getvalue(), "safe\tattempt-1\tstarted\tdispatch/safe\n")


if __name__ == "__main__":
    unittest.main()
