"""File-safety and regression-boundary tests for baseline.sh."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from test_shell_support import isolated_env, run_script, write_executable

RESULT_FILE = Path("/tmp/deneb-iterate-result.json")


def run_baseline(command: str, env: dict[str, str]):
    return run_script("scripts/dev/baseline.sh", command, env=env)


def result(
    checks=(),
    *,
    quality=None,
    build_ms=0,
    start_ms=0,
    test_ms=0,
    cleanup_ms=0,
):
    return {
        "version": 1,
        "checks": [{"name": name, "ok": passed} for name, passed in checks],
        "quality": dict(quality or {}),
        "phase": {
            "build": {"ok": True, "ms": build_ms},
            "start": {"ok": True, "ms": start_ms},
            "test": {"ok": True, "ms": test_ms},
            "cleanup": {"ok": True, "ms": cleanup_ms},
        },
    }


class BaselineShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.home.mkdir()
        self.bin.mkdir()
        self.original_result = RESULT_FILE.read_bytes() if RESULT_FILE.exists() else None
        self.addCleanup(self.restore_result)
        RESULT_FILE.unlink(missing_ok=True)

        write_executable(self.bin / "git", """
            #!/usr/bin/env bash
            case "$*" in
              *"rev-parse --abbrev-ref HEAD"*) printf '%s\n' "${FAKE_BRANCH:-feature/health}" ;;
              *"rev-parse --short HEAD"*) printf '%s\n' "${FAKE_COMMIT:-abc1234}" ;;
              *) echo "unexpected git args: $*" >&2; exit 91 ;;
            esac
        """)
        write_executable(self.bin / "date", """
            #!/usr/bin/env bash
            if [[ "$*" == "-u +%Y-%m-%dT%H:%M:%SZ" ]]; then
              echo '2026-07-11T03:00:00Z'
            else
              /bin/date "$@"
            fi
        """)
        self.env = isolated_env(self.home, self.bin)
        self.baseline = self.home / ".deneb/baselines/feature_health.json"

    def restore_result(self) -> None:
        if self.original_result is None:
            RESULT_FILE.unlink(missing_ok=True)
        else:
            RESULT_FILE.write_bytes(self.original_result)

    def write_result(self, value) -> None:
        RESULT_FILE.write_text(json.dumps(value, ensure_ascii=False), encoding="utf-8")

    def write_baseline(self, value) -> None:
        self.baseline.parent.mkdir(parents=True, exist_ok=True)
        self.baseline.write_text(json.dumps(value, ensure_ascii=False), encoding="utf-8")

    def test_unknown_or_missing_command_keeps_usage_and_exit_one(self) -> None:
        for command in ("help", "unknown"):
            with self.subTest(command=command):
                proc = run_baseline(command, self.env)
                self.assertEqual(proc.returncode, 1)
                self.assertIn("Usage: baseline.sh {save|compare|show|history}", proc.stdout)
                self.assertEqual(proc.stderr, "")

    def test_save_without_iteration_result_is_non_destructive(self) -> None:
        proc = run_baseline("save", self.env)
        self.assertEqual(proc.returncode, 1)
        self.assertIn(f"ERROR: no result file found at {RESULT_FILE}", proc.stdout)
        self.assertIn("Run scripts/dev/iterate.sh first", proc.stdout)
        self.assertFalse(self.baseline.exists())

    def test_save_preserves_result_and_adds_stable_branch_metadata(self) -> None:
        current = result(
            [("health", True), ("ready", False)],
            quality={"korean": 75, "label": "한글"},
            build_ms=12,
        )
        self.write_result(current)
        proc = run_baseline("save", self.env)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn("feature_health.json", proc.stdout)
        self.assertIn("branch=feature_health commit=abc1234", proc.stdout)

        saved = json.loads(self.baseline.read_text(encoding="utf-8"))
        self.assertEqual(saved["checks"], current["checks"])
        self.assertEqual(saved["quality"], current["quality"])
        self.assertEqual(saved["baseline_meta"], {
            "branch": "feature_health",
            "commit": "abc1234",
            "saved_at": "2026-07-11T03:00:00Z",
        })

    def test_show_without_baseline_is_clean_and_does_not_create_file(self) -> None:
        proc = run_baseline("show", self.env)
        self.assertEqual(proc.returncode, 0)
        self.assertEqual(proc.stdout.strip(), "no baseline for branch feature_health")
        self.assertFalse(self.baseline.exists())

    def test_show_reports_counts_latency_quality_and_metadata(self) -> None:
        saved = result(
            [("health", True), ("ready", False), ("chat", True)],
            quality={"substance": 88, "safe": True},
            build_ms=100,
            start_ms=200,
            test_ms=300,
            cleanup_ms=4,
        )
        saved["baseline_meta"] = {
            "branch": "feature_health",
            "commit": "deadbee",
            "saved_at": "yesterday",
        }
        self.write_baseline(saved)
        proc = run_baseline("show", self.env)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn("branch:  feature_health", proc.stdout)
        self.assertIn("commit:  deadbee", proc.stdout)
        self.assertIn("saved:   yesterday", proc.stdout)
        self.assertIn("checks:  2/3", proc.stdout)
        self.assertIn("latency: 604ms", proc.stdout)
        self.assertIn('quality: {"substance": 88, "safe": true}', proc.stdout)

    def test_history_handles_empty_directory_and_multiple_saved_branches(self) -> None:
        empty = run_baseline("history", self.env)
        self.assertEqual(empty.returncode, 0)
        self.assertEqual(empty.stdout, "saved baselines:\n")

        baseline_dir = self.baseline.parent
        baseline_dir.mkdir(parents=True, exist_ok=True)
        (baseline_dir / "alpha.json").write_text(json.dumps({
            "baseline_meta": {"commit": "a1", "saved_at": "first"},
        }), encoding="utf-8")
        (baseline_dir / "beta.json").write_text(json.dumps({
            "baseline_meta": {"commit": "b2", "saved_at": "second"},
        }), encoding="utf-8")
        (baseline_dir / "broken.json").write_text("{broken", encoding="utf-8")
        history = run_baseline("history", self.env)
        self.assertEqual(history.returncode, 0)
        self.assertIn("  alpha: a1 first", history.stdout)
        self.assertIn("  beta: b2 second", history.stdout)
        self.assertIn("  broken: ?", history.stdout)

    def test_compare_without_baseline_is_advisory_success(self) -> None:
        self.write_result(result([("health", True)]))
        proc = run_baseline("compare", self.env)
        self.assertEqual(proc.returncode, 0)
        self.assertIn("NO_BASELINE: no baseline saved for branch feature_health", proc.stdout)
        self.assertIn("baseline.sh save", proc.stdout)

    def test_compare_without_current_result_is_failure(self) -> None:
        self.write_baseline(result([("health", True)]))
        proc = run_baseline("compare", self.env)
        self.assertEqual(proc.returncode, 1)
        self.assertIn(f"ERROR: no current result at {RESULT_FILE}", proc.stdout)

    def test_compare_improvements_and_small_drops_do_not_regress(self) -> None:
        base = result(
            [("health", True), ("ready", False)],
            quality={"korean": 70, "minor": 50, "safe": True},
            build_ms=500,
            start_ms=300,
            test_ms=200,
        )
        base["baseline_meta"] = {"commit": "base123", "saved_at": "then"}
        current = result(
            [("health", True), ("ready", True), ("chat", False)],
            quality={"korean": 76, "minor": 46, "safe": True, "new_only": 99},
            build_ms=600,
            start_ms=300,
            test_ms=300,
        )
        self.write_baseline(base)
        self.write_result(current)
        proc = run_baseline("compare", self.env)
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("metric=1→2(+1)", proc.stdout)
        self.assertIn("latency=1000→1200(+200ms)", proc.stdout)
        self.assertIn("korean=70→76(+6)", proc.stdout)
        self.assertIn("minor=50→46(-4)", proc.stdout)
        self.assertIn("safe=True→True", proc.stdout)
        self.assertNotIn("new_only", proc.stdout)
        self.assertIn("baseline: base123 (then)", proc.stdout)
        self.assertIn("REGRESSION: (none)", proc.stdout)

    def test_compare_detects_metric_quality_boolean_and_latency_regressions(self) -> None:
        base = result(
            [("health", True), ("ready", True), ("chat", True)],
            quality={"quality": 90, "safe": True},
            build_ms=400,
            start_ms=300,
            test_ms=300,
        )
        current = result(
            [("health", True), ("ready", False)],
            quality={"quality": 84, "safe": False},
            build_ms=500,
            start_ms=400,
            test_ms=400,
        )
        self.write_baseline(base)
        self.write_result(current)
        proc = run_baseline("compare", self.env)
        self.assertEqual(proc.returncode, 1)
        self.assertIn("metric=3→1(-2)", proc.stdout)
        self.assertIn("quality=90→84(-6)", proc.stdout)
        self.assertIn("safe=True→False", proc.stdout)
        self.assertIn("latency=1000→1300(+300ms)", proc.stdout)
        self.assertIn("metric=-2", proc.stdout)
        self.assertIn("quality=-6", proc.stdout)
        self.assertIn("safe: true→false", proc.stdout)
        self.assertIn("latency=+300ms (+30%)", proc.stdout)

    def test_compare_missing_optional_shapes_default_to_empty_and_zero(self) -> None:
        self.write_baseline({"baseline_meta": {}})
        self.write_result({})
        proc = run_baseline("compare", self.env)
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("metric=0→0(+0)", proc.stdout)
        self.assertIn("latency=0→0(+0ms)", proc.stdout)
        self.assertIn("REGRESSION: (none)", proc.stdout)


if __name__ == "__main__":
    unittest.main()
