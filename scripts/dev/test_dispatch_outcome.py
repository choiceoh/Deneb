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
    def test_decide(self):
        cases = [
            # (rc, ahead, pr_state) -> outcome
            ((0, 0, "MERGED"), "landed"),
            ((1, 3, "MERGED"), "landed"),  # merged wins over a dirty exit
            ((124, 0, ""), "timeout"),
            ((1, 0, ""), "failed"),
            ((0, 2, ""), "attempted"),  # unpushed/unlanded commits
            ((0, 0, "OPEN"), "attempted"),  # PR still in flight
            ((0, 0, ""), "declined"),  # clean exit, no work: contract clause
            ((0, 0, "CLOSED"), "declined"),  # PR closed without merge, no work left
        ]
        for (rc, ahead, state), want in cases:
            self.assertEqual(
                dispatch_outcome.decide(rc, ahead, state), want, (rc, ahead, state)
            )


class MarkerRewriteTest(unittest.TestCase):
    def test_records_outcome_additively(self):
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

    def test_upgrade_only_non_merged_is_noop(self):
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

    def test_unreadable_marker_never_fails_the_lane(self):
        with TemporaryDirectory() as td:
            missing = Path(td) / "nope.json"
            rc, _ = run_main(["--marker", str(missing), "--rc", "1"])
            self.assertEqual(rc, 0)
            self.assertFalse(missing.exists())


if __name__ == "__main__":
    unittest.main()
