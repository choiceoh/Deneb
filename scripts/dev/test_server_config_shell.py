"""Isolated shell tests for lib-server and the dev-safe config generator."""

from __future__ import annotations

import json
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

from test_shell_support import REPO_ROOT, isolated_env, run_script, write_executable

LIB_SERVER = REPO_ROOT / "scripts/dev/lib-server.sh"


def run_bash(command: str, env: dict[str, str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["bash", "-c", command],
        cwd=REPO_ROOT,
        env=env,
        capture_output=True,
        text=True,
        timeout=10,
        check=False,
    )


class InstanceIsolationTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.home = Path(self.tmp.name) / "home"
        self.home.mkdir()

    def source_and_print(self, instance: str) -> list[str]:
        env = isolated_env(self.home, DENEB_INSTANCE=instance)
        proc = run_bash(
            f'source "{LIB_SERVER}"; '
            'printf "%s\\n%s\\n%s\\n%s\\n" "$DEVLIB_TMP_PREFIX" '
            '"$DEVLIB_LIVE_PORT" "$DEVLIB_ITERATE_PORT" "$DEVLIB_PUPPET_PORT"',
            env,
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        return proc.stdout.splitlines()

    def test_default_instance_preserves_historical_paths_and_ports(self) -> None:
        self.assertEqual(
            self.source_and_print("default"),
            ["/tmp/deneb", "18790", "18791", "18793"],
        )

    def test_when_named_instance_has_deterministic_nonoverlapping_port_offsets(self) -> None:
        first = self.source_and_print("health-wave")
        second = self.source_and_print("health-wave")
        self.assertEqual(first, second)
        self.assertEqual(first[0], "/tmp/deneb-health-wave")
        live, iterate, puppet = map(int, first[1:])
        self.assertGreaterEqual(live, 18800)
        self.assertLessEqual(live, 19196)
        self.assertEqual(iterate, live + 1)
        self.assertEqual(puppet, live + 3)

    def test_double_source_guard_preserves_first_instance_configuration(self) -> None:
        env = isolated_env(self.home, DENEB_INSTANCE="first")
        proc = run_bash(
            f'source "{LIB_SERVER}"; DENEB_INSTANCE=second; source "{LIB_SERVER}"; '
            'printf "%s" "$DEVLIB_INSTANCE"',
            env,
        )
        self.assertEqual((proc.returncode, proc.stdout), (0, "first"))


class DotenvAndTokenTests(unittest.TestCase):
    def setUp(self) -> None:
        # WSL's mounted Windows temp directory does not expose POSIX chmod bits.
        self.tmp = tempfile.TemporaryDirectory(dir="/tmp")
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.fake_bin = self.root / "bin"
        (self.home / ".deneb").mkdir(parents=True)
        self.fake_bin.mkdir()

    def test_dotenv_loads_quotes_comments_and_never_overwrites_existing_env(self) -> None:
        (self.home / ".deneb/.env").write_text(
            "# comment\n"
            "KEEP=from-file\n"
            "DOUBLE=\"two words\"\n"
            "SINGLE='three words'\n"
            " EMPTY = value \n",
            encoding="utf-8",
        )
        env = isolated_env(self.home, KEEP="already-set")
        proc = run_bash(
            f'source "{LIB_SERVER}"; devlib_load_dotenv; '
            'printf "%s|%s|%s|%s" "$KEEP" "$DOUBLE" "$SINGLE" "$EMPTY"',
            env,
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout, "already-set|two words|three words|value")

    def test_seed_client_token_copies_updates_and_forces_owner_only_mode(self) -> None:
        prod = self.home / ".deneb/client_token"
        prod.write_text("first-token\n", encoding="utf-8")
        state = self.root / "state"
        state.mkdir()
        env = isolated_env(self.home)
        proc = run_bash(
            f'source "{LIB_SERVER}"; devlib_seed_client_token "{state}"; '
            f'cat "{state}/client_token"',
            env,
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout, "first-token\n")
        self.assertEqual(stat.S_IMODE((state / "client_token").stat().st_mode), 0o600)

        prod.write_text("rotated-token\n", encoding="utf-8")
        proc = run_bash(
            f'source "{LIB_SERVER}"; devlib_seed_client_token "{state}"; '
            f'cat "{state}/client_token"',
            env,
        )
        self.assertEqual(proc.stdout, "rotated-token\n")

    def test_ensure_client_token_generates_once_with_openssl_and_keeps_existing(self) -> None:
        write_executable(self.fake_bin / "openssl", """
            #!/usr/bin/env bash
            printf '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n'
        """)
        state = self.root / "state"
        env = isolated_env(self.home, self.fake_bin)
        command = (
            f'source "{LIB_SERVER}"; devlib_ensure_client_token "{state}"; '
            f'cat "{state}/client_token"'
        )
        proc = run_bash(command, env)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(
            proc.stdout,
            "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n",
        )
        self.assertEqual(stat.S_IMODE((state / "client_token").stat().st_mode), 0o600)

        (state / "client_token").write_text("keep-me\n", encoding="utf-8")
        proc = run_bash(command, env)
        self.assertEqual(proc.stdout, "keep-me\n")
        self.assertEqual(stat.S_IMODE((state / "client_token").stat().st_mode), 0o600)


class ReadinessFunctionTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.fake_bin = self.root / "bin"
        self.home.mkdir()
        self.fake_bin.mkdir()

    def test_health_wait_returns_immediately_on_success(self) -> None:
        write_executable(self.fake_bin / "curl", "#!/usr/bin/env bash\nexit 0\n")
        env = isolated_env(self.home, self.fake_bin)
        proc = run_bash(
            f'source "{LIB_SERVER}"; devlib_wait_healthy 127.0.0.1 19000 3; printf "%s" "$?"',
            env,
        )
        self.assertEqual((proc.returncode, proc.stdout), (0, "0"))

    def test_health_wait_reports_timeout_and_dead_child(self) -> None:
        write_executable(self.fake_bin / "curl", "#!/usr/bin/env bash\nexit 1\n")
        env = isolated_env(self.home, self.fake_bin)
        timeout = run_bash(
            f'source "{LIB_SERVER}"; devlib_wait_healthy 127.0.0.1 19000 1; printf "%s" "$?"',
            env,
        )
        self.assertEqual((timeout.returncode, timeout.stdout), (0, "1"))

        dead_child = run_bash(
            f'source "{LIB_SERVER}"; DEVLIB_PID=99999999; '
            'devlib_wait_healthy 127.0.0.1 19000 10; printf "%s" "$?"',
            env,
        )
        self.assertEqual((dead_child.returncode, dead_child.stdout), (0, "1"))

    def test_when_port_wait_distinguishes_free_and_still_held(self) -> None:
        write_executable(self.fake_bin / "ss", """
            #!/usr/bin/env bash
            if [[ "${FAKE_PORT_HELD:-0}" == 1 ]]; then
              echo 'LISTEN 0 128 127.0.0.1:19000 0.0.0.0:*'
            fi
        """)
        free = isolated_env(self.home, self.fake_bin, FAKE_PORT_HELD="0")
        held = isolated_env(self.home, self.fake_bin, FAKE_PORT_HELD="1")
        command = (
            f'source "{LIB_SERVER}"; devlib_wait_port_free 19000 1; printf "%s" "$?"'
        )
        self.assertEqual(run_bash(command, free).stdout, "0")
        self.assertEqual(run_bash(command, held).stdout, "1")

    def test_pid_alive_rejects_blank_and_accepts_current_shell(self) -> None:
        env = isolated_env(self.home)
        proc = run_bash(
            f'source "{LIB_SERVER}"; '
            'devlib_is_pid_alive ""; a=$?; devlib_is_pid_alive "$$"; b=$?; printf "%s|%s" "$a" "$b"',
            env,
        )
        self.assertEqual(proc.stdout, "1|0")


class ConfigGeneratorTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        (self.home / ".deneb").mkdir(parents=True)

    def env(self, **values):
        return isolated_env(self.home, **values)

    def test_missing_production_config_generates_minimal_safe_config(self) -> None:
        output = self.root / "minimal.json"
        missing = self.root / "missing.json"
        proc = run_script(
            "scripts/dev/config-gen.sh",
            "--prod", str(missing),
            "--out", str(output),
            env=self.env(),
        )
        self.assertEqual(proc.returncode, 0)
        self.assertIn("production config not found", proc.stderr)
        self.assertEqual(json.loads(output.read_text(encoding="utf-8")), {
            "cron": {"enabled": False},
            "gmailPoll": {"enabled": False},
        })
        self.assertTrue(output.read_bytes().endswith(b"\n"))

    def test_check_mode_reports_missing_or_present_without_writing_output(self) -> None:
        missing = self.root / "missing.json"
        absent = run_script(
            "scripts/dev/config-gen.sh", "--check", "--prod", str(missing), env=self.env()
        )
        self.assertEqual((absent.returncode, absent.stdout.strip()), (1, "no-prod-config"))

        present = self.root / "prod.json"
        present.write_text("{}", encoding="utf-8")
        found = run_script(
            "scripts/dev/config-gen.sh", "--check", "--prod", str(present), env=self.env()
        )
        self.assertEqual((found.returncode, found.stdout.strip(), found.stderr), (0, "ok", ""))

    def test_generate_disables_background_workers_and_preserves_unrelated_config(self) -> None:
        production = self.root / "prod.json"
        output = self.root / "dev.json"
        production.write_text(json.dumps({
            "gmailPoll": {"enabled": True, "interval": "30s"},
            "cron": {"enabled": True, "jobs": ["daily"]},
            "mailLmtp": {"enabled": True, "listen": "127.0.0.1:10024"},
            "providers": {"local": {"url": "http://model"}},
        }), encoding="utf-8")
        proc = run_script(
            "scripts/dev/config-gen.sh",
            "--prod", str(production),
            "--out", str(output),
            env=self.env(),
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout.strip(), str(output))
        config = json.loads(output.read_text(encoding="utf-8"))
        self.assertFalse(config["gmailPoll"]["enabled"])
        self.assertFalse(config["cron"]["enabled"])
        self.assertFalse(config["mailLmtp"]["enabled"])
        self.assertEqual(config["gmailPoll"]["interval"], "30s")
        self.assertEqual(config["cron"]["jobs"], ["daily"])
        self.assertEqual(config["providers"], {"local": {"url": "http://model"}})

    def test_diff_lists_only_active_safety_changes_or_reports_already_safe(self) -> None:
        active = self.root / "active.json"
        active.write_text(json.dumps({
            "gmailPoll": {"enabled": True},
            "cron": {"enabled": True},
            "mailLmtp": {"enabled": True},
        }), encoding="utf-8")
        proc = run_script(
            "scripts/dev/config-gen.sh", "--diff", "--prod", str(active), env=self.env()
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout.count("  - "), 6)
        for field in ("gmailPoll.enabled", "cron.enabled", "mailLmtp.enabled"):
            self.assertIn(field, proc.stdout)
        self.assertIn("Storage isolation", proc.stdout)

        safe = self.root / "safe.json"
        safe.write_text(json.dumps({
            "gmailPoll": {"enabled": False},
            "cron": {"enabled": False},
            "mailLmtp": {"enabled": False},
        }), encoding="utf-8")
        proc = run_script(
            "scripts/dev/config-gen.sh", "--diff", "--prod", str(safe), env=self.env()
        )
        self.assertIn("No fields modified (config is dev-safe as-is)", proc.stdout)

    def test_dotenv_can_supply_production_path_and_invalid_json_preserves_existing_output(self) -> None:
        production = self.root / "from-env.json"
        production.write_text("{}", encoding="utf-8")
        (self.home / ".deneb/.env").write_text(
            f"DENEB_CONFIG_PATH={production}\n",
            encoding="utf-8",
        )
        proc = run_script("scripts/dev/config-gen.sh", "--check", env=self.env())
        self.assertEqual((proc.returncode, proc.stdout.strip()), (0, "ok"))

        invalid = self.root / "invalid.json"
        invalid.write_text("{broken", encoding="utf-8")
        output = self.root / "existing.json"
        output.write_text("previous-complete-output\n", encoding="utf-8")
        proc = run_script(
            "scripts/dev/config-gen.sh",
            "--prod", str(invalid),
            "--out", str(output),
            env=self.env(),
        )
        self.assertNotEqual(proc.returncode, 0)
        self.assertEqual(output.read_text(encoding="utf-8"), "previous-complete-output\n")


if __name__ == "__main__":
    unittest.main()
