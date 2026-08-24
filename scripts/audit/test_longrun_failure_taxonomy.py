#!/usr/bin/env python3
"""Long-horizon failure taxonomy (StateM/LongHorizon-Harness/OneDayAgent).

The audit exists to answer "do we have the papers' disease?" honestly, so the
tests pin the two ways a taxonomy lies: tagging healthy work as sick (a coding
session's heavy exec use is not a loop) and letting sickness hide (a run that
compacted mid-flight must show as context-pressure even when it ended cleanly).
"""

from __future__ import annotations

import json
import tempfile
import time
import unittest
from collections import Counter
from pathlib import Path

from longrun_failure_taxonomy import analyze, render, tag_run

NOW = time.time() * 1000


def _write_log(root: Path, session: str, rows: list[dict]) -> None:
    root.mkdir(parents=True, exist_ok=True)
    (root / f"{session}.jsonl").write_text(
        "".join(json.dumps(r) + "\n" for r in rows), encoding="utf-8"
    )


def _end(run_id: str, *, turns: int = 12, stop: str = "end_turn", **extra) -> dict:
    return {"ts": NOW, "type": "run.end", "runId": run_id,
            "data": {"turns": turns, "toolCalls": 0, "stopReason": stop, **extra}}


def _tool(run_id: str, name: str, *, err: bool = False) -> dict:
    return {"ts": NOW, "type": "turn.tool", "runId": run_id,
            "data": {"name": name, "isError": err}}


class TagRunTests(unittest.TestCase):
    def test_heavy_tool_use_without_errors_is_not_churn(self) -> None:
        # The naive call-count proxy tagged 61% of long runs as loops; a coding
        # session's 53 exec calls are a workhorse, not a failure.
        modes = tag_run({"stopReason": "end_turn"}, Counter({"exec": 53}), Counter())
        self.assertEqual(modes, ["clean"])

    def test_repeated_calls_with_errors_are_churn(self) -> None:
        modes = tag_run({"stopReason": "end_turn"}, Counter({"gmail": 9}), Counter({"gmail": 4}))
        self.assertIn("repeat-churn", modes)

    def test_compaction_and_truncation_are_context_pressure(self) -> None:
        self.assertIn("context-pressure", tag_run({"stopReason": "end_turn", "compacted": 1}, Counter(), Counter()))
        # truncatedToolCalls is an int on old rows and a per-tool map on new ones.
        self.assertIn("context-pressure",
                      tag_run({"stopReason": "end_turn", "truncatedToolCalls": {"exec": 2}}, Counter(), Counter()))
        self.assertIn("context-pressure",
                      tag_run({"stopReason": "end_turn", "truncatedToolCalls": 1}, Counter(), Counter()))

    def test_user_abort_is_not_a_premature_stop(self) -> None:
        modes = tag_run({"stopReason": "aborted"}, Counter(), Counter())
        self.assertEqual(modes, ["user-abort"])
        self.assertIn("premature-stop", tag_run({"stopReason": "timeout"}, Counter(), Counter()))

    def test_modes_stack_rather_than_mask(self) -> None:
        modes = tag_run({"stopReason": "timeout", "compacted": 1},
                        Counter({"web": 10}), Counter({"web": 5}))
        self.assertEqual(set(modes), {"context-pressure", "premature-stop", "repeat-churn", "error-churn"})


class AnalyzeTests(unittest.TestCase):
    def test_short_runs_stay_out_of_the_denominator(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write_log(root, "s", [
                _end("r1", turns=2),
                _end("r2", turns=12, stop="timeout"),
            ])
            tax = analyze(root, now_ms=NOW)
        self.assertEqual((tax.total_runs, tax.long_runs), (2, 1))
        self.assertEqual(tax.mode_counts["premature-stop"], 1)

    def test_tool_errors_join_their_own_run_only(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write_log(root, "s", [
                *[_tool("bad", "gmail", err=True) for _ in range(5)],
                *[_tool("bad", "gmail") for _ in range(4)],
                _end("bad", turns=12),
                _end("good", turns=12),
            ])
            tax = analyze(root, now_ms=NOW)
        self.assertEqual(tax.mode_counts["repeat-churn"], 1)
        self.assertEqual(tax.mode_counts["clean"], 1)

    def test_old_rows_and_missing_dir_degrade_quietly(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            stale = dict(_end("r", turns=12, stop="timeout"))
            stale["ts"] = NOW - 40 * 86400 * 1000
            _write_log(root, "s", [stale])
            tax = analyze(root, now_ms=NOW)
            self.assertEqual(tax.total_runs, 0)
            out = render(tax)
        self.assertIn("no long-horizon runs", out)
        self.assertEqual(analyze(root / "absent", now_ms=NOW).total_runs, 0)

    def test_render_names_the_blind_spot(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write_log(root, "s", [_end("r", turns=12)])
            out = render(analyze(root, now_ms=NOW))
        self.assertIn("goal drift", out)


if __name__ == "__main__":
    unittest.main()
