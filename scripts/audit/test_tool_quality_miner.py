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
    latency_candidates,
    main,
    tool_quality_candidates,
)


def behavior(**overrides):
    report = {
        "tools": [
            # over the error bar, high volume — should rank first (most impact)
            {"name": "web", "calls": 200, "errors": 60, "repaired": 2, "avgMs": 3000},
            # over the repair bar only
            {"name": "exec", "calls": 100, "errors": 3, "repaired": 18, "avgMs": 2000},
            # healthy — below both bars
            {"name": "read", "calls": 500, "errors": 5, "repaired": 1, "avgMs": 400},
            # over the error bar by rate but below MIN_CALLS — noise, dropped
            {"name": "asr", "calls": 4, "errors": 3, "repaired": 0, "avgMs": 100},
        ],
    }
    report.update(overrides)
    return report


class CandidateTest(unittest.TestCase):
    def test_when_only_offenders_above_min_calls(self):
        cands = tool_quality_candidates(behavior()["tools"])
        names = [c["source"].split(":")[1] for c in cands]
        self.assertEqual(set(names), {"web", "exec"})  # read healthy, asr too few calls

    def test_when_ranked_by_impact(self):
        cands = tool_quality_candidates(behavior()["tools"])
        # web: (0.30+0.01)*200=62 ; exec: (0.03+0.18)*100=21 → web first
        self.assertEqual(cands[0]["source"], f"{SOURCE_PREFIX}:web:desc")

    def test_when_evidence_carries_exact_stats(self):
        cands = tool_quality_candidates(behavior()["tools"])
        web = next(c for c in cands if c["source"].endswith(":web:desc"))
        self.assertIn("calls=200", web["evidence"])
        self.assertIn("errors=60", web["evidence"])
        self.assertEqual(web["scope"], "code")
        self.assertTrue(web["targetFiles"])

    def test_min_calls_boundary(self):
        tools = [{"name": "x", "calls": MIN_CALLS - 1, "errors": MIN_CALLS, "repaired": 0}]
        self.assertEqual(tool_quality_candidates(tools), [])

    def test_when_source_stable_per_tool(self):
        a = tool_quality_candidates(behavior()["tools"])
        b = tool_quality_candidates(behavior()["tools"])
        self.assertEqual([c["source"] for c in a], [c["source"] for c in b])


class LatencyTest(unittest.TestCase):
    def test_when_over_ceiling_flags(self):
        # read's ceiling is 800ms; 2000ms avg over enough calls is over-ceiling.
        recent = [{"name": "read", "calls": 100, "avgMs": 2000}]
        cands = latency_candidates(recent, {"read": {"avgMs": 1900}})
        self.assertEqual(len(cands), 1)
        self.assertEqual(cands[0]["source"], f"{SOURCE_PREFIX}:read:latency")
        self.assertIn("avgMs=2000", cands[0]["evidence"])

    def test_when_regression_flags_even_under_ceiling(self):
        # web ceiling is 12000ms; 9000ms is under it, but it doubled vs baseline.
        recent = [{"name": "web", "calls": 50, "avgMs": 9000}]
        cands = latency_candidates(recent, {"web": {"avgMs": 4000}})
        self.assertEqual(len(cands), 1)
        self.assertIn("regressed", cands[0]["candidate"])

    def test_when_inherently_slow_but_stable_not_flagged(self):
        # web at 10000ms is under its 12000ms ceiling and steady vs baseline → OK.
        recent = [{"name": "web", "calls": 50, "avgMs": 10000}]
        self.assertEqual(latency_candidates(recent, {"web": {"avgMs": 9500}}), [])

    def test_when_low_volume_not_flagged(self):
        recent = [{"name": "read", "calls": MIN_CALLS - 1, "avgMs": 9000}]
        self.assertEqual(latency_candidates(recent, {}), [])

    def test_when_latency_source_distinct_from_desc(self):
        # The :latency and :desc sources for one tool must not prefix-collide.
        lat = latency_candidates([{"name": "web", "calls": 100, "avgMs": 20000}], {})
        self.assertEqual(lat[0]["source"], f"{SOURCE_PREFIX}:web:latency")


class CliDryRunTest(unittest.TestCase):
    def _fixture(self, tmp):
        path = os.path.join(tmp, "behavior.json")
        with open(path, "w", encoding="utf-8") as handle:
            json.dump(behavior(), handle)
        return path

    def test_when_dry_run_needs_no_gateway(self):
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

    def test_when_cap_limits_plan(self):
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

    def test_when_real_run_refuses_to_file_blind(self):
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


class ImpactContractTests(unittest.TestCase):
    """Both candidate kinds carry a finding-present contract closed by this
    miner from a fresh recent window — a landed description fix must earn a
    verified/no_effect verdict instead of staying unlabeled forever."""

    def _desc_tool(self, name="badtool", calls=40, errors=10, repaired=0, avg=100):
        return {"name": name, "calls": calls, "errors": errors,
                "repaired": repaired, "unknown": 0, "blocked": 0, "avgMs": avg}

    def test_candidates_carry_contracts(self):
        from tool_quality_miner import (
            IMPACT_WINDOW_MS, latency_candidates, tool_quality_candidates,
        )

        desc = tool_quality_candidates([self._desc_tool()])
        self.assertEqual(
            desc[0]["impactContract"]["metric"],
            "tool.quality.finding_present:badtool:desc",
        )
        self.assertEqual(desc[0]["impactContract"]["observationWindowMs"], IMPACT_WINDOW_MS)

        slow = latency_candidates(
            [{"name": "read", "calls": 40, "avgMs": 5000}], {})
        self.assertEqual(
            slow[0]["impactContract"]["metric"],
            "tool.quality.finding_present:read:latency",
        )

    def test_resolver_judges_fresh_recent_window(self):
        from health_finding_miner import ImpactMetricUnavailable
        from tool_quality_miner import tool_quality_impact_resolver

        recent = [
            self._desc_tool("recovered", calls=40, errors=1),      # 2.5% < bar
            self._desc_tool("stillbad", calls=40, errors=10),      # 25% >= bar
            {"name": "slowtool", "calls": 40, "avgMs": 9000},      # > default 3000
        ]
        resolve = tool_quality_impact_resolver(recent, {})

        observed, samples, note = resolve("tool.quality.finding_present:recovered:desc")
        self.assertEqual(observed, 0.0)
        self.assertEqual(samples, 40)
        self.assertIn("recovered", note)

        observed, _, _ = resolve("tool.quality.finding_present:stillbad:desc")
        self.assertEqual(observed, 1.0)

        observed, _, _ = resolve("tool.quality.finding_present:slowtool:latency")
        self.assertEqual(observed, 1.0)

        # Insufficient evidence keeps the verdict pending — silence is not success.
        with self.assertRaises(ImpactMetricUnavailable):
            resolve("tool.quality.finding_present:neverused:desc")

        # Other namespaces belong to their own evaluator.
        self.assertIsNone(resolve("deadcode.finding_present:abc"))
