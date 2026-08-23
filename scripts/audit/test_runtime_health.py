"""Stdlib regression tests for runtime-health scoring and its compatibility CLI."""

from __future__ import annotations

import io
import subprocess
import sys
import unittest
from datetime import date
from pathlib import Path

AUDIT_DIR = Path(__file__).resolve().parent
if str(AUDIT_DIR) not in sys.path:
    sys.path.insert(0, str(AUDIT_DIR))

import runtime_health as health


VALID_FIXTURE = "\n".join(
    [
        "2026-07-08T10:00:00+0900 host deneb-gateway[1]: agent loop complete agentMs=60000 turns=2 totalToolCalls=1 stopReason=done",
        "2026-07-09T11:00:00+0900 host deneb-gateway[1]: agent loop complete agentMs=45000 turns=1 totalToolCalls=0 stopReason=done",
    ]
)


def latency_score(signals: health.Signals) -> float:
    _, dimensions = health.compute(signals)
    return next(dimension.score for dimension in dimensions if dimension.name == "latency")


def dimension_score(signals: health.Signals, name: str) -> float:
    _, dimensions = health.compute(signals)
    return next(dimension.score for dimension in dimensions if dimension.name == name)


class ParsingAndDataSufficiencyTests(unittest.TestCase):
    def test_when_journal_no_entries_is_insufficient_data_exit_2(self) -> None:
        stdout = io.StringIO()
        stderr = io.StringIO()

        result = health.main(
            ["--stdin"],
            stdin=io.StringIO("-- No entries --\n"),
            stdout=stdout,
            stderr=stderr,
        )

        self.assertEqual(result, 2)
        self.assertEqual(stdout.getvalue(), "")
        self.assertIn("insufficient data: 0 completed runs", stderr.getvalue())
        self.assertNotIn("100", stderr.getvalue())

    def test_when_zero_runs_cannot_be_scored_through_importable_api(self) -> None:
        with self.assertRaisesRegex(health.InsufficientDataError, "0 completed runs"):
            health.compute(health.Signals())

    def test_when_zero_agent_ms_samples_cannot_be_scored_through_importable_api(self) -> None:
        with self.assertRaisesRegex(health.InsufficientDataError, "0 agentMs samples"):
            health.compute(health.Signals(runs=1))

    def test_when_zero_agent_ms_samples_are_insufficient_data_in_cli(self) -> None:
        stdout = io.StringIO()
        stderr = io.StringIO()
        line = "2026-07-10T10:00:00+0900 host unit: agent loop complete totalToolCalls=0"

        result = health.main(
            ["--stdin"],
            stdin=io.StringIO(line),
            stdout=stdout,
            stderr=stderr,
        )

        self.assertEqual(result, 2)
        self.assertEqual(stdout.getvalue(), "")
        self.assertIn("insufficient data: 0 agentMs samples", stderr.getvalue())

    def test_preserves_valid_run_fields_when_parsing_journal(self) -> None:
        signals = health.parse(VALID_FIXTURE.splitlines())
        self.assertEqual(signals.runs, 2)
        self.assertEqual(signals.agent_ms, [60000, 45000])
        self.assertEqual(signals.turns, [2, 1])
        self.assertEqual(signals.tool_calls, 1)
        self.assertEqual(signals.tool_call_reports, 2)


class JournalSpanTests(unittest.TestCase):
    def test_when_iso_journal_span_is_inclusive(self) -> None:
        lines = [
            "2026-07-08T23:59:59+0900 host unit: first",
            "2026-07-10T00:00:01+0900 host unit: last",
        ]
        self.assertEqual(health.span_days(lines), 3.0)

    def test_when_english_journal_span_is_inclusive(self) -> None:
        lines = [
            "Jul  8 23:59:59 host unit[1]: first",
            "Jul 10 00:00:01 host unit[1]: last",
        ]
        self.assertEqual(health.span_days(lines, reference=date(2026, 7, 10)), 3.0)

    def test_when_english_journal_span_handles_year_rollover(self) -> None:
        lines = [
            "Dec 31 23:59:59 host unit[1]: first",
            "Jan  1 00:00:01 host unit[1]: last",
        ]
        self.assertEqual(health.span_days(lines, reference=date(2026, 1, 2)), 2.0)

    def test_when_english_journal_span_uses_non_leap_reference_year(self) -> None:
        lines = [
            "Feb 28 23:59:59 host unit[1]: first",
            "Mar  1 00:00:01 host unit[1]: last",
        ]
        self.assertEqual(health.span_days(lines, reference=date(2026, 3, 2)), 2.0)

    def test_when_korean_journal_span_is_inclusive(self) -> None:
        lines = [
            "7\uc6d4  8 23:59:59 host unit[1]: first",
            "7\uc6d4 10 00:00:01 host unit[1]: last",
        ]
        self.assertEqual(health.span_days(lines, reference=date(2026, 7, 10)), 3.0)


class ScoringBoundaryTests(unittest.TestCase):
    def test_when_graded_soft_midpoint_and_hard_boundaries(self) -> None:
        self.assertEqual(health.graded(60.0, 60.0, 240.0), 1.0)
        self.assertEqual(health.graded(150.0, 60.0, 240.0), 0.5)
        self.assertEqual(health.graded(240.0, 60.0, 240.0), 0.0)

    def test_p95_uses_nearest_rank_without_off_by_one(self) -> None:
        self.assertEqual(health.percentile(list(range(1, 21)), 0.95), 19)
        signals = health.Signals(runs=20, agent_ms=[60000] * 19 + [240000], days=1.0)
        self.assertEqual(latency_score(signals), 100.0)

    def test_when_latency_score_honors_soft_midpoint_and_hard_boundaries(self) -> None:
        self.assertEqual(latency_score(health.Signals(runs=20, agent_ms=[60000] * 20)), 100.0)
        self.assertEqual(latency_score(health.Signals(runs=20, agent_ms=[150000] * 20)), 50.0)
        self.assertEqual(latency_score(health.Signals(runs=20, agent_ms=[240000] * 20)), 0.0)

    def test_when_partial_latency_coverage_reduces_latency_score(self) -> None:
        signals = health.Signals(runs=2, agent_ms=[60000])
        self.assertEqual(latency_score(signals), 50.0)

    def test_tool_report_coverage_distinguishes_missing_from_explicit_zero(self) -> None:
        missing = health.Signals(runs=2, agent_ms=[60000, 60000], tool_call_reports=1)
        explicit_zero = health.Signals(runs=2, agent_ms=[60000, 60000], tool_call_reports=2)

        self.assertEqual(dimension_score(missing, "tool-reliability"), 50.0)
        self.assertEqual(dimension_score(explicit_zero, "tool-reliability"), 100.0)


class OutputContractTests(unittest.TestCase):
    def test_valid_fixture_keeps_metric_tail_contract(self) -> None:
        stdout = io.StringIO()
        stderr = io.StringIO()

        result = health.main(
            ["--stdin"],
            stdin=io.StringIO(VALID_FIXTURE),
            stdout=stdout,
            stderr=stderr,
        )

        self.assertEqual(result, 0)
        self.assertEqual(
            stdout.getvalue().splitlines(),
            [
                "metric_value=100.0",
                "DENEB_RUNTIME_DETAIL stability=100.0 error-rate=100.0 llm-serving=100.0 "
                "turn-reliability=100.0 tool-reliability=100.0 latency=100.0",
            ],
        )
        self.assertIn("2 runs over ~2.0d", stderr.getvalue())

    def test_when_hyphenated_cli_path_remains_executable(self) -> None:
        result = subprocess.run(
            [sys.executable, str(AUDIT_DIR / "runtime-health.py"), "--stdin"],
            input=VALID_FIXTURE,
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(result.stdout.splitlines()[-2].startswith("metric_value="))
        self.assertTrue(result.stdout.splitlines()[-1].startswith("DENEB_RUNTIME_DETAIL "))

    def test_hyphenated_cli_returns_2_for_journal_no_entries(self) -> None:
        result = subprocess.run(
            [sys.executable, str(AUDIT_DIR / "runtime-health.py"), "--stdin"],
            input="-- No entries --\n",
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )

        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stdout, "")
        self.assertIn("insufficient data: 0 completed runs", result.stderr)


if __name__ == "__main__":
    unittest.main()


class StageBreakdownTest(unittest.TestCase):
    """llmMs/toolMs stage decomposition on the latency dimension (2026-07-20)."""

    def _line(self, agent, llm, tool):
        return (f"2026-07-08T10:00:00+0900 host deneb-gateway[1]: agent loop complete "
                f"agentMs={agent} llmMs={llm} toolMs={tool} turns=2 totalToolCalls=1 stopReason=done")

    def test_slow_cohort_attribution(self):
        # Nine fast runs + one slow run dominated by tool time: the top-10%
        # cohort is exactly the slow run, so the breakdown must attribute it.
        lines = [self._line(5000, 4000, 500) for _ in range(9)]
        lines.append(self._line(120000, 20000, 90000))
        s = health.parse(lines)
        self.assertEqual(len(s.stage_samples), 10)
        extra = health.latency_stage_extra(s.stage_samples)
        self.assertEqual(extra["slow_cohort"], 1)
        self.assertAlmostEqual(extra["slow_tool_share"], 0.75, places=2)
        detail = health.latency_stage_detail(s.stage_samples)
        self.assertIn("tools 75%", detail[0])

    def test_legacy_lines_without_stage_fields_are_na(self):
        lines = ["2026-07-08T10:00:00+0900 host deneb-gateway[1]: agent loop complete "
                 "agentMs=60000 turns=2 totalToolCalls=1 stopReason=done"]
        s = health.parse(lines)
        self.assertEqual(s.stage_samples, [])
        self.assertEqual(health.latency_stage_extra([]), {})
        self.assertIn("n/a", health.latency_stage_detail([])[0])


class McpStderrPassthroughTest(unittest.TestCase):
    """MCP child stderr is another process's logging, not a gateway error."""

    def test_child_stderr_lines_are_counted_but_not_scored(self) -> None:
        lines = [
            "2026-08-20T10:00:00+0900 host deneb-gateway[1]: agent loop complete agentMs=1000 turns=1 totalToolCalls=1 stopReason=done",
            '2026-08-20T10:00:01+0900 host deneb-gateway[1]: mcp server stderr cmd=npx line="error: [TypeError: fetch failed] {"',
            '2026-08-20T10:00:01+0900 host deneb-gateway[1]: mcp server stderr cmd=npx line="[cause]: Error: getaddrinfo EAI_AGAIN us.i.posthog.com"',
        ]
        signals = health.parse(lines)
        self.assertEqual(signals.other_errors, 0)
        self.assertEqual(signals.mcp_stderr_lines, 2)
        _, dimensions = health.compute(signals)
        error_rate = next(d for d in dimensions if d.name == "error-rate")
        self.assertEqual(error_rate.score, 100.0)
        self.assertEqual(error_rate.extra["mcp_stderr_lines"], 2)

    def test_gateway_own_errors_still_count(self) -> None:
        lines = [
            "2026-08-20T10:00:00+0900 host deneb-gateway[1]: agent loop complete agentMs=1000 turns=1 totalToolCalls=1 stopReason=done",
            "2026-08-20T10:00:02+0900 host deneb-gateway[1]: [autonomous] periodic task failed task=groupware-radar error=boom",
        ]
        signals = health.parse(lines)
        self.assertEqual(signals.other_errors, 1)
        self.assertEqual(signals.mcp_stderr_lines, 0)
