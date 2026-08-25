"""Lifecycle, role-overlay, routing, and result tests for puppet.sh."""

from __future__ import annotations

import json
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from test_shell_support import REPO_ROOT, isolated_env, write_executable

ACTIVE_MARKER = Path("/tmp/deneb-puppet-active")


class PuppetShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "fixture"
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.dev = self.root / "scripts/dev"
        self.state = self.root / "state"
        self.calls = self.root / "calls.log"
        self.broker_marker = self.root / "broker-up"
        self.home.mkdir(parents=True)
        self.bin.mkdir()
        self.dev.mkdir(parents=True)
        self.state.mkdir()
        shutil.copy2(REPO_ROOT / "scripts/dev/puppet.sh", self.dev / "puppet.sh")
        (self.dev / "puppet.sh").chmod(0o755)
        self.saved_active = ACTIVE_MARKER.read_bytes() if ACTIVE_MARKER.exists() else None
        self.addCleanup(self.restore_active_marker)
        ACTIVE_MARKER.unlink(missing_ok=True)

        write_executable(self.dev / "lib-server.sh", """
            #!/usr/bin/env bash
            DEVLIB_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
            DEVLIB_REPO_DIR="$(cd "$DEVLIB_SCRIPT_DIR/../.." && pwd)"
            DEVLIB_INSTANCE="${DENEB_INSTANCE:-fixture}"
            DEVLIB_TMP_PREFIX="$FIXTURE_STATE/deneb"
            DEVLIB_LIVE_PORT=18790
            DEVLIB_PUPPET_PORT=18793
            DEVLIB_HOST=127.0.0.1
            devlib_load_dotenv() { printf 'load-dotenv\n' >> "$FAKE_CALLS"; }
            devlib_is_pid_alive() {
              if [[ "${BROKER_STARTS:-0}" == 1 && -f "${DEVLIB_TMP_PREFIX}-puppet-broker.pid" ]] \
                 && [[ "$(cat "${DEVLIB_TMP_PREFIX}-puppet-broker.pid")" == "$1" ]]; then
                touch "$BROKER_MARKER"
                return 0
              fi
              [[ ",${ALIVE_PIDS:-}," == *",$1,"* ]]
            }
            devlib_stop_pid() { printf 'stop-pid %s\n' "$1" >> "$FAKE_CALLS"; }
            devlib_wait_port_free() { printf 'wait-port %s\n' "$1" >> "$FAKE_CALLS"; return 0; }
            devlib_build() {
              printf 'build %s\n' "$1" >> "$FAKE_CALLS"
              mkdir -p "$(dirname "$1")"; printf '#!/usr/bin/env bash\nexit 0\n' > "$1"; chmod +x "$1"
            }
            devlib_gen_config() {
              printf 'gen-config %s source=%s\n' "$1" "${DENEB_CONFIG_PATH:-}" >> "$FAKE_CALLS"
              cat > "$1" <<'JSON'
{"models":{"providers":{"existing":{"baseUrl":"http://model"}}},"agents":{"defaultModel":"prod/main","lightweightModel":"prod/light","chatbotModel":"prod/chat","visionModel":"prod/vision","defaults":{"subagents":{"model":"prod/sub"}}},"unrelated":{"keep":true}}
JSON
            }
            devlib_start_gateway() {
              printf 'start-gateway binary=%s port=%s config=%s state=%s log=%s mode=%s idle=%s\n' \
                "$1" "$2" "$3" "$4" "$5" "$6" "${DENEB_STREAM_IDLE_TIMEOUT_MS:-}" >> "$FAKE_CALLS"
              DEVLIB_PID=888
            }
            devlib_wait_healthy() {
              printf 'wait-healthy %s %s %s\n' "$1" "$2" "$3" >> "$FAKE_CALLS"
              [[ "${GATEWAY_HEALTHY:-1}" == 1 ]]
            }
        """)
        write_executable(self.bin / "curl", """
            #!/usr/bin/env bash
            printf 'curl %s\n' "$*" >> "$FAKE_CALLS"
            if [[ "$*" == *"127.0.0.1:18793/puppet/health"* ]]; then
              [[ "${BROKER_UP:-0}" == 1 || -f "$BROKER_MARKER" ]]
            elif [[ "$*" == *"/puppet/state"* ]]; then
              printf '{"pending":2}'
            elif [[ "$*" == *"other.test"* ]]; then
              [[ "${OTHER_UP:-0}" == 1 ]]
            else
              return 22 2>/dev/null || exit 22
            fi
        """)
        write_executable(self.bin / "nohup", """
            #!/usr/bin/env bash
            printf 'nohup %s\n' "$*" >> "$FAKE_CALLS"
            if [[ "${BROKER_STARTS:-1}" == 1 ]]; then touch "$BROKER_MARKER"; fi
        """)
        write_executable(self.bin / "sleep", "#!/usr/bin/env bash\nexit 0\n")
        write_executable(self.dev / "puppet_broker.py", """
            #!/usr/bin/env python3
            import os, sys
            with open(os.environ["FAKE_CALLS"], "a") as stream:
                stream.write("broker-cli " + " ".join(sys.argv[1:]) + "\\n")
            print("broker-result")
        """)

    def restore_active_marker(self) -> None:
        if self.saved_active is None:
            ACTIVE_MARKER.unlink(missing_ok=True)
        else:
            ACTIVE_MARKER.write_bytes(self.saved_active)

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "HOME": str(self.home),
            "FIXTURE_STATE": str(self.state),
            "FAKE_CALLS": str(self.calls),
            "BROKER_MARKER": str(self.broker_marker),
            "DENEB_INSTANCE": "fixture",
            "BROKER_UP": "0",
            "BROKER_STARTS": "1",
            "OTHER_UP": "0",
            "ALIVE_PIDS": "",
            "GATEWAY_HEALTHY": "1",
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def invoke(self, *args: str, env=None):
        return subprocess.run(
            [str(self.dev / "puppet.sh"), *args],
            cwd=self.root,
            env=env or self.env(),
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        )

    def call_lines(self) -> list[str]:
        return self.calls.read_text(encoding="utf-8").splitlines() if self.calls.exists() else []

    @property
    def prefix(self) -> Path:
        return self.state / "deneb"

    def test_help_is_header_derived_and_unknown_command_is_explicit(self) -> None:
        help_proc = self.invoke("--help")
        self.assertEqual(help_proc.returncode, 0)
        self.assertIn("run the dev gateway with its LLM replaced", help_proc.stdout)
        self.assertIn("puppet.sh reply ID --text", help_proc.stdout)
        self.assertNotIn("set -euo", help_proc.stdout)

        unknown = self.invoke("dance")
        self.assertEqual(unknown.returncode, 1)
        self.assertIn("unknown command: dance", unknown.stderr)

    def test_start_builds_broker_config_and_gateway_with_all_roles_possessed(self) -> None:
        proc = self.invoke("start")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("Building gateway", proc.stdout)
        self.assertIn("Roles: ALL LLM roles possessed", proc.stdout)
        self.assertIn("Running (PID 888, port 18790)", proc.stdout)
        config = json.loads(Path(f"{self.prefix}-puppet-config.json").read_text())
        self.assertEqual(config["models"]["providers"]["existing"]["baseUrl"], "http://model")
        self.assertEqual(config["models"]["providers"]["puppet"], {
            "baseUrl": "http://127.0.0.1:18793/v1",
            "apiKey": "puppet-local",
            "api": "openai",
            "contextWindow": 200000,
        })
        agents = config["agents"]
        self.assertEqual(agents["defaultModel"], "puppet/main-seat")
        self.assertEqual(agents["lightweightModel"], "puppet/lightweight-seat")
        self.assertEqual(agents["fallbackModel"], "puppet/fallback-seat")
        self.assertEqual(agents["tinyModel"], "puppet/tiny-seat")
        self.assertEqual(agents["codingModel"], "puppet/coding-seat")
        self.assertEqual(agents["chatbotModel"], "puppet/chatbot-seat")
        self.assertEqual(agents["visionModel"], "puppet/vision-seat")
        self.assertEqual(agents["defaults"]["subagents"]["model"], "puppet/subagent-seat")
        self.assertTrue(config["unrelated"]["keep"])
        self.assertEqual(Path(f"{self.prefix}-gateway-live.pid").read_text(), "888\n")
        self.assertEqual(ACTIVE_MARKER.read_text().splitlines(), ["fixture", "http://127.0.0.1:18793"])
        self.assertIn("idle=-1", "\n".join(self.call_lines()))

    def test_main_only_overlay_preserves_other_production_roles_unchanged(self) -> None:
        proc = self.invoke("start", "--main-only")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("main → puppet/main-seat", proc.stdout)
        agents = json.loads(Path(f"{self.prefix}-puppet-config.json").read_text())["agents"]
        self.assertEqual(agents["defaultModel"], "puppet/main-seat")
        self.assertEqual(agents["lightweightModel"], "prod/light")
        self.assertEqual(agents["chatbotModel"], "prod/chat")
        self.assertEqual(agents["visionModel"], "prod/vision")
        self.assertEqual(agents["defaults"]["subagents"]["model"], "prod/sub")

    def test_generated_tmp_config_env_is_ignored_but_custom_source_is_preserved(self) -> None:
        generated = self.invoke(
            "start", "--main-only",
            env=self.env(DENEB_CONFIG_PATH="/tmp/deneb-old-puppet-config.json"),
        )
        self.assertEqual(generated.returncode, 0, generated.stdout + generated.stderr)
        self.assertIn("WARN: ignoring DENEB_CONFIG_PATH", generated.stdout)
        self.assertIn("source=", next(line for line in self.call_lines() if line.startswith("gen-config")))
        self.assertNotIn("source=/tmp", "\n".join(self.call_lines()))

        shutil.rmtree(self.state)
        self.state.mkdir()
        self.calls.unlink()
        self.broker_marker.unlink(missing_ok=True)
        custom = self.invoke(
            "start", "--main-only",
            env=self.env(DENEB_CONFIG_PATH=str(self.root / "custom.json")),
        )
        self.assertEqual(custom.returncode, 0, custom.stdout + custom.stderr)
        self.assertIn(f"source={self.root / 'custom.json'}", "\n".join(self.call_lines()))

    def test_start_rebuilds_by_default_and_no_rebuild_reuses_the_binary(self) -> None:
        """A stale binary silently tests old code — the default must rebuild."""
        binary = Path(f"{self.prefix}-gateway-live")
        write_executable(binary, "#!/usr/bin/env bash\nexit 0\n")

        default = self.invoke("start", env=self.env())
        self.assertEqual(default.returncode, 0, default.stdout + default.stderr)
        self.assertTrue(any(line.startswith("build ") for line in self.call_lines()))

        self.calls.unlink(missing_ok=True)
        skipped = self.invoke("start", "--no-rebuild", env=self.env())
        self.assertEqual(skipped.returncode, 0, skipped.stdout + skipped.stderr)
        self.assertFalse(any(line.startswith("build ") for line in self.call_lines()))
        self.assertIn("Reusing existing binary", skipped.stdout)

    def test_existing_gateway_is_stopped_and_rebuild_flag_forces_build(self) -> None:
        binary = Path(f"{self.prefix}-gateway-live")
        write_executable(binary, "#!/usr/bin/env bash\nexit 0\n")
        pidfile = Path(f"{self.prefix}-gateway-live.pid")
        pidfile.write_text("444\n")
        proc = self.invoke("start", "--rebuild", env=self.env(ALIVE_PIDS="444"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = self.call_lines()
        self.assertIn("stop-pid 444", calls)
        self.assertIn("wait-port 18790", calls)
        self.assertTrue(any(line.startswith("build ") for line in calls))

    def test_unknown_start_flag_and_failed_broker_never_start_gateway(self) -> None:
        unknown = self.invoke("start", "--all-main")
        self.assertEqual(unknown.returncode, 1)
        self.assertIn("unknown flag: --all-main", unknown.stderr)

        self.calls.unlink(missing_ok=True)
        failed = self.invoke(
            "start",
            env=self.env(BROKER_STARTS="0", ALIVE_PIDS=""),
        )
        self.assertEqual(failed.returncode, 1)
        self.assertIn("FAIL: broker did not start", failed.stdout)
        self.assertNotIn("start-gateway", "\n".join(self.call_lines()))

    def test_gateway_health_failure_is_reported_after_pid_is_recorded(self) -> None:
        proc = self.invoke("start", env=self.env(GATEWAY_HEALTHY="0"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("WARN: gateway started but /health not responding", proc.stdout)
        self.assertEqual(Path(f"{self.prefix}-gateway-live.pid").read_text(), "888\n")

    def test_status_distinguishes_stopped_and_running_surfaces(self) -> None:
        stopped = self.invoke("status")
        self.assertEqual(stopped.returncode, 0)
        self.assertIn("gateway: STOPPED", stopped.stdout)
        self.assertIn("broker:  STOPPED", stopped.stdout)

        Path(f"{self.prefix}-gateway-live.pid").write_text("444\n")
        self.broker_marker.touch()
        running = self.invoke("status", env=self.env(ALIVE_PIDS="444"))
        self.assertEqual(running.returncode, 0, running.stderr)
        self.assertIn("gateway: RUNNING (PID 444, port 18790", running.stdout)
        self.assertIn("broker:  RUNNING (http://127.0.0.1:18793)", running.stdout)
        self.assertIn('{"pending":2}', running.stdout)

    def test_when_other_instance_hint_is_only_printed_for_reachable_live_marker(self) -> None:
        ACTIVE_MARKER.write_text("other\nhttp://other.test:19000\n")
        hinted = self.invoke("status", env=self.env(OTHER_UP="1"))
        self.assertEqual(hinted.returncode, 0)
        self.assertIn("instance 'other' has one at http://other.test:19000", hinted.stderr)
        self.assertIn("export DENEB_INSTANCE='other'", hinted.stderr)

        self.calls.unlink()
        silent = self.invoke("status", env=self.env(OTHER_UP="0"))
        self.assertEqual(silent.returncode, 0)
        self.assertNotIn("instance 'other'", silent.stderr)

    def test_when_send_argument_validation_happens_before_gateway_preflight(self) -> None:
        missing = self.invoke("send")
        self.assertEqual(missing.returncode, 1)
        self.assertIn("Usage: puppet.sh send MESSAGE", missing.stderr)

        unknown = self.invoke("send", "hello", "--wat")
        self.assertEqual(unknown.returncode, 1)
        self.assertIn("unknown flag: --wat", unknown.stderr)

        no_gateway = self.invoke("send", "hello", "--timeout", "5")
        self.assertEqual(no_gateway.returncode, 1)
        self.assertIn("gateway not running", no_gateway.stderr)

    def test_result_handles_missing_completed_and_running_send_files(self) -> None:
        missing = self.invoke("result")
        self.assertEqual(missing.returncode, 1)
        self.assertEqual(missing.stdout, "(no send yet)\n")

        output = self.state / "send-1.out"
        output.write_text('{"reply":"done"}\n')
        pointer = Path(f"{self.prefix}-puppet-send.last")
        pointer.write_text(str(output) + "\n")
        complete = self.invoke("result")
        self.assertEqual(complete.returncode, 0)
        self.assertEqual(complete.stdout, '{"reply":"done"}\n')

        output.with_suffix(".pid").write_text("555\n")
        running = self.invoke("result", env=self.env(ALIVE_PIDS="555"))
        self.assertEqual(running.returncode, 0)
        self.assertIn("turn still running", running.stdout)
        self.assertTrue(running.stdout.endswith('{"reply":"done"}\n'))

    def test_broker_commands_delegate_exact_arguments_even_when_health_is_down(self) -> None:
        proc = self.invoke("show", "r7", "--outline")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertEqual(proc.stdout, "broker-result\n")
        self.assertIn("broker-cli show r7 --outline", self.call_lines())

    def test_stop_is_instance_scoped_cleans_results_and_preserves_other_marker(self) -> None:
        Path(f"{self.prefix}-gateway-live.pid").write_text("444\n")
        Path(f"{self.prefix}-puppet-broker.pid").write_text("555\n")
        Path(f"{self.prefix}-puppet-send-1.out").write_text("out")
        Path(f"{self.prefix}-puppet-send-1.pid").write_text("999")
        Path(f"{self.prefix}-puppet-send.last").write_text("pointer")
        ACTIVE_MARKER.write_text("other\nhttp://other.test\n")
        proc = self.invoke("stop", env=self.env(ALIVE_PIDS="444,555"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("stop-pid 444", self.call_lines())
        self.assertIn("stop-pid 555", self.call_lines())
        self.assertTrue(ACTIVE_MARKER.exists())
        self.assertEqual(list(self.state.glob("deneb-puppet-send-*")), [])


if __name__ == "__main__":
    unittest.main()
