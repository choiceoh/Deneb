"""Lifecycle, smoke, model, log, and delegation tests for live-test.sh."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from test_shell_support import REPO_ROOT, isolated_env, write_executable


class LiveTestShellTests(unittest.TestCase):
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
        shutil.copy2(REPO_ROOT / "scripts/dev/live-test.sh", self.dev / "live-test.sh")
        (self.dev / "live-test.sh").chmod(0o755)

        write_executable(self.dev / "lib-server.sh", r"""
#!/usr/bin/env bash
DEVLIB_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEVLIB_REPO_DIR="$(cd "$DEVLIB_SCRIPT_DIR/../.." && pwd)"
DEVLIB_INSTANCE="${DENEB_INSTANCE:-fixture}"
DEVLIB_TMP_PREFIX="$FIXTURE_STATE/deneb"
DEVLIB_LIVE_PORT=18790
DEVLIB_HOST=127.0.0.1
devlib_load_dotenv() { printf 'load-dotenv\n' >> "$FAKE_CALLS"; }
devlib_version() { printf '9.8.7\n'; }
devlib_build() {
  printf 'build %s\n' "$1" >> "$FAKE_CALLS"
  [[ "${BUILD_OK:-1}" == 1 ]] || return 17
  mkdir -p "$(dirname "$1")"
  printf '#!/usr/bin/env bash\nexit 0\n' > "$1"
  chmod +x "$1"
}
devlib_gen_config() {
  printf 'gen-config %s\n' "$1" >> "$FAKE_CALLS"
  printf '{"generated":true}\n' > "$1"
}
devlib_start_gateway() {
  printf 'start binary=%s port=%s config=%s state=%s log=%s mode=%s\n' \
    "$1" "$2" "$3" "$4" "$5" "$6" >> "$FAKE_CALLS"
  DEVLIB_PID=888
}
devlib_wait_healthy() {
  printf 'wait-healthy %s %s %s\n' "$1" "$2" "$3" >> "$FAKE_CALLS"
  [[ "${WAIT_HEALTHY:-1}" == 1 ]]
}
devlib_stop_pid() { printf 'stop-pid %s\n' "$1" >> "$FAKE_CALLS"; }
devlib_wait_port_free() {
  printf 'wait-port %s\n' "$1" >> "$FAKE_CALLS"
  [[ "${PORT_FREE:-1}" == 1 ]]
}
""")
        write_executable(self.bin / "curl", r"""
#!/usr/bin/env bash
printf 'curl %s\n' "$*" >> "$FAKE_CALLS"
[[ "${CURL_OK:-1}" == 1 ]] || exit 22
if [[ "$*" == *'/admin/model'* && "$*" == *'-X PUT'* ]]; then
  printf '%s' "$SWITCH_JSON"
elif [[ "$*" == *'/admin/model'* ]]; then
  printf '%s' "$MODEL_JSON"
elif [[ "$*" == *'/ready'* ]]; then
  printf '%s' "${READY_CODE:-200}"
elif [[ "$*" == *'/health'* ]]; then
  printf '%s' "$HEALTH_JSON"
else
  exit 23
fi
""")
        helper = r"""
#!/usr/bin/env python3
import os, sys
with open(os.environ["FAKE_CALLS"], "a", encoding="utf-8") as stream:
    stream.write(os.path.basename(sys.argv[0]) + " " + " ".join(sys.argv[1:]) + "\n")
print("delegate-ok")
"""
        for name in ("quality-test.py", "reproduce.py"):
            write_executable(self.dev / name, helper)
        shell_helper = r"""
#!/usr/bin/env bash
printf '%s %s\n' "$(basename "$0")" "$*" >> "$FAKE_CALLS"
printf 'delegate-ok\n'
"""
        for name in ("autoresearch.sh", "ar-results.sh", "baseline.sh"):
            write_executable(self.dev / name, shell_helper)

    @property
    def prefix(self) -> Path:
        return self.state / "deneb"

    @property
    def binary(self) -> Path:
        return Path(f"{self.prefix}-gateway-live")

    @property
    def pidfile(self) -> Path:
        return Path(f"{self.prefix}-gateway-live.pid")

    @property
    def logfile(self) -> Path:
        return Path(f"{self.prefix}-gateway-live.log")

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "FIXTURE_STATE": str(self.state),
            "FAKE_CALLS": str(self.calls),
            "DENEB_INSTANCE": "fixture",
            "BUILD_OK": "1",
            "WAIT_HEALTHY": "1",
            "PORT_FREE": "1",
            "CURL_OK": "1",
            "READY_CODE": "200",
            "HEALTH_JSON": '{"status":"ok","version":"fixture"}',
            "MODEL_JSON": '{"current":"main/model","available":'
            '[{"role":"main","full_id":"main/model"},'
            '{"role":"fallback","full_id":"backup/model"}]}',
            "SWITCH_JSON": '{"previous":"old/model","current":"new/model"}',
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def invoke(self, *args: str, env=None) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [str(self.dev / "live-test.sh"), *args],
            cwd=self.root,
            env=env or self.env(),
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        )

    def call_lines(self) -> list[str]:
        return self.calls.read_text(encoding="utf-8").splitlines() if self.calls.exists() else []

    def seed_running(self) -> None:
        self.pidfile.write_text(f"{os.getpid()}\n", encoding="utf-8")

    def test_default_help_unknown_command_and_legacy_global_flag_are_side_effect_free(self) -> None:
        default = self.invoke()
        self.assertEqual(default.returncode, 0)
        self.assertIn("Usage: scripts/dev/live-test.sh COMMAND", default.stdout)
        self.assertIn("네이티브 miniapp RPC", default.stdout)

        unknown = self.invoke("does-not-exist")
        self.assertEqual(unknown.returncode, 0)
        self.assertIn("Lifecycle:", unknown.stdout)

        filtered = self.invoke("--prod-parity", "status")
        self.assertEqual(filtered.returncode, 0)
        self.assertIn("Dev gateway: STOPPED", filtered.stdout)
        self.assertEqual(self.call_lines(), ["load-dotenv", "load-dotenv", "load-dotenv"])

    def test_build_creates_executable_and_reports_binary_size(self) -> None:
        proc = self.invoke("build")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertTrue(os.access(self.binary, os.X_OK))
        self.assertIn("Building gateway from fixture", proc.stdout)
        self.assertIn(f"Binary: {self.binary}", proc.stdout)
        self.assertIn(f"build {self.binary}", self.call_lines())

    def test_build_failure_propagates_without_claiming_a_binary(self) -> None:
        proc = self.invoke("build", env=self.env(BUILD_OK="0"))
        self.assertEqual(proc.returncode, 17)
        self.assertFalse(self.binary.exists())
        self.assertNotIn("Binary:", proc.stdout)

    def test_start_builds_generates_config_and_passes_isolated_state_to_gateway(self) -> None:
        proc = self.invoke("start")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("No binary found, building first", proc.stdout)
        self.assertIn("Config: production", proc.stdout)
        self.assertIn("Running (PID 888, port 18790)", proc.stdout)
        self.assertEqual(self.pidfile.read_text(), "888\n")
        config = Path(f"{self.prefix}-dev-config.json")
        self.assertEqual(json.loads(config.read_text()), {"generated": True})
        calls = "\n".join(self.call_lines())
        self.assertIn(f"gen-config {config}", calls)
        self.assertIn(f"state={self.prefix}-dev-state", calls)
        self.assertIn(f"log={self.logfile} mode=nohup", calls)
        self.assertIn("wait-healthy 127.0.0.1 18790 25", calls)

    def test_start_short_circuits_for_live_pid_and_unhealthy_start_keeps_pid_for_logs(self) -> None:
        self.seed_running()
        running = self.invoke("start")
        self.assertEqual(running.returncode, 0)
        self.assertIn(f"already running (PID {os.getpid()})", running.stdout)
        self.assertNotIn("build ", "\n".join(self.call_lines()))

        self.pidfile.unlink()
        self.calls.unlink()
        unhealthy = self.invoke("start", env=self.env(WAIT_HEALTHY="0"))
        self.assertEqual(unhealthy.returncode, 1)
        self.assertIn("WARN: Gateway started but /health not responding", unhealthy.stdout)
        self.assertEqual(self.pidfile.read_text(), "888\n")

    def test_stop_handles_stopped_running_and_busy_port_states(self) -> None:
        stopped = self.invoke("stop")
        self.assertEqual(stopped.returncode, 0)
        self.assertIn("Dev gateway not running", stopped.stdout)

        self.seed_running()
        self.calls.unlink()
        running = self.invoke("stop")
        self.assertEqual(running.returncode, 0, running.stdout + running.stderr)
        self.assertIn("Stopped", running.stdout)
        self.assertFalse(self.pidfile.exists())
        self.assertIn(f"stop-pid {os.getpid()}", self.call_lines())

        self.seed_running()
        self.calls.unlink()
        busy = self.invoke("stop", env=self.env(PORT_FREE="0"))
        self.assertEqual(busy.returncode, 0)
        self.assertIn("WARN: Port 18790 still in use", busy.stdout)

    def test_status_distinguishes_stale_and_live_pid_and_formats_health_json(self) -> None:
        self.pidfile.write_text("99999999\n")
        stopped = self.invoke("status")
        self.assertEqual(stopped.returncode, 0)
        self.assertIn("Dev gateway: STOPPED", stopped.stdout)

        self.seed_running()
        live = self.invoke("status")
        self.assertEqual(live.returncode, 0, live.stdout + live.stderr)
        self.assertIn(f"RUNNING (PID {os.getpid()}, port 18790)", live.stdout)
        self.assertIn('"status": "ok"', live.stdout)
        self.assertIn('"version": "fixture"', live.stdout)

        silent = self.invoke("status", env=self.env(CURL_OK="0"))
        self.assertEqual(silent.returncode, 0)
        self.assertIn("health endpoint not responding", silent.stdout)

    def test_health_and_smoke_preserve_http_failure_exit_statuses(self) -> None:
        health = self.invoke("health")
        self.assertEqual(health.returncode, 0, health.stdout + health.stderr)
        self.assertIn('"status": "ok"', health.stdout)

        success = self.invoke("smoke")
        self.assertEqual(success.returncode, 0, success.stdout + success.stderr)
        self.assertIn("[1/2] GET /health ... OK", success.stdout)
        self.assertIn("[2/2] GET /ready ... OK", success.stdout)
        self.assertIn("All smoke tests passed", success.stdout)

        bad_ready = self.invoke("smoke", env=self.env(READY_CODE="503"))
        self.assertEqual(bad_ready.returncode, 1)
        self.assertIn("FAIL (HTTP 503)", bad_ready.stdout)

        bad_health = self.invoke("smoke", env=self.env(HEALTH_JSON='{"status":"down"}'))
        self.assertEqual(bad_health.returncode, 1)
        self.assertIn("FAIL (status=down)", bad_health.stdout)

    def test_parity_reports_missing_and_present_auth_surfaces_without_mutation(self) -> None:
        missing = self.invoke("parity")
        self.assertEqual(missing.returncode, 0)
        self.assertIn("[GAP]  No production config", missing.stdout)
        self.assertIn("client_token: none", missing.stdout)
        self.assertIn("1 parity gap(s) found", missing.stdout)

        deneb = self.home / ".deneb"
        deneb.mkdir()
        (deneb / "deneb.json").write_text("{}\n")
        (deneb / "client_token").write_text("secret\n")
        present = self.invoke("parity", env=self.env(GEMINI_API_KEY="set"))
        self.assertEqual(present.returncode, 0)
        self.assertIn("Production config:", present.stdout)
        self.assertIn("GEMINI_API_KEY: set", present.stdout)
        self.assertIn("prod token present → seeded on next 'start'", present.stdout)
        self.assertIn("No parity gaps detected", present.stdout)

    def test_quality_reproduction_benchmark_and_utility_dispatch_keep_arguments(self) -> None:
        commands = (
            (("quality", "core", "--record"), "quality-test.py --port 18790 --scenario core --record"),
            (("quality-custom", "안녕", "--model", "main"), "quality-test.py --port 18790 --custom 안녕 --model main"),
            (("quality-history", "--limit", "3"), "quality-test.py --history --limit 3"),
            (("quality-compare", "a", "b"), "quality-test.py --compare a b"),
            (("quality-trend", "daily"), "quality-test.py --trend daily"),
            (("chat-check", "hello", "--expect", "world"), "reproduce.py --port 18790 chat-check hello --expect world"),
            (("multi-chat", "one", "two"), "reproduce.py --port 18790 multi-chat one two"),
            (("tool-check", "fs", "list"), "reproduce.py --port 18790 tool-check fs list"),
            (("bench", "oolong", "--record"), "quality-test.py --scenario bench-ool --port 18790 --record"),
            (("baseline", "show"), "baseline.sh show"),
            (("ar-start", "--target", "x"), "autoresearch.sh start --target x"),
            (("ar-results", "--best"), "ar-results.sh --best"),
        )
        for args, expected in commands:
            with self.subTest(args=args):
                self.calls.unlink(missing_ok=True)
                proc = self.invoke(*args)
                self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
                self.assertIn(expected, self.call_lines())

    def test_log_commands_cover_missing_match_error_and_since_filters(self) -> None:
        missing = self.invoke("logs")
        self.assertEqual(missing.returncode, 0)
        self.assertIn("No log file", missing.stdout)

        self.logfile.write_text(
            "2026-01-01T00:00:00 old line\n"
            "2999-01-01T00:00:00 future INFO needle\n"
            '{"level":"warn","msg":"careful"}\n'
            "panic: boom\n",
            encoding="utf-8",
        )
        tailed = self.invoke("logs", "2")
        self.assertEqual(tailed.returncode, 0)
        self.assertNotIn("old line", tailed.stdout)
        self.assertIn("panic: boom", tailed.stdout)

        matched = self.invoke("logs-grep", "needle")
        self.assertEqual(matched.returncode, 0)
        self.assertIn("future INFO needle", matched.stdout)
        no_match = self.invoke("logs-grep", "absent")
        self.assertEqual(no_match.returncode, 0)
        self.assertIn("No matches for 'absent'", no_match.stdout)
        missing_pattern = self.invoke("logs-grep")
        self.assertEqual(missing_pattern.returncode, 1)

        errors = self.invoke("logs-errors", "10")
        self.assertEqual(errors.returncode, 0)
        self.assertIn('"level":"warn"', errors.stdout)
        self.assertIn("panic: boom", errors.stdout)
        recent = self.invoke("logs-since", "60")
        self.assertEqual(recent.returncode, 0)
        self.assertNotIn("old line", recent.stdout)
        self.assertIn("future INFO needle", recent.stdout)
        self.assertIn("panic: boom", recent.stdout)

    def test_model_show_list_set_and_transport_failure_contracts(self) -> None:
        shown = self.invoke("model")
        self.assertEqual(shown.returncode, 0, shown.stdout + shown.stderr)
        self.assertIn("현재 모델: main/model", shown.stdout)

        listed = self.invoke("model", "list")
        self.assertEqual(listed.returncode, 0)
        self.assertIn("[main] main/model ✓", listed.stdout)
        self.assertIn("[fallback] backup/model", listed.stdout)

        changed = self.invoke("model", "set", "new/model")
        self.assertEqual(changed.returncode, 0, changed.stdout + changed.stderr)
        self.assertIn("모델 변경: old/model → new/model", changed.stdout)
        self.assertTrue(any('-d {"model":"new/model"}' in line for line in self.call_lines()))

        missing = self.invoke("model", "set")
        self.assertEqual(missing.returncode, 1)
        self.assertIn("model set MODEL", missing.stdout)

        down = self.invoke("model", "show", env=self.env(CURL_OK="0"))
        self.assertEqual(down.returncode, 1)
        self.assertIn("dev gateway not responding", down.stdout)


if __name__ == "__main__":
    unittest.main()
