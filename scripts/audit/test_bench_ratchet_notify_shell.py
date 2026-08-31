"""Operator-routing contract for bench-ratchet-notify.sh.

The RSI ratchet check has run daily since the bench-refresh timer was installed
and has exited 1 on a breach the whole time, on purpose. Nobody heard it: the
verdict went to a file no code reads and to `systemctl status`, and twelve
consecutive red runs (2026-08-20 -> 08-31) passed unnoticed. The notifier is
the half that carries the verdict out, so its contract is worth pinning:

  * one issue, refreshed in place with a streak counter (a persistent breach is
    the NORMAL case here -- a fresh comment every night buries it instead),
  * auto-closed on green, so open/closed IS the live ratchet state,
  * never non-zero, because a notification must not turn a green refresh red
    nor mask the breach exit code the refresh already raises.

The gh calls are stubbed; nothing here reaches GitHub, and every write lands in
a temp dir.
"""

from __future__ import annotations

import subprocess
import tempfile
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]

GH_STUB = r"""
#!/usr/bin/env bash
printf 'gh %s\n' "$*" >> "$GH_CALLS"
case "$1 $2" in
  "auth status") exit 0 ;;
  "issue list") printf '%s\n' "${STUB_EXISTING:-}" ;;
  "issue view") printf '%s\n' "${STUB_BODY:-}" ;;
  "issue create") printf 'https://github.com/choiceoh/Deneb/issues/9999\n' ;;
esac
exit 0
"""

REPORT = """RSI Bench  overall = 49.0/100  (rubric 1.3.0, profile deep)
DENEB_RSI_BENCH score=49.0 profile=deep rubric=1.3.0 process=41.7 utility=59.7
REGRESSION: pillar process.anti-collapse 42.0 < baseline 48.0 (tol 1.0)
UNMEASURED: pillar process.acceptor-trust 28.0 < baseline 55.0 (tol 1.0)
"""


class BenchRatchetNotifyShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "fixture"
        self.bin = self.root / "bin"
        self.state = self.root / "state"
        audit = self.root / "scripts/audit"
        audit.mkdir(parents=True)
        self.bin.mkdir(parents=True)
        (self.state / "data").mkdir(parents=True)

        # REPO_DIR is derived as <script>/../.., so the script must sit at
        # <root>/scripts/audit/ for the fixture to be a plausible repo root.
        self.script = audit / "bench-ratchet-notify.sh"
        self.script.write_text(
            (REPO_ROOT / "scripts/audit/bench-ratchet-notify.sh").read_text(encoding="utf-8"),
            encoding="utf-8",
        )
        self.script.chmod(0o755)

        self.calls = self.root / "gh-calls.log"
        gh = self.bin / "gh"
        gh.write_text(GH_STUB.lstrip("\n"), encoding="utf-8")
        gh.chmod(0o755)

        self.report = self.state / "data/rsi-bench-check-latest.txt"
        self.report.write_text(REPORT, encoding="utf-8")

    def run_notify(self, mode: str, **env: str) -> tuple[int, str, str]:
        result = subprocess.run(
            [str(self.script), mode],
            capture_output=True,
            text=True,
            env={
                "PATH": f"{self.bin}:/usr/bin:/bin",
                "HOME": str(self.root),
                "DENEB_STATE_DIR": str(self.state),
                "GH_CALLS": str(self.calls),
                **env,
            },
            check=False,
        )
        calls = self.calls.read_text(encoding="utf-8") if self.calls.exists() else ""
        return result.returncode, result.stdout, calls

    def test_first_breach_opens_one_labelled_issue_carrying_the_regression_lines(self) -> None:
        rc, out, calls = self.run_notify("breach", STUB_EXISTING="")
        self.assertEqual(rc, 0)
        self.assertIn("issue create", calls)
        self.assertNotIn("issue edit", calls)
        self.assertIn("--label rsi-bench", calls)
        self.assertIn("연속 회귀: 1일차", calls)
        # The operator must see WHICH pillar broke, not just that something did.
        self.assertIn("process.anti-collapse 42.0 < baseline 48.0", calls)
        self.assertIn("opened 9999", out)

    def test_repeat_breach_refreshes_the_same_issue_and_advances_the_streak(self) -> None:
        rc, out, calls = self.run_notify(
            "breach", STUB_EXISTING="4300", STUB_BODY="연속 회귀: 7일차\n\nold body"
        )
        self.assertEqual(rc, 0)
        self.assertIn("issue edit 4300", calls)
        self.assertNotIn("issue create", calls)
        # The streak is the only part of a repeat report that carries new
        # information; an identical daily comment would bury the issue.
        self.assertIn("연속 회귀: 8일차", calls)
        self.assertIn("streak 8d", out)

    def test_green_closes_the_open_issue_so_state_tracks_the_ratchet(self) -> None:
        rc, out, calls = self.run_notify("green", STUB_EXISTING="4300")
        self.assertEqual(rc, 0)
        self.assertIn("issue comment 4300", calls)
        self.assertIn("issue close 4300", calls)
        self.assertIn("closed #4300", out)

    def test_green_with_nothing_open_touches_nothing(self) -> None:
        rc, _out, calls = self.run_notify("green", STUB_EXISTING="")
        self.assertEqual(rc, 0)
        self.assertNotIn("issue close", calls)
        self.assertNotIn("issue comment", calls)

    def test_missing_gh_degrades_quietly_instead_of_failing_the_unit(self) -> None:
        result = subprocess.run(
            [str(self.script), "breach"],
            capture_output=True,
            text=True,
            env={
                "PATH": "/usr/bin:/bin",
                "HOME": str(self.root),
                "DENEB_STATE_DIR": str(self.state),
                "GH_CALLS": str(self.calls),
            },
            check=False,
        )
        self.assertEqual(result.returncode, 0)
        self.assertIn("gh not installed", result.stderr)

    def test_breach_without_a_report_still_reports(self) -> None:
        # OnFailure= fires for ANY failure of the refresh unit -- a health-v3
        # crash or the 45min timeout lands here too, before the RSI check ever
        # wrote its file. Say so rather than asserting a breach that may not
        # have happened.
        self.report.unlink()
        rc, _out, calls = self.run_notify("breach", STUB_EXISTING="")
        self.assertEqual(rc, 0)
        self.assertIn("issue create", calls)
        self.assertIn("the refresh failed before the RSI check ran", calls)

    def test_unknown_mode_is_a_no_op(self) -> None:
        rc, _out, calls = self.run_notify("bogus")
        self.assertEqual(rc, 0)
        self.assertNotIn("gh ", calls)


if __name__ == "__main__":
    unittest.main()
