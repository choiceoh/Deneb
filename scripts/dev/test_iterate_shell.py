"""One-shot build/start/metric/result tests for the iteration orchestrator."""

from __future__ import annotations

import json
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from test_shell_support import REPO_ROOT, isolated_env, write_executable


class IterateShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "fixture"
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.dev = self.root / "scripts/dev"
        self.state = self.root / "state"
        self.calls = self.root / "calls.log"
        self.home.mkdir(parents=True)
        self.bin.mkdir()
        self.dev.mkdir(parents=True)
        self.state.mkdir()
        shutil.copy2(REPO_ROOT / "scripts/dev/iterate.sh", self.dev / "iterate.sh")
        (self.dev / "iterate.sh").chmod(0o755)
        write_executable(self.dev / "lib-server.sh", """
            #!/usr/bin/env bash
            DEVLIB_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
            DEVLIB_REPO_DIR="$(cd "$DEVLIB_SCRIPT_DIR/../.." && pwd)"
            DEVLIB_TMP_PREFIX="$FIXTURE_STATE/deneb"
            DEVLIB_ITERATE_PORT=18791
            DEVLIB_HOST=127.0.0.1

            devlib_build() {
              printf 'build binary=%s repo=%s\n' "$1" "${2:-}" >> "$FAKE_CALLS"
              if [[ "${BUILD_OK:-1}" != 1 ]]; then
                errors="${BUILD_ERROR_CONTENT:-}"
                if [[ -z "$errors" ]]; then
                  errors='gateway-go/main.go:12:3: fixture compile failure\ngateway-go/other.go:9:2: second error\n'
                fi
                printf '%b' "$errors" >&2
                return 2
              fi
              printf '#!/usr/bin/env bash\nexit 0\n' > "$1"; chmod +x "$1"
            }
            devlib_gen_config() {
              printf 'config out=%s\n' "$1" >> "$FAKE_CALLS"
              printf '{}\n' > "$1"
            }
            devlib_start_gateway() {
              printf 'start binary=%s port=%s config=%s state=%s log=%s\n' \
                "$1" "$2" "$3" "$4" "$5" >> "$FAKE_CALLS"
              mkdir -p "$(dirname "$5")"
              printf '%b' "${GATEWAY_LOG_CONTENT:-}" > "$5"
              DEVLIB_PID=424242
            }
            devlib_wait_healthy() {
              printf 'wait-healthy host=%s port=%s retries=%s\n' "$1" "$2" "$3" >> "$FAKE_CALLS"
              [[ "${START_OK:-1}" == 1 ]]
            }
            devlib_stop_pid() {
              printf 'stop pid=%s\n' "$1" >> "$FAKE_CALLS"
            }
            devlib_wait_port_free() {
              printf 'wait-port port=%s\n' "$1" >> "$FAKE_CALLS"
              return "${PORT_FREE_RC:-0}"
            }
        """)
        write_executable(self.bin / "flock", """
            #!/usr/bin/env bash
            printf 'flock %s\n' "$*" >> "$FAKE_CALLS"
            exit "${FLOCK_RC:-0}"
        """)
        write_executable(self.bin / "git", """
            #!/usr/bin/env bash
            printf 'git %s\n' "$*" >> "$FAKE_CALLS"
            case "$*" in
              *"rev-parse --short HEAD"*) echo abc1234 ;;
              *"rev-parse --abbrev-ref HEAD"*) echo codex/iterate ;;
              *) exit 88 ;;
            esac
        """)
        write_executable(self.bin / "curl", """
            #!/usr/bin/env bash
            printf 'curl %s\n' "$*" >> "$FAKE_CALLS"
            if [[ "$*" == *"/ready"* ]]; then
              [[ "${READY_OK:-1}" == 1 ]] && { printf '200'; exit 0; }
              printf '000'; exit 22
            fi
            if [[ "$*" == *"/health"* ]]; then
              [[ "${HEALTH_OK:-1}" == 1 ]] && { printf '{"status":"ok"}'; exit 0; }
              exit 22
            fi
            exit 87
        """)
        write_executable(self.bin / "bc", """
            #!/usr/bin/env bash
            read -r expression
            case "$expression" in
              "0 > 0"|"0.0 > 0") echo 0 ;;
              *) echo 1 ;;
            esac
        """)
        write_executable(self.bin / "custom-metric", """
            #!/usr/bin/env bash
            printf '%b' "${CUSTOM_METRIC_OUTPUT:-metric_value=42\\n}"
            exit "${CUSTOM_METRIC_RC:-0}"
        """)
        write_executable(self.dev / "quality-metric.sh", """
            #!/usr/bin/env bash
            printf 'quality-metric\n' >> "$FAKE_CALLS"
            printf 'metric_value=%s\n' "${QUALITY_VALUE:-75}"
            printf 'DENEB_METRIC_DETAIL korean=9 substance=8 errors=0\n'
        """)
        write_executable(self.dev / "baseline.sh", """
            #!/usr/bin/env bash
            printf 'baseline %s\n' "$*" >> "$FAKE_CALLS"
            echo "baseline-$1"
            exit "${BASELINE_RC:-0}"
        """)

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "HOME": str(self.home),
            "FIXTURE_STATE": str(self.state),
            "FAKE_CALLS": str(self.calls),
            "BUILD_OK": "1",
            "BUILD_ERROR_CONTENT": (
                "gateway-go/main.go:12:3: fixture compile failure\n"
                "gateway-go/other.go:9:2: second error\n"
            ),
            "START_OK": "1",
            "PORT_FREE_RC": "0",
            "FLOCK_RC": "0",
            "HEALTH_OK": "1",
            "READY_OK": "1",
            "QUALITY_VALUE": "75",
            "CUSTOM_METRIC_OUTPUT": "metric_value=42\n",
            "CUSTOM_METRIC_RC": "0",
            "GATEWAY_LOG_CONTENT": "",
            "BASELINE_RC": "0",
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def invoke(self, *args: str, env=None):
        return subprocess.run(
            [str(self.dev / "iterate.sh"), *args],
            cwd=self.root,
            env=env or self.env(),
            capture_output=True,
            text=True,
            timeout=20,
            check=False,
        )

    def call_lines(self) -> list[str]:
        return self.calls.read_text(encoding="utf-8").splitlines() if self.calls.exists() else []

    def result_json(self, proc) -> dict:
        line = next(line for line in proc.stdout.splitlines() if line.startswith("DENEB_TEST_JSON "))
        return json.loads(line.removeprefix("DENEB_TEST_JSON "))

    def legacy_line(self, proc) -> str:
        return next(line for line in proc.stdout.splitlines() if line.startswith("ITERATE_RESULT "))

    def test_concurrent_lock_failure_is_fast_and_does_not_build(self) -> None:
        proc = self.invoke(env=self.env(FLOCK_RC="1"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("BLOCKED: another iterate.sh is running", proc.stdout)
        self.assertIn(
            "ITERATE_RESULT metric=0 build=skip server=skip checks=0/0 latency_ms=0",
            proc.stdout,
        )
        self.assertEqual(self.call_lines(), ["flock -n 200"])

    def test_build_failure_emits_bounded_diagnostics_and_structured_phase(self) -> None:
        proc = self.invoke(env=self.env(BUILD_OK="0"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("build... FAIL", proc.stdout)
        self.assertIn("first error: gateway-go/main.go:12:3: fixture compile failure", proc.stdout)
        self.assertIn("total errors: 2", proc.stdout)
        self.assertIn("ITERATE_RESULT metric=0 build=fail server=skip checks=0/0", proc.stdout)
        data = self.result_json(proc)
        self.assertFalse(data["phase"]["build"]["ok"])
        self.assertFalse(data["phase"]["start"]["ok"])
        self.assertEqual(data["diagnostics"]["category"], "build_error")
        self.assertEqual(data["diagnostics"]["error_count"], 2)
        self.assertNotIn("start binary=", "\n".join(self.call_lines()))

    def test_non_go_build_failure_keeps_zero_error_count_and_valid_json(self) -> None:
        proc = self.invoke(env=self.env(
            BUILD_OK="0",
            BUILD_ERROR_CONTENT="linker unavailable for target architecture\n",
        ))
        self.assertEqual(proc.returncode, 1)
        self.assertNotIn("first error:", proc.stdout)
        self.assertIn("linker unavailable for target architecture", proc.stdout)
        data = self.result_json(proc)
        self.assertEqual(data["diagnostics"]["error_count"], 0)
        self.assertEqual(data["diagnostics"]["first_error"], "")
        self.assertEqual(data["diagnostics"]["category"], "build_error")

    def test_start_crash_is_distinguished_from_build_failure(self) -> None:
        proc = self.invoke(env=self.env(START_OK="0"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("FAIL (start_crash:", proc.stdout)
        self.assertIn("Process exited with code", proc.stdout)
        self.assertIn("ITERATE_RESULT metric=0 build=ok server=fail", proc.stdout)
        data = self.result_json(proc)
        self.assertTrue(data["phase"]["build"]["ok"])
        self.assertFalse(data["phase"]["start"]["ok"])
        self.assertEqual(data["diagnostics"]["category"], "start_crash")

    def test_default_smoke_runs_health_and_ready_and_serializes_both_checks(self) -> None:
        proc = self.invoke()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("smoke... 2/2", proc.stdout)
        self.assertIn("metric=2 build=ok server=ok checks=2/2", self.legacy_line(proc))
        data = self.result_json(proc)
        self.assertEqual(data["version"], 1)
        self.assertEqual(data["commit"], "abc1234")
        self.assertEqual(data["branch"], "codex/iterate")
        self.assertTrue(data["prod_config"])
        self.assertTrue(data["phase"]["build"]["ok"])
        self.assertTrue(data["phase"]["start"]["ok"])
        self.assertTrue(data["phase"]["test"]["ok"])
        self.assertEqual(
            [(item["name"], item["ok"], item["detail"]) for item in data["checks"]],
            [("health", True, "status=ok"), ("ready", True, "http=200")],
        )
        self.assertIn("stop pid=424242", self.call_lines())

    def test_partial_smoke_failure_is_machine_visible_in_text_and_json(self) -> None:
        proc = self.invoke(env=self.env(HEALTH_OK="0", READY_OK="1"))
        self.assertEqual(proc.returncode, 0)
        self.assertIn("smoke... 1/2", proc.stdout)
        self.assertIn("FAILED: health (status=", proc.stdout)
        self.assertIn("metric=1 build=ok server=ok checks=1/2", self.legacy_line(proc))
        data = self.result_json(proc)
        self.assertFalse(data["phase"]["test"]["ok"])
        self.assertEqual([check["ok"] for check in data["checks"]], [False, True])

    def test_custom_metric_extracts_last_value_and_key_value_detail(self) -> None:
        output = (
            "metric_value=10\n"
            "noise\n"
            "DENEB_METRIC_DETAIL korean=9 label=good errors=0\n"
            "metric_value=87.5\n"
        )
        proc = self.invoke("--metric", "custom-metric", env=self.env(CUSTOM_METRIC_OUTPUT=output))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("metric... 87.5", proc.stdout)
        self.assertIn("metric=87.5 build=ok server=ok checks=1/1", self.legacy_line(proc))
        data = self.result_json(proc)
        self.assertEqual(data["quality"], {"korean": 9, "label": "good", "errors": 0})
        self.assertTrue(data["phase"]["test"]["ok"])

    def test_custom_metric_uses_last_detail_line_and_keeps_float_values_as_strings(self) -> None:
        output = (
            "DENEB_METRIC_DETAIL stale=1\n"
            "metric_value=55\n"
            "DENEB_METRIC_DETAIL exact=7 ratio=0.75 state=ready\n"
        )
        proc = self.invoke(
            "--metric", "custom-metric",
            env=self.env(CUSTOM_METRIC_OUTPUT=output),
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertEqual(
            self.result_json(proc)["quality"],
            {"exact": 7, "ratio": "0.75", "state": "ready"},
        )
        self.assertNotIn("stale", self.result_json(proc)["quality"])

    def test_custom_metric_without_machine_value_is_zero_and_failed_phase(self) -> None:
        proc = self.invoke(
            "--metric", "custom-metric",
            env=self.env(CUSTOM_METRIC_OUTPUT="only diagnostics\n", CUSTOM_METRIC_RC="4"),
        )
        self.assertEqual(proc.returncode, 0)
        self.assertIn("FAIL (no metric_value in output)", proc.stdout)
        self.assertIn("metric=0 build=ok server=ok checks=0/1", self.legacy_line(proc))
        self.assertFalse(self.result_json(proc)["phase"]["test"]["ok"])

    def test_quality_preset_invokes_checked_in_metric_and_preserves_detail(self) -> None:
        proc = self.invoke("--metric", "quality", env=self.env(QUALITY_VALUE="73"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("metric... 73", proc.stdout)
        self.assertIn("quality-metric", self.call_lines())
        self.assertEqual(
            self.result_json(proc)["quality"],
            {"korean": 9, "substance": 8, "errors": 0},
        )

    def test_combined_preset_weights_smoke_twenty_and_quality_eighty(self) -> None:
        proc = self.invoke("--metric", "combined", env=self.env(QUALITY_VALUE="75"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("metric(combined)... 80", proc.stdout)
        self.assertIn("metric=80 build=ok server=ok checks=1/1", self.legacy_line(proc))
        self.assertIn("quality-metric", self.call_lines())

    def test_combined_preset_skips_quality_when_both_smoke_probes_fail(self) -> None:
        proc = self.invoke(
            "--metric", "combined",
            env=self.env(HEALTH_OK="0", READY_OK="0", QUALITY_VALUE="99"),
        )
        self.assertEqual(proc.returncode, 0)
        self.assertIn("0 (smoke failed)", proc.stdout)
        self.assertNotIn("quality-metric", self.call_lines())
        self.assertFalse(self.result_json(proc)["phase"]["test"]["ok"])

    def test_gateway_log_errors_are_counted_and_echoed_without_hiding_result(self) -> None:
        log_content = (
            '{"level":"error","msg":"first"}\n'
            "panic: fixture panic\n"
            "ordinary line\n"
        )
        proc = self.invoke(env=self.env(GATEWAY_LOG_CONTENT=log_content))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("log errors: 2", proc.stdout)
        self.assertIn("panic: fixture panic", proc.stdout)
        self.assertEqual(self.result_json(proc)["log_errors"], 2)

    def test_baseline_compare_and_save_flags_run_after_result_and_are_advisory(self) -> None:
        proc = self.invoke(
            "--baseline", "--save-baseline",
            env=self.env(BASELINE_RC="7"),
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("baseline-compare", proc.stdout)
        self.assertIn("baseline-save", proc.stdout)
        calls = self.call_lines()
        self.assertLess(calls.index("baseline compare"), calls.index("baseline save"))

    def test_port_override_and_deprecated_prod_parity_reach_start_contract(self) -> None:
        proc = self.invoke("--port", "19991", "--prod-parity")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = "\n".join(self.call_lines())
        self.assertIn("start binary=", calls)
        self.assertIn("port=19991", calls)
        self.assertIn("wait-healthy host=127.0.0.1 port=19991 retries=30", calls)

    def test_cleanup_warns_when_port_remains_held_but_result_still_emits(self) -> None:
        proc = self.invoke(env=self.env(PORT_FREE_RC="1"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("WARN: port 18791 still held", proc.stderr)
        self.assertIn("DENEB_TEST_JSON", proc.stdout)
        self.assertIn("wait-port port=18791", self.call_lines())


if __name__ == "__main__":
    unittest.main()
