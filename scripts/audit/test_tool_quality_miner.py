"""Deterministic tests for the tool-quality miner (RSI surface expansion, Lane A).

Load-bearing assertions: only tools over the error/repair bar AND above the
minimum call volume become candidates, ranking is impact-weighted (rate x
calls), the source id is stable per tool name, the exact stats ride in the
evidence for deterministic review, and a real run refuses to file blind.
"""

from __future__ import annotations

import io
import json
import os
import tempfile
import unittest

from tool_quality_miner import (
    MIN_CALLS,
    SOURCE_PREFIX,
    main,
    tool_quality_candidates,
)


def behavior(**overrides):
    report = {
        "tools": [
            # over the error bar, high volume — should rank first (most impact)
            {"name": "web", "calls": 200, "errors": 60, "repaired": 2},
            # over the repair bar only
            {"name": "exec", "calls": 100, "errors": 3, "repaired": 18},
            # healthy — below both bars
            {"name": "read", "calls": 500, "errors": 5, "repaired": 1},
            # over the error bar by rate but below MIN_CALLS — noise, dropped
            {"name": "asr", "calls": 4, "errors": 3, "repaired": 0},
        ],
    }
    report.update(overrides)
    return report


class CandidateTest(unittest.TestCase):
    def test_only_offenders_above_min_calls(self):
        cands = tool_quality_candidates(behavior()["tools"])
        names = [c["source"].split(":", 1)[1] for c in cands]
        self.assertEqual(set(names), {"web", "exec"})  # read healthy, asr too few calls

    def test_ranked_by_impact(self):
        cands = tool_quality_candidates(behavior()["tools"])
        # web: (0.30+0.01)*200=62 ; exec: (0.03+0.18)*100=21 → web first
        self.assertEqual(cands[0]["source"], f"{SOURCE_PREFIX}:web")

    def test_evidence_carries_exact_stats(self):
        cands = tool_quality_candidates(behavior()["tools"])
        web = next(c for c in cands if c["source"].endswith(":web"))
        self.assertIn("calls=200", web["evidence"])
        self.assertIn("errors=60", web["evidence"])
        self.assertEqual(web["scope"], "code")
        self.assertTrue(web["targetFiles"])

    def test_min_calls_boundary(self):
        tools = [{"name": "x", "calls": MIN_CALLS - 1, "errors": MIN_CALLS, "repaired": 0}]
        self.assertEqual(tool_quality_candidates(tools), [])

    def test_source_stable_per_tool(self):
        a = tool_quality_candidates(behavior()["tools"])
        b = tool_quality_candidates(behavior()["tools"])
        self.assertEqual([c["source"] for c in a], [c["source"] for c in b])


class CliDryRunTest(unittest.TestCase):
    def _fixture(self, tmp):
        path = os.path.join(tmp, "behavior.json")
        with open(path, "w", encoding="utf-8") as handle:
            json.dump(behavior(), handle)
        return path

    def test_dry_run_needs_no_gateway(self):
        with tempfile.TemporaryDirectory() as tmp:
            out, err = io.StringIO(), io.StringIO()
            rc = main(
                ["--behavior-report", self._fixture(tmp), "--dry-run", "--json",
                 "--url", "http://127.0.0.1:1", "--token", "t"],
                stdout=out, stderr=err,
            )
            self.assertEqual(rc, 0)
            self.assertIn("DRY-RUN continues WITHOUT dedup", err.getvalue())
            summary = json.loads(out.getvalue().strip().splitlines()[-1])
            self.assertEqual(summary["planned"], 2)  # web + exec
            self.assertEqual(summary["filed"], 0)
            self.assertTrue(summary["dry_run"])

    def test_cap_limits_plan(self):
        with tempfile.TemporaryDirectory() as tmp:
            out, err = io.StringIO(), io.StringIO()
            rc = main(
                ["--behavior-report", self._fixture(tmp), "--dry-run", "--json",
                 "--max", "1", "--url", "http://127.0.0.1:1", "--token", "t"],
                stdout=out, stderr=err,
            )
            self.assertEqual(rc, 0)
            summary = json.loads(out.getvalue().strip().splitlines()[-1])
            self.assertEqual(summary["planned"], 1)

    def test_real_run_refuses_to_file_blind(self):
        with tempfile.TemporaryDirectory() as tmp:
            out, err = io.StringIO(), io.StringIO()
            rc = main(
                ["--behavior-report", self._fixture(tmp),
                 "--url", "http://127.0.0.1:1", "--token", "t"],
                stdout=out, stderr=err,
            )
            self.assertEqual(rc, 1)
            self.assertIn("refusing to file blind", err.getvalue())


if __name__ == "__main__":
    unittest.main()
