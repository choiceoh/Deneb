"""Deterministic tests for the L4 dispatch-outcome recorder.

Load-bearing behaviors: the decision table maps observable facts to one
outcome; the marker rewrite is additive and atomic; reprobe (--upgrade-only)
only ever upgrades to landed on a MERGED probe and preserves every other
field; an unreadable marker never breaks the lane (exit 0).
"""

from __future__ import annotations

import io
import json
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import mock

import dispatch_outcome


def run_main(argv: list[str]) -> tuple[int, str]:
    out = io.StringIO()
    with (
        mock.patch("sys.argv", ["dispatch_outcome.py", *argv]),
        redirect_stdout(out),
        redirect_stderr(io.StringIO()),
    ):
        rc = dispatch_outcome.main()
    return rc, out.getvalue().strip()


class DecisionTableTest(unittest.TestCase):
    def test_when_decide_outcome_from_rc_ahead_and_pr_state(self):
        cases = [
            # (rc, ahead, pr_state) -> outcome
            ((0, 0, "MERGED"), "landed"),
            ((1, 3, "MERGED"), "landed"),  # merged wins over a dirty exit
            ((124, 0, ""), "timeout"),
            ((1, 0, ""), "failed"),
            ((0, 2, ""), "attempted"),  # unpushed/unlanded commits
            ((0, 0, "OPEN"), "attempted"),  # PR still in flight
            # OPEN / ahead beat timeout so reprobe can still upgrade to landed
            ((124, 0, "OPEN"), "attempted"),
            ((124, 2, ""), "attempted"),
            ((0, 0, ""), "declined"),  # clean exit, no work: contract clause
            ((0, 0, "CLOSED"), "declined"),  # PR closed without merge, no work left
        ]
        for (rc, ahead, state), want in cases:
            self.assertEqual(
                dispatch_outcome.decide(rc, ahead, state), want, (rc, ahead, state)
            )

    def test_unknown_ahead_without_pr_skips_outcome(self):
        with TemporaryDirectory() as td:
            marker = Path(td) / "sc-u.json"
            before = {"id": "sc-u", "promptVersion": "x"}
            marker.write_text(json.dumps(before) + "\n")
            rc, printed = run_main(
                ["--marker", str(marker), "--rc", "0", "--ahead", "", "--pr-state", ""]
            )
            self.assertEqual(rc, 0)
            self.assertEqual(printed, "")
            self.assertEqual(json.loads(marker.read_text()), before)


class MarkerRewriteTest(unittest.TestCase):
    def test_preserves_existing_fields_when_recording_outcome(self):
        with TemporaryDirectory() as td:
            marker = Path(td) / "sc-1.json"
            marker.write_text(
                json.dumps({"id": "sc-1", "promptVersion": "abc123"}) + "\n"
            )
            rc, printed = run_main(
                ["--marker", str(marker), "--rc", "0", "--elapsed", "42",
                 "--ahead", "0", "--pr-state", "MERGED"]
            )
            self.assertEqual((rc, printed), (0, "landed"))
            rec = json.loads(marker.read_text())
            self.assertEqual(rec["outcome"], "landed")
            self.assertEqual(rec["outcomeElapsedSec"], 42)
            self.assertEqual(rec["outcomePrState"], "MERGED")
            self.assertEqual(rec["promptVersion"], "abc123", "existing fields kept")
            self.assertGreater(rec["outcomeAt"], 0)

    def test_upgrade_only_merged_upgrades_and_preserves(self):
        with TemporaryDirectory() as td:
            marker = Path(td) / "sc-2.json"
            marker.write_text(json.dumps({
                "id": "sc-2", "outcome": "attempted",
                "outcomeRc": 1, "outcomeElapsedSec": 900,
            }) + "\n")
            rc, printed = run_main(
                ["--marker", str(marker), "--rc", "0",
                 "--pr-state", "MERGED", "--upgrade-only"]
            )
            self.assertEqual((rc, printed), (0, "landed"))
            rec = json.loads(marker.read_text())
            self.assertEqual(rec["outcome"], "landed")
            self.assertEqual(rec["outcomeRc"], 1, "reprobe must not clobber rc")
            self.assertEqual(rec["outcomeElapsedSec"], 900, "reprobe must not clobber elapsed")

    def test_when_upgrade_only_non_merged_is_noop(self):
        with TemporaryDirectory() as td:
            marker = Path(td) / "sc-3.json"
            before = {"id": "sc-3", "outcome": "attempted", "outcomeAt": 7}
            marker.write_text(json.dumps(before) + "\n")
            rc, printed = run_main(
                ["--marker", str(marker), "--rc", "0",
                 "--pr-state", "OPEN", "--upgrade-only"]
            )
            self.assertEqual((rc, printed), (0, "attempted"))
            self.assertEqual(json.loads(marker.read_text()), before)

    def test_decline_note_is_stored_only_on_declined(self):
        with TemporaryDirectory() as td:
            marker = Path(td) / "sc-d.json"
            marker.write_text(json.dumps({"id": "sc-d"}) + "\n")
            rc, printed = run_main(
                ["--marker", str(marker), "--rc", "0", "--ahead", "0",
                 "--decline-note", "root cause is external (kimi 401); nothing to land"]
            )
            self.assertEqual((rc, printed), (0, "declined"))
            rec = json.loads(marker.read_text())
            self.assertEqual(
                rec["declineReason"],
                "root cause is external (kimi 401); nothing to land",
            )

            # A landed outcome must not carry a stale decline reason.
            marker2 = Path(td) / "sc-l.json"
            marker2.write_text(json.dumps({"id": "sc-l"}) + "\n")
            rc, printed = run_main(
                ["--marker", str(marker2), "--rc", "0", "--ahead", "1",
                 "--pr-state", "MERGED", "--decline-note", "leftover note"]
            )
            self.assertEqual((rc, printed), (0, "landed"))
            self.assertNotIn("declineReason", json.loads(marker2.read_text()))

    def test_unreadable_marker_never_fails_the_lane(self):
        with TemporaryDirectory() as td:
            missing = Path(td) / "nope.json"
            rc, _ = run_main(["--marker", str(missing), "--rc", "1"])
            self.assertEqual(rc, 0)
            self.assertFalse(missing.exists())


class BlocksRedispatchTest(unittest.TestCase):
    def _write(self, td: str, name: str, body: dict) -> Path:
        path = Path(td) / name
        path.write_text(json.dumps(body) + "\n")
        return path

    def test_missing_marker_does_not_block(self):
        self.assertFalse(dispatch_outcome.blocks_redispatch("/no/such/marker.json"))

    def test_when_terminal_outcomes_block(self):
        with TemporaryDirectory() as td:
            for outcome in ("landed", "attempted", "declined"):
                path = self._write(td, f"{outcome}.json", {"id": "x", "outcome": outcome})
                self.assertTrue(dispatch_outcome.blocks_redispatch(path), outcome)

    def test_process_failures_allow_retry(self):
        with TemporaryDirectory() as td:
            for outcome in ("failed", "timeout"):
                path = self._write(td, f"{outcome}.json", {"id": "x", "outcome": outcome})
                self.assertFalse(dispatch_outcome.blocks_redispatch(path), outcome)

    def test_when_outcome_less_fresh_blocks_abandoned_releases(self):
        # Live bug 2026-07-13: crash before outcome accounting left a marker
        # that permanently starved the pick lane.
        with TemporaryDirectory() as td:
            path = self._write(td, "hang.json", {"id": "sc-hang", "promptVersion": "abc"})
            now = path.stat().st_mtime
            self.assertTrue(
                dispatch_outcome.blocks_redispatch(path, now_sec=now + 60, abandon_after_sec=7200)
            )
            self.assertFalse(
                dispatch_outcome.blocks_redispatch(path, now_sec=now + 7201, abandon_after_sec=7200)
            )

    def test_when_corrupt_marker_blocks(self):
        with TemporaryDirectory() as td:
            path = Path(td) / "bad.json"
            path.write_text("{not-json\n")
            self.assertTrue(dispatch_outcome.blocks_redispatch(path))

    def test_when_authoritative_lifecycle_wins_over_stale_marker(self):
        with TemporaryDirectory() as td:
            path = self._write(td, "stale.json", {"id": "x", "outcome": "failed"})
            for phase in ("started", "pr_opened", "merged", "deployed", "watch_passed"):
                self.assertTrue(
                    dispatch_outcome.blocks_redispatch(path, authoritative_phase=phase), phase
                )
            path.write_text(json.dumps({"id": "x", "outcome": "attempted"}) + "\n")
            self.assertTrue(
                dispatch_outcome.blocks_redispatch(path, authoritative_phase="failed"),
                "failed lifecycle must not erase evidence of unlanded local work",
            )
            self.assertFalse(
                dispatch_outcome.blocks_redispatch(path, authoritative_phase="rolled_back"),
                "an observed rollback explicitly authorizes a fresh attempt",
            )


if __name__ == "__main__":
    unittest.main()
