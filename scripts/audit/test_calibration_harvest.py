"""Tests for the P5-2 calibration-window harvest computation."""

from __future__ import annotations

import datetime as dt
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from calibration_harvest import BENCH_TARGET, EPOCHS, WINDOW_OPEN_UTC, harvest, render_report

OPEN_MS = int(WINDOW_OPEN_UTC.timestamp() * 1000)


def cycle(offset_ms: int, epoch: str = "", bench: bool = False) -> dict:
    row = {"createdAt": OPEN_MS + offset_ms, "epoch": epoch, "reason": "r"}
    if bench:
        row["benchIncumbent"] = {"score": 1.0}
    return row


class HarvestTests(unittest.TestCase):
    def test_counts_only_benched_epoch_cycles_since_window_open(self) -> None:
        result = harvest(
            [
                cycle(-1000, "producer", bench=True),  # pre-window: excluded
                cycle(1000, "producer", bench=True),
                cycle(2000, "producer", bench=False),  # no bench payload
                cycle(3000, "evaluator", bench=True),
                cycle(4000, "", bench=True),  # no epoch: inventory-only
            ]
        )
        self.assertEqual(result["benched"], {"producer": 1, "evaluator": 1, "genesis": 0})
        self.assertEqual(result["cyclesSinceOpen"], 4)
        self.assertFalse(result["ready"])

    def test_ready_when_every_epoch_reaches_target(self) -> None:
        rows = [
            cycle(1000 * i + int(e != "producer") * 50, e, bench=True)
            for e in EPOCHS
            for i in range(BENCH_TARGET)
        ]
        result = harvest(rows)
        self.assertTrue(result["ready"])

    def test_report_renders_shortfall_and_ready_verdicts(self) -> None:
        now = dt.datetime(2026, 8, 23, 9, 0, tzinfo=dt.timezone.utc)
        short = render_report(
            harvest([cycle(1000, "producer", bench=True)]), now, conf_present=True, reverted=True
        )
        self.assertIn("미달", short)
        self.assertIn("이번 실행에서 제거", short)
        ready_rows = [cycle(1000 * i, e, bench=True) for e in EPOCHS for i in range(BENCH_TARGET)]
        ready = render_report(harvest(ready_rows), now, conf_present=False, reverted=False)
        self.assertIn("목표 달성", ready)

    def test_malformed_rows_are_skipped_not_fatal(self) -> None:
        result = harvest([{"epoch": "producer"}, {"createdAt": "garbage"}, {}])
        self.assertEqual(result["cyclesSinceOpen"], 0)


if __name__ == "__main__":
    unittest.main()
