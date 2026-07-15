"""Tests for the durable L4 dispatcher runtime status."""

from __future__ import annotations

import subprocess
import shutil
import json
import datetime
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
                "record_pr_outcome cid attempt branch 0 1 0; rc=$?; "
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
            self._session_status("declined", "declined", 0),
            "completed|session declined safely; no code or PR|candidate",
        )

    def test_clean_no_diff_records_authoritative_declined_phase(self):
        dispatcher_path = Path(__file__).with_name("coding-dispatch.sh")
        proc = subprocess.run(
            [
                "/bin/bash", "-c",
                'source "$1"; '
                'pr_json_for_branch() { printf "[]"; }; '
                'record_event() { printf "%s" "$4"; }; '
                'record_pr_outcome cid attempt branch 0 1 0; '
                'printf ":%s" "$PR_OUTCOME"',
                "test", str(dispatcher_path),
            ],
            check=True, capture_output=True, text=True,
        )
        self.assertEqual(proc.stdout, "declined:declined")

    def test_reconcile_never_regresses_terminal_or_post_merge_phases(self):
        dispatcher_path = Path(__file__).with_name("coding-dispatch.sh")
        outcome_path = Path(__file__).with_name("dispatch_outcome.py")
        with TemporaryDirectory() as td:
            root = Path(td)
            dispatch_dir = root / "coding_dispatch"
            dispatch_dir.mkdir()
            candidates = []
            for phase in ("deployed", "watch_passed", "rolled_back", "declined"):
                cid, attempt = f"sc-{phase}", f"attempt-{phase}"
                (dispatch_dir / f"{cid}.json").write_text(json.dumps({
                    "id": cid, "attemptId": attempt, "branch": f"dispatch/{attempt}",
                    "outcome": "landed",
                }) + "\n", encoding="utf-8")
                candidates.append({
                    "id": cid, "attemptId": attempt, "dispatchPhase": phase,
                })
            (dispatch_dir / "corrupt.json").write_text("{", encoding="utf-8")
            rpc = root / "rpc.py"
            rpc.write_text(
                "import json\nprint(json.dumps(" + repr(candidates) + "))\n",
                encoding="utf-8",
            )
            log = root / "dispatch.log"
            proc = subprocess.run(
                [
                    "/bin/bash", "-c",
                    'source "$1"; DISPATCH_DIR="$2"; DISPATCH_RPC="$3"; '
                    'LOG_FILE="$4"; DISPATCH_OUTCOME="$5"; '
                    'pr_json_for_branch() { printf \'[{"state":"MERGED","number":1,'
                    '"url":"u","mergeCommit":{"oid":"sha"}}]\'; }; '
                    'record_event() { printf "CALLED:%s" "$*"; }; reconcile_dispatches',
                    "test", str(dispatcher_path), str(dispatch_dir), str(rpc),
                    str(log), str(outcome_path),
                ],
                check=True, capture_output=True, text=True,
            )
            self.assertEqual(proc.stdout, "")

    def test_picker_delegates_corrupt_marker_age_to_shared_outcome_policy(self):
        dispatcher = Path(__file__).with_name("coding-dispatch.sh").read_text(encoding="utf-8")
        self.assertNotIn("os.path.isfile(marker_path) and not phase", dispatcher)
        self.assertIn("dispatch_outcome.blocks_redispatch(", dispatcher)

    def test_shell_daily_cap_uses_operator_timezone(self):
        dispatcher_path = Path(__file__).with_name("coding-dispatch.sh")
        with TemporaryDirectory() as td:
            root = Path(td)
            dispatch_dir = root / "coding_dispatch"
            dispatch_dir.mkdir()
            dispatched = datetime.datetime(2026, 7, 14, 23, 30, tzinfo=datetime.timezone.utc)
            now = datetime.datetime(2026, 7, 15, 0, 30, tzinfo=datetime.timezone.utc)
            (dispatch_dir / "kst.json").write_text(json.dumps({
                "dispatchedAt": int(dispatched.timestamp() * 1000),
            }) + "\n", encoding="utf-8")
            proc = subprocess.run(
                [
                    "/bin/bash", "-c",
                    'source "$1"; DENEB_TIMEZONE=Asia/Seoul '
                    'dispatch_cap_usage "$2" "$3" "$4"',
                    "test", str(dispatcher_path), str(dispatch_dir), str(root),
                    str(int(now.timestamp() * 1000)),
                ],
                check=True, capture_output=True, text=True,
            )
            self.assertEqual(proc.stdout.strip(), "2026-07-15\t1\tAsia/Seoul")

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
            self.assertEqual(dispatched["consecutiveFailures"], 2)
            self.assertEqual(dispatched["lastDispatchAtMs"], 3)
            self.assertEqual(completed["consecutiveFailures"], 0)
            self.assertEqual(completed["lastSuccessfulAtMs"], 4)

    def test_busy_tick_does_not_hide_existing_failure_streak(self):
        with TemporaryDirectory() as td:
            path = Path(td) / "status.json"
            record_status(path, "environment_failed", now_ms=1)
            busy = record_status(path, "busy", now_ms=2)
            self.assertEqual(busy["consecutiveFailures"], 1)

    def test_neutral_ticks_preserve_existing_failure_streak(self):
        with TemporaryDirectory() as td:
            path = Path(td) / "status.json"
            record_status(path, "session_failed", now_ms=1)
            for result in ("cap_reached", "idle", "dispatched"):
                status = record_status(path, result, now_ms=2)
                self.assertEqual(status["consecutiveFailures"], 1, result)


if __name__ == "__main__":
    unittest.main()
