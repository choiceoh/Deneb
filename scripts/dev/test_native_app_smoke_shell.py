"""Navigation, crash-log, liveness, and graceful-degradation tests for native smoke."""

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from test_shell_support import REPO_ROOT, isolated_env, write_executable


class NativeAppSmokeShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.dev = self.root / "scripts/dev"
        self.runtime = self.root / "runtime"
        self.app_log = self.runtime / "app.log"
        self.pid_file = self.runtime / "app_jvm.pid"
        self.shots = self.runtime / "shots"
        self.calls = self.root / "calls.log"
        self.scroll_counter = self.root / "scroll-counter"
        self.error_marker = self.root / "error-written"
        self.assert_counter = self.root / "assert-counter"
        self.home.mkdir()
        self.bin.mkdir()
        self.dev.mkdir(parents=True)
        self.runtime.mkdir()
        self.shots.mkdir()
        self.app_log.write_text("boot complete\n", encoding="utf-8")
        self.pid_file.write_text(f"{os.getpid()}\n", encoding="utf-8")
        shutil.copy2(REPO_ROOT / "scripts/dev/native-app-smoke.sh", self.dev / "native-app-smoke.sh")
        (self.dev / "native-app-smoke.sh").chmod(0o755)
        write_executable(self.bin / "sleep", "#!/usr/bin/env bash\nexit 0\n")
        write_executable(self.dev / "native-app.sh", """
            #!/usr/bin/env bash
            cmd="${1:-}"; shift || true
            printf '%s %s\n' "$cmd" "$*" >> "$FAKE_CALLS"
            case "$cmd" in
              status)
                echo "profile: ${APP_PROFILE:-phone}"
                if [[ "${OMIT_LOG_PATH:-0}" != 1 ]]; then
                  echo "app log: $APP_LOG"
                  echo "shots: $APP_SHOTS"
                fi
                ;;
              start|restart) exit "${START_RC:-0}" ;;
              shot)
                if [[ "${ERROR_ON_SHOT:-}" == "${1:-}" && ! -f "$ERROR_MARKER" ]]; then
                  echo 'Exception in thread "AWT" fixture crash' >> "$APP_LOG"
                  touch "$ERROR_MARKER"
                fi
                ;;
              scroll)
                n=$(cat "$SCROLL_COUNTER" 2>/dev/null || echo 0); n=$((n + 1)); echo "$n" > "$SCROLL_COUNTER"
                if [[ "${ERROR_ON_SCROLL:-0}" == "$n" && ! -f "$ERROR_MARKER" ]]; then
                  echo 'IllegalStateException: scroll crash' >> "$APP_LOG"
                  touch "$ERROR_MARKER"
                fi
                ;;
              assert)
                anchor="$*"
                if [[ -n "${FLAKY_ANCHOR:-}" && "$anchor" == "$FLAKY_ANCHOR" ]]; then
                  n=$(cat "$ASSERT_COUNTER" 2>/dev/null || echo 0); n=$((n + 1)); echo "$n" > "$ASSERT_COUNTER"
                  [[ $n -gt 1 ]] || exit 1
                fi
                if [[ "${FAIL_ALL_ANCHORS:-0}" == 1 ]]; then
                  if [[ "$anchor" == "다시 시도" && "${RETRY_AVAILABLE:-0}" == 1 ]]; then exit 0; fi
                  exit 1
                fi
                [[ -z "${FAIL_ANCHOR:-}" || "$anchor" != "$FAIL_ANCHOR" ]]
                ;;
            esac
        """)

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "HOME": str(self.home),
            "FAKE_CALLS": str(self.calls),
            "APP_LOG": str(self.app_log),
            "APP_SHOTS": str(self.shots),
            "APP_PROFILE": "phone",
            "START_RC": "0",
            "OMIT_LOG_PATH": "0",
            "ERROR_MARKER": str(self.error_marker),
            "SCROLL_COUNTER": str(self.scroll_counter),
            "ASSERT_COUNTER": str(self.assert_counter),
            "ERROR_ON_SHOT": "",
            "ERROR_ON_SCROLL": "0",
            "FAIL_ANCHOR": "",
            "FAIL_ALL_ANCHORS": "0",
            "RETRY_AVAILABLE": "0",
            "FLAKY_ANCHOR": "",
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def invoke(self, env=None):
        return subprocess.run(
            [str(self.dev / "native-app-smoke.sh")],
            cwd=self.root,
            env=env or self.env(),
            capture_output=True,
            text=True,
            timeout=20,
            check=False,
        )

    def call_lines(self) -> list[str]:
        return self.calls.read_text(encoding="utf-8").splitlines() if self.calls.exists() else []

    def test_when_clean_phone_walk_passes_all_primary_secondary_and_detail_screens(self) -> None:
        proc = self.invoke()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("PASS — every key screen rendered", proc.stdout)
        self.assertIn(f"shots: {self.shots}/smoke-*.png", proc.stdout)
        calls = self.call_lines()
        self.assertIn("start phone", calls)
        self.assertNotIn("restart phone", calls)
        for shot in (
            "smoke-01-feed",
            "smoke-03-mail",
            "smoke-06-categories",
            "smoke-07-people",
            "smoke-13-mail-detail",
        ):
            self.assertIn(f"shot {shot}", calls)
        self.assertIn("tap 25 37", calls)
        self.assertIn("assert 대화 기록", calls)
        self.assertGreaterEqual(calls.count("scroll down 6"), 12)

    def test_nonphone_profile_restarts_into_phone_before_walk(self) -> None:
        proc = self.invoke(self.env(APP_PROFILE="desktop"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = self.call_lines()
        self.assertIn("restart phone", calls)
        self.assertNotIn("start phone", calls)

    def test_boot_failure_exits_before_status_resolution_or_navigation(self) -> None:
        proc = self.invoke(self.env(START_RC="9"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("native-app.sh start failed", proc.stdout)
        calls = self.call_lines()
        self.assertEqual(calls.count("status "), 1)
        self.assertFalse(any(line.startswith("tap ") for line in calls))

    def test_missing_log_path_from_status_is_an_actionable_harness_failure(self) -> None:
        proc = self.invoke(self.env(OMIT_LOG_PATH="1"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("could not resolve app log path", proc.stdout)
        self.assertFalse(any(line.startswith("shot ") for line in self.call_lines()))

    def test_when_preexisting_crash_line_is_not_misattributed_to_new_screen(self) -> None:
        self.app_log.write_text(
            "IllegalArgumentException: historical before smoke\nboot recovered\n",
            encoding="utf-8",
        )
        proc = self.invoke()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertNotIn("new crash-class log lines", proc.stdout)

    def test_new_crash_line_after_screen_navigation_fails_that_screen(self) -> None:
        proc = self.invoke(self.env(ERROR_ON_SHOT="smoke-03-mail"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("✗ smoke-03-mail — new crash-class log lines", proc.stdout)
        self.assertIn("Exception in thread", proc.stdout)
        self.assertIn("FAIL  smoke-03-mail", proc.stdout)
        self.assertIn("FAIL — a screen crashed / went wrong", proc.stdout)

    def test_dead_app_pid_is_reported_even_when_log_and_anchor_are_clean(self) -> None:
        self.pid_file.write_text("99999999\n", encoding="utf-8")
        proc = self.invoke()
        self.assertEqual(proc.returncode, 1)
        self.assertIn("app JVM (pid 99999999) is gone", proc.stdout)
        self.assertIn("DEAD  smoke-01-feed", proc.stdout)

    def test_missing_data_anchor_accepts_visible_retry_affordance(self) -> None:
        proc = self.invoke(self.env(FAIL_ALL_ANCHORS="1", RETRY_AVAILABLE="1"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("rendered cleanly (load failed → 다시 시도", proc.stdout)
        self.assertIn("ok    smoke-03-mail (no data)", proc.stdout)

    def test_missing_anchor_without_retry_affordance_is_wrong_screen(self) -> None:
        proc = self.invoke(self.env(FAIL_ALL_ANCHORS="1", RETRY_AVAILABLE="0"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("wrong screen / blank render?", proc.stdout)
        self.assertIn("WRONG smoke-03-mail", proc.stdout)

    def test_scroll_probe_detects_below_fold_crash_and_stops_that_probe(self) -> None:
        proc = self.invoke(self.env(ERROR_ON_SCROLL="2"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("scroll-smoke-01-feed — crash while scrolling (step 2)", proc.stdout)
        self.assertIn("IllegalStateException: scroll crash", proc.stdout)
        self.assertIn("FAIL  scroll-smoke-01-feed (step 2)", proc.stdout)

    def test_when_flaky_anchor_retries_navigation_once_then_passes(self) -> None:
        proc = self.invoke(self.env(FLAKY_ANCHOR="카테고리"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = self.call_lines()
        self.assertGreaterEqual(calls.count("taptext 카테고리"), 2)
        self.assertGreaterEqual(int(self.assert_counter.read_text()), 2)


if __name__ == "__main__":
    unittest.main()
