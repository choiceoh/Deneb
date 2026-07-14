"""Tests for the durable L4 dispatcher runtime status."""

from __future__ import annotations

import subprocess
import shutil
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from coding_dispatch_status import record_status


class CodingDispatchStatusTest(unittest.TestCase):
    def _session_status(self, pr_outcome: str, outcome: str, rc: int) -> str:
        dispatcher_path = Path(__file__).with_name("coding-dispatch.sh")
        proc = subprocess.run(
            [
                "/bin/bash",
                "-c",
                'source "$1"; '
                'record_runtime_status() { printf "%s|%s|%s" "$1" "$2" "$3"; }; '
                'record_session_status candidate "$2" "$3" "$4"',
                "test",
                str(dispatcher_path),
                pr_outcome,
                outcome,
                str(rc),
            ],
            check=True,
            capture_output=True,
            text=True,
        )
        return proc.stdout

    def test_dispatcher_uses_codex_executor_without_claude_binary_contract(self):
        dispatcher = Path(__file__).with_name("coding-dispatch.sh").read_text(encoding="utf-8")
        self.assertIn("coding_dispatch_executor.py", dispatcher)
        self.assertIn('python3 "$DISPATCH_EXECUTOR" --check', dispatcher)
        self.assertNotIn("DENEB_DISPATCH_CLAUDE_BIN", dispatcher)
        self.assertNotIn("resolve_claude", dispatcher)
        self.assertNotIn("Claude Code를 -p", dispatcher)

    def test_dispatcher_resolves_github_cli_outside_systemd_path(self):
        dispatcher_path = Path(__file__).with_name("coding-dispatch.sh")
        dispatcher = dispatcher_path.read_text(encoding="utf-8")
        release_doc = (
            Path(__file__).parents[2] / "docs/agent-rules/release-and-deploy.md"
        ).read_text(encoding="utf-8")
        self.assertIn("DENEB_DISPATCH_GH_BIN", dispatcher)
        self.assertIn("$HOME/.local/bin/gh", dispatcher)
        self.assertGreaterEqual(dispatcher.count('"$GH_BIN" pr list'), 3)
        self.assertIn("Environment=DENEB_DISPATCH_GH_BIN=%h/.local/bin/gh", release_doc)

        with TemporaryDirectory() as td:
            gh = Path(td) / ".local/bin/gh"
            gh.parent.mkdir(parents=True)
            gh.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
            gh.chmod(0o755)
            isolated_bin = Path(td) / "bin"
            isolated_bin.mkdir()
            for tool in ("dirname", "python3"):
                source = shutil.which(tool)
                self.assertIsNotNone(source)
                (isolated_bin / tool).symlink_to(source)
            env = {"HOME": td, "PATH": str(isolated_bin)}
            proc = subprocess.run(
                [
                    "/bin/bash",
                    "-c",
                    'source "$1"; printf "%s" "$GH_BIN"',
                    "test",
                    str(dispatcher_path),
                ],
                env=env,
                check=True,
                capture_output=True,
                text=True,
            )
            self.assertEqual(proc.stdout, str(gh))

    def test_unavailable_github_probe_is_not_recorded_as_candidate_failure(self):
        dispatcher_path = Path(__file__).with_name("coding-dispatch.sh")
        dispatcher = dispatcher_path.read_text(encoding="utf-8")
        self.assertIn('if ! pr_json=$(pr_json_for_branch "$branch"); then', dispatcher)
        self.assertIn('PR_OUTCOME="unknown"', dispatcher)
        env = {"HOME": "/tmp/deneb-test-no-gh", "PATH": "/usr/bin:/bin"}
        proc = subprocess.run(
            [
                "/bin/bash",
                "-c",
                'source "$1"; GH_BIN=""; set +e; '
                "record_pr_outcome cid attempt branch 0 1; rc=$?; "
                'printf "%s:%s" "$rc" "$PR_OUTCOME"',
                "test",
                str(dispatcher_path),
            ],
            env=env,
            check=True,
            capture_output=True,
            text=True,
        )
        self.assertEqual(proc.stdout, "1:unknown")

    def test_instant_process_failure_is_classified_as_environment_failure(self):
        dispatcher = Path(__file__).with_name("coding-dispatch.sh").read_text(encoding="utf-8")
        expected = (
            'record_runtime_status environment_failed "instant environment failure rc=$rc" "$cid"'
        )
        self.assertIn(expected, dispatcher)

    def test_clean_decline_is_completed_not_session_failure(self):
        self.assertEqual(
            self._session_status("failed", "declined", 0),
            "completed|session declined safely; no code or PR|candidate",
        )

    def test_real_session_failures_remain_visible(self):
        self.assertTrue(
            self._session_status("failed", "failed", 1).startswith("session_failed|")
        )
        self.assertTrue(
            self._session_status("failed", "timeout", 124).startswith("session_failed|")
        )
        self.assertEqual(
            self._session_status("open", "attempted", 0),
            "pr_opened|PR open|candidate",
        )

    def test_failures_accumulate_and_success_resets(self):
        with TemporaryDirectory() as td:
            path = Path(td) / "status.json"
            first = record_status(path, "setup_failed", detail="branch conflict", now_ms=1)
            second = record_status(path, "ledger_failed", now_ms=2)
            dispatched = record_status(path, "dispatched", candidate_id="sc-1", now_ms=3)
            completed = record_status(path, "completed", now_ms=4)
            self.assertEqual(first["consecutiveFailures"], 1)
            self.assertEqual(second["consecutiveFailures"], 2)
            self.assertEqual(dispatched["consecutiveFailures"], 0)
            self.assertEqual(dispatched["lastDispatchAtMs"], 3)
            self.assertEqual(completed["lastSuccessfulAtMs"], 4)

    def test_busy_tick_does_not_hide_existing_failure_streak(self):
        with TemporaryDirectory() as td:
            path = Path(td) / "status.json"
            record_status(path, "environment_failed", now_ms=1)
            busy = record_status(path, "busy", now_ms=2)
            self.assertEqual(busy["consecutiveFailures"], 1)


if __name__ == "__main__":
    unittest.main()
