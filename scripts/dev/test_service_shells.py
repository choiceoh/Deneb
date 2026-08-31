"""Lifecycle and installer tests for deployment and systemd shell entrypoints."""

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from test_shell_support import REPO_ROOT, isolated_env, run_script, wait_for_text, write_executable

RUNTIME_FILES = (
    Path("/tmp/wormhole.pid"),
    Path("/tmp/wormhole.log"),
    Path("/tmp/bge-m3-server.pid"),
    Path("/tmp/bge-m3-server.log"),
)


class ManualServiceLauncherTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "repo"
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.deploy = self.root / "scripts/deploy"
        self.dist = self.root / "dist"
        self.log = self.root / "calls.log"
        self.home.mkdir(parents=True)
        self.bin.mkdir()
        self.deploy.mkdir(parents=True)
        self.dist.mkdir()
        for name in ("start-wormhole.sh", "start-bge-m3.sh", "bge-m3-server.py"):
            shutil.copy2(REPO_ROOT / "scripts/deploy" / name, self.deploy / name)
        for path in (self.deploy / "start-wormhole.sh", self.deploy / "start-bge-m3.sh"):
            path.chmod(0o755)
        self.saved = {path: path.read_bytes() if path.exists() else None for path in RUNTIME_FILES}
        self.addCleanup(self.restore_runtime_files)
        for path in RUNTIME_FILES:
            path.unlink(missing_ok=True)

        write_executable(self.bin / "curl", """
            #!/usr/bin/env bash
            printf 'curl %s\n' "$*" >> "$FAKE_LOG"
            if [[ "${CURL_OK:-1}" == 1 ]]; then
              [[ "$*" == *bge* || "$*" == *8001* ]] && printf '{"status":"ok"}'
              exit 0
            fi
            exit 22
        """)
        write_executable(self.bin / "nohup", """
            #!/usr/bin/env bash
            printf 'nohup %s\n' "$*" >> "$FAKE_LOG"
            exit "${NOHUP_RC:-0}"
        """)
        write_executable(self.bin / "sleep", "#!/usr/bin/env bash\nexit 0\n")
        write_executable(self.bin / "make", """
            #!/usr/bin/env bash
            printf 'make cwd=%s args=%s\n' "$PWD" "$*" >> "$FAKE_LOG"
            mkdir -p dist
            printf '#!/usr/bin/env bash\nexit 0\n' > dist/wormhole
            chmod +x dist/wormhole
        """)

    def restore_runtime_files(self) -> None:
        for path, content in self.saved.items():
            if content is None:
                path.unlink(missing_ok=True)
            else:
                path.write_bytes(content)

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "FAKE_LOG": str(self.log),
            "CURL_OK": "1",
            "WORMHOLE_CONFIG": str(self.root / "wormhole.json"),
            "WORMHOLE_HEALTH_HOST": "10.0.0.2",
            "WORMHOLE_HEALTH_PORT": "18888",
            "BGE_M3_HOST": "10.0.0.3",
            "BGE_M3_PORT": "8001",
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def run_fixture(self, name: str, command: str, env=None):
        return subprocess.run(
            [str(self.deploy / name), command],
            cwd=self.root,
            env=env or self.env(),
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )

    def calls(self) -> str:
        return self.log.read_text(encoding="utf-8") if self.log.exists() else ""

    def test_wormhole_usage_status_and_missing_config_contracts(self) -> None:
        usage = self.run_fixture("start-wormhole.sh", "help")
        self.assertEqual(usage.returncode, 1)
        self.assertIn("{start|stop|restart|status}", usage.stdout)

        healthy = self.run_fixture("start-wormhole.sh", "status")
        self.assertEqual(healthy.returncode, 0, healthy.stderr)
        self.assertIn("wormhole healthy on 10.0.0.2:18888", healthy.stdout)
        self.assertIn("http://10.0.0.2:18888/health", self.calls())

        self.log.unlink()
        unhealthy = self.run_fixture("start-wormhole.sh", "status", self.env(CURL_OK="0"))
        self.assertEqual(unhealthy.returncode, 1)
        self.assertIn("unhealthy or not running", unhealthy.stdout)

        missing = self.run_fixture("start-wormhole.sh", "start")
        self.assertEqual(missing.returncode, 1)
        self.assertIn("wormhole config not found", missing.stderr)
        self.assertFalse(Path("/tmp/wormhole.pid").exists())

    def test_wormhole_start_builds_if_needed_and_passes_config_to_background_process(self) -> None:
        (self.root / "wormhole.json").write_text("{}", encoding="utf-8")
        proc = self.run_fixture("start-wormhole.sh", "start")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("building wormhole", proc.stdout)
        self.assertIn("started (pid", proc.stdout)
        self.assertIn("healthy", proc.stdout)
        nohup_call = f"nohup {self.dist / 'wormhole'} --config {self.root / 'wormhole.json'}"
        calls = wait_for_text(self.log, nohup_call)
        self.assertIn(f"make cwd={self.root} args=wormhole", calls)
        self.assertIn(nohup_call, calls)
        self.assertTrue(Path("/tmp/wormhole.pid").read_text().strip().isdigit())

    def test_wormhole_already_running_and_stale_stop_never_signal_wrong_process(self) -> None:
        Path("/tmp/wormhole.pid").write_text(f"{os.getpid()}\n")
        already = self.run_fixture("start-wormhole.sh", "start")
        self.assertEqual(already.returncode, 0)
        self.assertIn(f"already running (pid {os.getpid()})", already.stdout)
        self.assertNotIn("nohup", self.calls())

        Path("/tmp/wormhole.pid").write_text("99999999\n")
        stopped = self.run_fixture("start-wormhole.sh", "stop")
        self.assertEqual(stopped.returncode, 0)
        self.assertIn("not running (stale pid file)", stopped.stdout)
        self.assertFalse(Path("/tmp/wormhole.pid").exists())

    def test_bge_start_honors_cpu_and_cuda_gpu_layer_resolution(self) -> None:
        cpu = self.run_fixture("start-bge-m3.sh", "start")
        self.assertEqual(cpu.returncode, 0, cpu.stdout + cpu.stderr)
        self.assertIn("gpu-layers=0", cpu.stdout)
        self.assertIn("healthy", cpu.stdout)
        self.assertIn(
            "--port 8001 --host 10.0.0.3 --gpu-layers 0",
            wait_for_text(self.log, "--gpu-layers 0"),
        )

        Path("/tmp/bge-m3-server.pid").unlink(missing_ok=True)
        self.log.unlink()
        cuda = self.run_fixture(
            "start-bge-m3.sh",
            "start",
            self.env(BGE_M3_DEVICE="cuda"),
        )
        self.assertEqual(cuda.returncode, 0, cuda.stdout + cuda.stderr)
        self.assertIn("gpu-layers=99", cuda.stdout)
        self.assertIn("--gpu-layers 99", wait_for_text(self.log, "--gpu-layers 99"))

        Path("/tmp/bge-m3-server.pid").unlink(missing_ok=True)
        self.log.unlink()
        override = self.run_fixture(
            "start-bge-m3.sh",
            "start",
            self.env(BGE_M3_DEVICE="cuda", BGE_M3_GPU_LAYERS="17"),
        )
        self.assertEqual(override.returncode, 0)
        self.assertIn("--gpu-layers 17", wait_for_text(self.log, "--gpu-layers 17"))

    def test_bge_status_and_stale_stop_have_stable_exit_codes(self) -> None:
        healthy = self.run_fixture("start-bge-m3.sh", "status")
        self.assertEqual(healthy.returncode, 0)
        self.assertIn('{"status":"ok"}', healthy.stdout)

        unhealthy = self.run_fixture("start-bge-m3.sh", "status", self.env(CURL_OK="0"))
        self.assertEqual(unhealthy.returncode, 1)
        self.assertIn("unhealthy or not running", unhealthy.stdout)

        Path("/tmp/bge-m3-server.pid").write_text("99999999\n")
        stopped = self.run_fixture("start-bge-m3.sh", "stop")
        self.assertEqual(stopped.returncode, 0)
        self.assertIn("not running (stale pid file)", stopped.stdout)
        self.assertFalse(Path("/tmp/bge-m3-server.pid").exists())


class SystemdInstallerTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.log = self.root / "calls.log"
        self.pid_counter = self.root / "pid-counter"
        self.home.mkdir()
        self.bin.mkdir()
        write_executable(self.bin / "git", """
            #!/usr/bin/env bash
            printf 'git %s\n' "$*" >> "$FAKE_LOG"
            case "$*" in
              "branch --show-current") echo "${GIT_BRANCH:-main}" ;;
              "rev-parse HEAD") echo "${GIT_HEAD:-commit123}" ;;
              *) exit 89 ;;
            esac
        """)
        write_executable(self.bin / "systemctl", """
            #!/usr/bin/env bash
            printf 'systemctl %s\n' "$*" >> "$FAKE_LOG"
            if [[ "$*" == *"show deneb-gateway.service"* ]]; then
              n=$(cat "$FAKE_PID_COUNTER" 2>/dev/null || echo 0); n=$((n + 1)); echo "$n" > "$FAKE_PID_COUNTER"
              if [[ "${SYSTEMD_RUNNING:-1}" != 1 ]]; then echo 0
              elif [[ $n -eq 1 ]]; then echo 111
              else echo 222; fi
            elif [[ "$*" == *"status deneb-lmtp.socket"* ]]; then
              printf '● deneb-lmtp.socket\n   Loaded: loaded\n   Active: active\n'
            fi
        """)
        write_executable(self.bin / "journalctl", """
            #!/usr/bin/env bash
            printf 'journalctl %s\n' "$*" >> "$FAKE_LOG"
            [[ "${JOURNAL_CONFIRMED:-1}" == 1 ]] && echo 'LMTP systemd 소켓 활성화 complete'
        """)
        write_executable(self.bin / "sleep", "#!/usr/bin/env bash\nexit 0\n")

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "FAKE_LOG": str(self.log),
            "FAKE_PID_COUNTER": str(self.pid_counter),
            "GIT_BRANCH": "main",
            "GIT_HEAD": "commit123",
            "DENEB_STATE_DIR": str(self.root / "state"),
            "SYSTEMD_RUNNING": "1",
            "JOURNAL_CONFIRMED": "1",
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def calls(self) -> str:
        return self.log.read_text(encoding="utf-8") if self.log.exists() else ""

    def test_when_auto_deploy_installer_refuses_non_main_before_writing_units(self) -> None:
        proc = run_script(
            "scripts/systemd/setup-auto-deploy.sh",
            env=self.env(GIT_BRANCH="feature"),
        )
        self.assertEqual(proc.returncode, 1)
        self.assertIn("production main checkout", proc.stderr)
        self.assertFalse((self.home / ".config/systemd/user").exists())
        self.assertNotIn("systemctl", self.calls())

    def test_when_auto_deploy_installer_copies_units_dropin_head_and_enables_timer(self) -> None:
        proc = run_script("scripts/systemd/setup-auto-deploy.sh", env=self.env())
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        units = self.home / ".config/systemd/user"
        self.assertEqual(
            (units / "deneb-auto-deploy.service").read_text(),
            (REPO_ROOT / "scripts/systemd/deneb-auto-deploy.service").read_text(),
        )
        self.assertEqual(
            (units / "deneb-auto-deploy.timer").read_text(),
            (REPO_ROOT / "scripts/systemd/deneb-auto-deploy.timer").read_text(),
        )
        self.assertIn(
            "SuccessExitStatus=0 75 143",
            (units / "deneb-gateway.service.d/restart-exit-status.conf").read_text(),
        )
        self.assertEqual((self.root / "state/auto-deploy.deployed-head").read_text(), "commit123\n")
        self.assertIn("systemctl --user daemon-reload", self.calls())
        self.assertIn("systemctl --user enable --now deneb-auto-deploy.timer", self.calls())

    def test_lmtp_cutover_refuses_when_gateway_is_not_running(self) -> None:
        proc = run_script(
            "scripts/systemd/setup-lmtp-socket.sh",
            env=self.env(SYSTEMD_RUNNING="0"),
        )
        self.assertEqual(proc.returncode, 1)
        self.assertIn("deneb-gateway.service is not running", proc.stderr)
        self.assertIn("systemctl --user enable deneb-lmtp.socket", self.calls())
        self.assertNotIn("kill --kill-who", self.calls())

    def test_lmtp_cutover_writes_dropin_rotates_pid_and_confirms_journal(self) -> None:
        proc = run_script("scripts/systemd/setup-lmtp-socket.sh", env=self.env())
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        units = self.home / ".config/systemd/user"
        dropin = (units / "deneb-gateway.service.d/lmtp-socket.conf").read_text()
        self.assertIn("After=deneb-lmtp.socket", dropin)
        self.assertIn("Wants=deneb-lmtp.socket", dropin)
        self.assertIn("Sockets=deneb-lmtp.socket", dropin)
        self.assertEqual(
            (units / "deneb-lmtp.socket").read_text(),
            (REPO_ROOT / "scripts/systemd/deneb-lmtp.socket").read_text(),
        )
        calls = self.calls()
        self.assertIn("systemctl --user kill --kill-who=main -s SIGUSR1 deneb-gateway.service", calls)
        self.assertIn("OK: socket activation confirmed", proc.stdout)
        self.assertIn("Rollback", proc.stdout)


class GatewayServiceInstallerTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.log = self.root / "calls.log"
        self.home.mkdir()
        self.bin.mkdir()
        for name, body in {
            "make": "#!/usr/bin/env bash\nprintf 'make %s\\n' \"$*\" >> \"$FAKE_LOG\"\n",
            "deneb": "#!/usr/bin/env bash\nprintf 'deneb %s\\n' \"$*\" >> \"$FAKE_LOG\"\n",
            "whoami": "#!/usr/bin/env bash\necho fixture-user\n",
            "sleep": "#!/usr/bin/env bash\nexit 0\n",
            "systemctl": """
                #!/usr/bin/env bash
                printf 'systemctl %s\n' "$*" >> "$FAKE_LOG"
                exit "${SYSTEMCTL_RC:-0}"
            """,
            "loginctl": """
                #!/usr/bin/env bash
                printf 'loginctl %s\n' "$*" >> "$FAKE_LOG"
                if [[ "$1" == "show-user" ]]; then
                  echo "Linger=${LINGER_STATUS:-yes}"
                fi
            """,
        }.items():
            write_executable(self.bin / name, body)

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "FAKE_LOG": str(self.log),
            "LINGER_STATUS": "yes",
            "SYSTEMCTL_RC": "0",
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def calls(self) -> str:
        return self.log.read_text(encoding="utf-8") if self.log.exists() else ""

    def test_gateway_installer_builds_and_installs_default_service_contract(self) -> None:
        proc = run_script("scripts/systemd/setup-gateway-service.sh", env=self.env())
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = self.calls()
        self.assertIn("make all", calls)
        self.assertIn("deneb gateway install --force", calls)
        self.assertNotIn("enable-linger", calls)
        self.assertIn(
            "systemctl --user status deneb-gateway.service --no-pager",
            calls,
        )
        self.assertIn("Gateway service installed", proc.stdout)

    def test_when_port_is_forwarded_and_disabled_linger_is_enabled_for_current_user(self) -> None:
        proc = run_script(
            "scripts/systemd/setup-gateway-service.sh",
            "--port", "19000",
            env=self.env(LINGER_STATUS="no"),
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = self.calls()
        self.assertIn("loginctl show-user fixture-user --property=Linger", calls)
        self.assertIn("loginctl enable-linger fixture-user", calls)
        self.assertIn("deneb gateway install --force --port 19000", calls)

    def test_status_failure_is_advisory_after_successful_install(self) -> None:
        proc = run_script(
            "scripts/systemd/setup-gateway-service.sh",
            env=self.env(SYSTEMCTL_RC="4"),
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("deneb gateway install --force", self.calls())
        self.assertIn("Gateway service installed", proc.stdout)


if __name__ == "__main__":
    unittest.main()
