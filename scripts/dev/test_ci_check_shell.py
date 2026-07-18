"""Selection, parallel-lane, and failure-report tests for ci-check.sh."""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from test_shell_support import isolated_env, run_script, write_executable


class CICheckShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.android = self.root / "android-sdk"
        self.log = self.root / "calls.log"
        self.home.mkdir()
        self.bin.mkdir()
        self.android.mkdir()

        write_executable(self.bin / "make", """
            #!/usr/bin/env bash
            target="${1:-}"
            printf 'make %s\n' "$*" >> "$FAKE_LOG"
            if [[ "$target" == "${MAKE_FAIL_TARGET:-__none__}" ]]; then
              case "$target" in
                go-fmt)
                  printf 'Go files need formatting:\n  gateway-go/bad.go\nmake: *** [go-fmt] Error 1\n'
                  ;;
                go-lint)
                  printf 'gateway-go/bad.go:4:2: shadowed variable (govet)\n'
                  ;;
                go-test|go-test-cached)
                  printf '%s\n' '--- FAIL: TestBroken (0.00s)' 'FAIL example/pkg' 'make: *** Error 1'
                  ;;
                kotlin-spotless)
                  printf 'format violations:\n  Bad.kt\nRun spotlessApply\n'
                  ;;
                runtime-health-test)
                  printf 'runtime health fixture failed at boundary\n'
                  ;;
                *) printf 'generic failure for %s\n' "$target" ;;
              esac
              exit 1
            fi
            printf 'success %s\n' "$target"
        """)
        write_executable(self.bin / "git", """
            #!/usr/bin/env bash
            printf 'git %s\n' "$*" >> "$FAKE_LOG"
            case "$*" in
              "merge-base HEAD "*)
                [[ "${GIT_BASE_OK:-1}" == 1 ]] || exit 1
                echo base123
                ;;
              "diff --name-only base123 HEAD") printf '%b' "${CHANGED_COMMITTED:-}" ;;
              "diff --name-only") printf '%b' "${CHANGED_UNSTAGED:-}" ;;
              "diff --name-only --cached") printf '%b' "${CHANGED_STAGED:-}" ;;
              "ls-files --others --exclude-standard") printf '%b' "${CHANGED_UNTRACKED:-}" ;;
              *) echo "unexpected git: $*" >&2; exit 88 ;;
            esac
        """)

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "FAKE_LOG": str(self.log),
            "ANDROID_HOME": str(self.android),
            "GIT_BASE_OK": "1",
            "MAKE_FAIL_TARGET": "__none__",
            "TMPDIR": str(self.root),
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def invoke(self, *args: str, env=None):
        return run_script(
            "scripts/dev/ci-check.sh",
            *args,
            env=env or self.env(),
            timeout=20,
        )

    def calls(self) -> list[str]:
        return self.log.read_text(encoding="utf-8").splitlines() if self.log.exists() else []

    def test_help_and_unknown_argument_do_not_start_any_gate(self) -> None:
        help_proc = self.invoke("--help")
        self.assertEqual(help_proc.returncode, 0)
        self.assertIn("local mirror of CI's fast gates", help_proc.stdout)
        self.assertIn("make ci ARGS=--go", help_proc.stdout)
        self.assertEqual(self.calls(), [])

        unknown = self.invoke("--slow")
        self.assertEqual(unknown.returncode, 2)
        self.assertIn("unknown argument '--slow'", unknown.stderr)
        self.assertEqual(self.calls(), [])

    def test_kotlin_preflight_fails_before_make_when_android_sdk_is_missing(self) -> None:
        missing = self.root / "missing-sdk"
        proc = self.invoke("--kotlin", env=self.env(ANDROID_HOME=str(missing)))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("ANDROID_HOME not found", proc.stderr)
        self.assertIn(str(missing), proc.stderr)
        self.assertIn("make ci ARGS=--go", proc.stderr)
        self.assertEqual(self.calls(), [])

    def test_when_audit_lane_runs_all_three_health_gates_and_cleans_logs(self) -> None:
        # health-v2-check is deliberately absent: the pillar ratchet is out of
        # local CI gates (operator decision 2026-07-18 — git-window false reds).
        proc = self.invoke("--audit")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("2 gates, selected lanes run in parallel", proc.stdout)
        for gate in ("runtime-health-test", "health-v2-test"):
            self.assertRegex(proc.stdout, rf"{gate}\s+PASS")
        self.assertIn("2 passed, 0 failed", proc.stdout)
        self.assertIn("make ci PASSED", proc.stdout)
        self.assertEqual(
            self.calls(),
            [
                "make runtime-health-test",
                "make health-v2-test",
            ],
        )
        self.assertEqual(list(self.root.glob("deneb-ci-check.*")), [])

    def test_audit_failure_surfaces_unparsed_log_and_preserves_scratch_path(self) -> None:
        proc = self.invoke(
            "--audit",
            env=self.env(MAKE_FAIL_TARGET="runtime-health-test"),
        )
        self.assertEqual(proc.returncode, 1)
        self.assertIn("runtime-health-test FAIL", proc.stdout)
        self.assertIn("runtime health fixture failed at boundary", proc.stdout)
        self.assertIn("make ci FAILED", proc.stdout)
        dirs = list(self.root.glob("deneb-ci-check.*"))
        self.assertEqual(len(dirs), 1)
        self.assertTrue((dirs[0] / "runtime-health-test.log").exists())

    def test_when_go_lane_runs_generation_substeps_and_all_five_gates(self) -> None:
        proc = self.invoke("--go")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("5 passed, 0 failed", proc.stdout)
        for gate in ("generate-check", "go-fmt", "go-vet", "go-lint", "go-test"):
            self.assertIn(f"{gate}", proc.stdout)
            self.assertIn("PASS", proc.stdout)
        self.assertEqual(self.calls(), [
            "make tool-schemas",
            "make data-gen",
            "make kotlin-models",
            "make go-fmt",
            "make go-vet",
            "make go-lint",
            "make go-test",
        ])

    def test_go_fmt_failure_extracts_file_and_mechanical_fix_hint(self) -> None:
        proc = self.invoke("--go", env=self.env(MAKE_FAIL_TARGET="go-fmt"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("▼ go-fmt", proc.stdout)
        self.assertIn("gateway-go/bad.go", proc.stdout)
        self.assertIn("fix: make fmt", proc.stdout)
        self.assertNotIn("Go files need formatting:", proc.stdout)

    def test_kotlin_lane_runs_exact_four_targets_when_sdk_exists(self) -> None:
        proc = self.invoke("--kotlin")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("4 passed, 0 failed", proc.stdout)
        self.assertEqual(self.calls(), [
            "make kotlin-spotless",
            "make kotlin-detekt",
            "make kotlin-desktop-smoke-test",
            "make kotlin-android-compile",
        ])

    def test_fast_mode_with_irrelevant_changes_exits_without_make(self) -> None:
        proc = self.invoke(
            "--fast",
            env=self.env(CHANGED_UNTRACKED="docs/note.md\\n"),
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("nothing to gate", proc.stdout)
        self.assertIn("run the full", proc.stdout)
        self.assertFalse(any(call.startswith("make ") for call in self.calls()))

    def test_when_fast_mode_merges_all_git_change_surfaces_and_uses_cached_go_tests(self) -> None:
        proc = self.invoke(
            "--fast",
            env=self.env(
                CHANGED_UNSTAGED="gateway-go/internal/a.go\\n",
                CHANGED_STAGED="docs/other.md\\n",
                CHANGED_UNTRACKED="README.tmp\\n",
            ),
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("Go:run  Kotlin:skip  Audit:run", proc.stdout)
        self.assertIn("go-test", proc.stdout)
        self.assertIn("make go-test-cached", self.calls())
        self.assertNotIn("make go-test", self.calls())
        self.assertIn("make ci/fast PASSED", proc.stdout)

    def test_when_fast_mode_can_select_only_runtime_audit_from_untracked_change(self) -> None:
        proc = self.invoke(
            "--fast",
            env=self.env(CHANGED_UNTRACKED="scripts/audit/runtime_health.py\\n"),
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("Go:skip  Kotlin:skip  Audit:run", proc.stdout)
        self.assertEqual(
            [call for call in self.calls() if call.startswith("make ")],
            [
                "make runtime-health-test",
                "make health-v2-test",
            ],
        )

    def test_when_unresolvable_fast_base_warns_and_runs_all_lanes(self) -> None:
        proc = self.invoke("--fast", env=self.env(GIT_BASE_OK="0"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("can't resolve 'origin/main' merge-base", proc.stderr)
        self.assertIn("Go:run  Kotlin:run  Audit:run", proc.stdout)
        self.assertIn("11 passed, 0 failed", proc.stdout)
        make_calls = [call for call in self.calls() if call.startswith("make ")]
        self.assertIn("make go-test-cached", make_calls)
        self.assertIn("make kotlin-android-compile", make_calls)
        self.assertIn("make runtime-health-test", make_calls)


if __name__ == "__main__":
    unittest.main()
