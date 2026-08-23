"""Failure-safe metric tests for deadcode, recall, and quality shell scripts."""

from __future__ import annotations

import hashlib
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from test_shell_support import (
    REPO_ROOT,
    extract_heredoc,
    isolated_env,
    run_script,
    write_executable,
)


class FakeGoTestCase(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.fake_bin = self.root / "bin"
        self.home.mkdir()
        self.fake_bin.mkdir()
        write_executable(self.fake_bin / "go", """
            #!/usr/bin/env bash
            printf '%s' "${FAKE_GO_OUTPUT:-}"
            exit "${FAKE_GO_RC:-0}"
        """)

    def env(self, output="", rc=0):
        return isolated_env(
            self.home,
            self.fake_bin,
            FAKE_GO_OUTPUT=output,
            FAKE_GO_RC=str(rc),
        )


class DeadcodeAuditTests(FakeGoTestCase):
    baseline = REPO_ROOT / "scripts/audit/deadcode-baseline.txt"

    @classmethod
    def known_findings(cls) -> list[str]:
        return [
            line for line in cls.baseline.read_text(encoding="utf-8").splitlines()
            if line and not line.startswith("#")
        ]

    @staticmethod
    def raw_deadcode(findings: list[str]) -> str:
        rows = []
        for finding in findings:
            path, symbol = finding.split(" :: ", 1)
            rows.append(f"{path}:10:20: unreachable func: {symbol}")
        return "\n".join(rows) + ("\n" if rows else "")

    def test_exact_baseline_is_clean_and_never_rewrites_baseline(self) -> None:
        before = hashlib.sha256(self.baseline.read_bytes()).hexdigest()
        known = self.known_findings()
        proc = run_script(
            "scripts/audit/deadcode-audit.sh",
            env=self.env(self.raw_deadcode(list(reversed(known)))),
            timeout=20,
        )
        after = hashlib.sha256(self.baseline.read_bytes()).hexdigest()
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(before, after)
        self.assertEqual(
            proc.stdout.strip(),
            f"deadcode-audit: clean ({len(known)} accepted baseline entries, 0 new)",
        )

    def test_new_finding_exits_one_and_normalizes_line_column_drift(self) -> None:
        known = self.known_findings()
        new_finding = "internal/new/orphan.go :: orphanedFunction"
        output = self.raw_deadcode(known) + (
            "internal/new/orphan.go:999:123: unreachable func: orphanedFunction\n"
        )
        proc = run_script(
            "scripts/audit/deadcode-audit.sh",
            env=self.env(output),
            timeout=20,
        )
        self.assertEqual(proc.returncode, 1)
        self.assertIn("NEW dead code (1 findings)", proc.stdout)
        self.assertIn(f"  + {new_finding}", proc.stdout)
        self.assertIn("delete the code", proc.stdout)

    def test_resolved_baseline_entry_is_reported_stale_but_not_a_failure(self) -> None:
        known = self.known_findings()
        removed = known[0]
        proc = run_script(
            "scripts/audit/deadcode-audit.sh",
            env=self.env(self.raw_deadcode(known[1:])),
            timeout=20,
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn("1 baseline entries no longer dead", proc.stdout)
        self.assertIn(f"  - {removed}", proc.stdout)
        self.assertIn("0 new", proc.stdout)

    def test_deadcode_tool_failure_has_distinct_exit_two(self) -> None:
        # One attempt, no sleep: the contract under test is the exit code and
        # the diagnostic, not the production retry budget (3 × 20s).
        env = self.env("partial output\n", rc=1)
        env["DENEB_DEADCODE_RETRIES"] = "1"
        proc = run_script(
            "scripts/audit/deadcode-audit.sh",
            env=env,
            timeout=20,
        )
        self.assertEqual(proc.returncode, 2)
        self.assertEqual(proc.stdout, "")
        self.assertIn("failed to run deadcode", proc.stderr)

    def test_deadcode_tool_failure_retries_then_surfaces_go_stderr(self) -> None:
        # 2026-08-18: the weekly miner failed with a bare "failed to run
        # deadcode" because go's stderr was discarded. The retry loop must try
        # the configured number of times and the final message must carry the
        # tool's own error text.
        env = self.env("", rc=1)
        env["DENEB_DEADCODE_RETRIES"] = "2"
        env["DENEB_DEADCODE_RETRY_SLEEP"] = "0"
        proc = run_script(
            "scripts/audit/deadcode-audit.sh",
            env=env,
            timeout=20,
        )
        self.assertEqual(proc.returncode, 2)
        self.assertIn("2 attempt(s)", proc.stderr)


class RecallGainTests(FakeGoTestCase):
    def test_success_emits_guard_lines_and_signed_gain_metric(self) -> None:
        output = (
            "=== RUN TestRecallWikiGain\n"
            "RECALL_GAIN both=8 diaryonly=5 wikionly=7 gain=3 total=10\n"
            "RECALL_STALELEAK old_surfaced=false new_surfaced=true\n"
            "RECALL_DRIFT query=x top=correct\n"
            "PASS\n"
        )
        proc = run_script("scripts/dev/recall-gain.sh", env=self.env(output), timeout=10)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout.splitlines(), [
            "RECALL_GAIN both=8 diaryonly=5 wikionly=7 gain=3 total=10",
            "RECALL_STALELEAK old_surfaced=false new_surfaced=true",
            "RECALL_DRIFT query=x top=correct",
            "metric_value=3",
        ])

    def test_failed_go_test_never_reports_positive_metric_from_partial_output(self) -> None:
        output = (
            "RECALL_GAIN both=9 diaryonly=1 wikionly=9 gain=8 total=10\n"
            "--- FAIL: TestRecallStaleBeliefGuard (0.01s)\nFAIL\n"
        )
        proc = run_script("scripts/dev/recall-gain.sh", env=self.env(output, rc=1), timeout=10)
        self.assertEqual(proc.returncode, 1)
        self.assertIn("metric_value=0", proc.stdout)
        self.assertNotIn("metric_value=8", proc.stdout)
        self.assertIn("recall gain tests failed", proc.stderr)
        self.assertIn("--- FAIL", proc.stderr)

    def test_missing_gain_line_is_invalid_even_when_go_exits_zero(self) -> None:
        proc = run_script(
            "scripts/dev/recall-gain.sh",
            env=self.env("PASS\n"),
            timeout=10,
        )
        self.assertEqual(proc.returncode, 1)
        self.assertEqual(proc.stdout.strip(), "metric_value=0")
        self.assertIn("did not produce RECALL_GAIN", proc.stderr)


class RecallMetricTests(FakeGoTestCase):
    def test_when_success_extracts_last_metric_line_and_value(self) -> None:
        output = (
            "RECALL_METRIC hits=1 total=2 pct=50\n"
            "noise\n"
            "RECALL_METRIC hits=9 total=10 pct=90\n"
        )
        proc = run_script("scripts/dev/recall-metric.sh", env=self.env(output), timeout=10)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout.splitlines(), [
            "RECALL_METRIC hits=9 total=10 pct=90",
            "metric_value=90",
        ])

    def test_failed_go_test_cannot_leak_a_partial_positive_metric(self) -> None:
        output = "RECALL_METRIC hits=10 total=10 pct=100\nFAIL\n"
        proc = run_script(
            "scripts/dev/recall-metric.sh",
            env=self.env(output, rc=1),
            timeout=10,
        )
        self.assertEqual(proc.returncode, 1)
        self.assertEqual(proc.stdout.strip(), "metric_value=0")
        self.assertIn("recall quality tests failed", proc.stderr)

    def test_missing_metric_line_fails_with_zero_machine_value(self) -> None:
        proc = run_script(
            "scripts/dev/recall-metric.sh",
            env=self.env("PASS\n"),
            timeout=10,
        )
        self.assertEqual(proc.returncode, 1)
        self.assertEqual(proc.stdout.strip(), "metric_value=0")
        self.assertIn("did not produce RECALL_METRIC", proc.stderr)


class QualityMetricEmbeddedProgramTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        script = REPO_ROOT / "scripts/dev/quality-metric.sh"
        cls.program = extract_heredoc(script, "python3 - <<'PYEOF'", "PYEOF")

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.module_dir = self.root / "modules"
        self.module_dir.mkdir()
        (self.module_dir / "mock_native_client.py").write_text(
            """
import os
from types import SimpleNamespace

def check_prerequisites():
    if os.environ.get('FAKE_PREREQ', 'ok') == 'ok':
        return True, 'ok'
    return False, os.environ.get('FAKE_PREREQ', 'missing token')

class NativeTestClient:
    async def connect(self):
        if os.environ.get('FAKE_CONNECT_ERROR'):
            raise RuntimeError(os.environ['FAKE_CONNECT_ERROR'])
        return 'fixture'

    async def chat(self, message):
        return SimpleNamespace(
            reply_text=os.environ.get('FAKE_REPLY', ''),
            latency_ms=float(os.environ.get('FAKE_LATENCY', '0')),
            draft_edits=[None] * int(os.environ.get('FAKE_EDITS', '0')),
            raw_events=[None] * int(os.environ.get('FAKE_EVENTS', '0')),
            errors=['error'] * int(os.environ.get('FAKE_ERRORS', '0')),
        )

    async def disconnect(self):
        return None
""".lstrip(),
            encoding="utf-8",
        )

    def run_program(self, **values: str) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        env.update({
            "DENEB_SCRIPTS_DIR": str(self.module_dir),
            "MOCK_QUALITY_MESSAGE": "fixture message",
        })
        env.update(values)
        return subprocess.run(
            [sys.executable, "-c", self.program],
            env=env,
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )

    def test_perfect_korean_substantive_clean_fast_stream_scores_hundred(self) -> None:
        proc = self.run_program(
            FAKE_REPLY="한글응답" * 30,
            FAKE_LATENCY="9999",
            FAKE_EDITS="4",
            FAKE_EVENTS="6",
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout.splitlines(), [
            "metric_value=100",
            "DENEB_METRIC_DETAIL korean=25 substance=25 clean=20 latency=15 streaming=15 penalty=0",
        ])
        self.assertIn("korean=25 substance=25 clean=20", proc.stderr)

    def test_when_threshold_boundaries_use_lower_bucket_at_exact_cutoffs(self) -> None:
        proc = self.run_program(
            FAKE_REPLY="abcdefghi",
            FAKE_LATENCY="10000",
            FAKE_EDITS="0",
            FAKE_EVENTS="1",
        )
        # korean=0, substance=5, clean=20, latency=12, streaming=5.
        self.assertEqual(proc.stdout.splitlines()[0], "metric_value=42")
        self.assertIn("latency=12", proc.stdout)
        self.assertIn("streaming=5", proc.stdout)

    def test_leaks_filler_and_many_errors_floor_total_at_zero(self) -> None:
        proc = self.run_program(
            FAKE_REPLY=(
                "좋은 질문 NO_REPLY <function=x> <thinking>secret</thinking> "
                "MEDIA:file [[reply_to:1]]"
            ),
            FAKE_LATENCY="60000",
            FAKE_EDITS="0",
            FAKE_EVENTS="0",
            FAKE_ERRORS="10",
        )
        self.assertEqual(proc.stdout.splitlines()[0], "metric_value=0")
        self.assertIn("clean=0", proc.stdout)
        self.assertIn("penalty=100", proc.stdout)

    def test_prerequisite_and_connect_failures_emit_zero_machine_metric(self) -> None:
        prereq = self.run_program(FAKE_PREREQ="missing token")
        self.assertEqual(prereq.stdout.strip(), "metric_value=0")
        self.assertIn("prereq_error=missing token", prereq.stderr)

        connect = self.run_program(FAKE_CONNECT_ERROR="gateway down")
        self.assertEqual(connect.stdout.strip(), "metric_value=0")
        self.assertIn("connect_error=gateway down", connect.stderr)


if __name__ == "__main__":
    unittest.main()
