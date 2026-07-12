"""Behavior tests for small operational CLIs and gateway launch plumbing."""

from __future__ import annotations

import json
import shutil
import tempfile
import unittest
from pathlib import Path

from test_shell_support import REPO_ROOT, isolated_env, run_script, write_executable


class ObserveShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.log = self.root / "curl.jsonl"
        (self.home / ".deneb").mkdir(parents=True)
        self.bin.mkdir()
        write_executable(self.bin / "curl", """
            #!/usr/bin/env python3
            import json, os, sys
            args = sys.argv[1:]
            with open(os.environ['FAKE_CURL_LOG'], 'a', encoding='utf-8') as stream:
                stream.write(json.dumps(args) + '\\n')
            print(json.dumps({'ok': True, 'payload': {'accepted': True}}))
        """)
        write_executable(self.bin / "jq", """
            #!/usr/bin/env python3
            import json, sys
            value = json.load(sys.stdin)
            print(json.dumps(value.get('payload', value), sort_keys=True))
        """)

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "FAKE_CURL_LOG": str(self.log),
            "DENEB_CLIENT_TOKEN": "environment-token",
            "DENEB_OBSERVE_URL": "http://observe.test:19000",
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def invoke(self, *args: str, env=None):
        return run_script("scripts/observe.sh", *args, env=env or self.env())

    def request_args(self) -> list[str]:
        lines = self.log.read_text(encoding="utf-8").splitlines()
        self.assertEqual(len(lines), 1)
        return json.loads(lines[0])

    def request_frame(self) -> dict:
        args = self.request_args()
        return json.loads(args[args.index("-d") + 1])

    def test_default_health_uses_configured_url_token_and_rpc_frame(self) -> None:
        proc = self.invoke()
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(json.loads(proc.stdout), {"accepted": True})
        args = self.request_args()
        self.assertIn("http://observe.test:19000/api/v1/miniapp/rpc", args)
        self.assertIn("X-Deneb-Client-Token: environment-token", args)
        self.assertIn("Content-Type: application/json", args)
        self.assertEqual(self.request_frame(), {
            "type": "req",
            "id": "obs",
            "method": "miniapp.observe.health",
            "params": {},
        })

    def test_home_token_is_used_when_environment_token_is_empty(self) -> None:
        (self.home / ".deneb/client_token").write_text("file-token\n", encoding="utf-8")
        proc = self.invoke("health", env=self.env(DENEB_CLIENT_TOKEN=""))
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn("X-Deneb-Client-Token: file-token", self.request_args())

    def test_logs_flags_preserve_numbers_and_json_escape_arbitrary_strings(self) -> None:
        proc = self.invoke(
            "logs",
            "--run", 'run-"quoted"',
            "--session", "client:한글",
            "--level", "warn",
            "--limit", "25",
            "--contains", "line1\nline2\\tail",
            "--since", "1700000000000",
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        frame = self.request_frame()
        self.assertEqual(frame["method"], "miniapp.observe.logs")
        self.assertEqual(frame["params"], {
            "runId": 'run-"quoted"',
            "session": "client:한글",
            "level": "warn",
            "limit": 25,
            "contains": "line1\nline2\\tail",
            "sinceMs": 1700000000000,
        })

    def test_turn_bare_positional_and_explicit_run_have_same_wire_shape(self) -> None:
        for args in [("turn", "run_0003"), ("turn", "--run", "run_0003")]:
            with self.subTest(args=args):
                self.log.unlink(missing_ok=True)
                proc = self.invoke(*args)
                self.assertEqual(proc.returncode, 0, proc.stderr)
                self.assertEqual(self.request_frame()["params"], {"runId": "run_0003"})

    def test_behavior_days_is_numeric_and_unknown_command_exits_two(self) -> None:
        proc = self.invoke("behavior", "--days", "7")
        self.assertEqual(proc.returncode, 0)
        self.assertEqual(self.request_frame()["params"], {"days": 7})

        self.log.unlink()
        unknown = self.invoke("metrics")
        self.assertEqual(unknown.returncode, 2)
        self.assertIn("usage: observe.sh", unknown.stderr)
        self.assertFalse(self.log.exists())

    def test_help_is_generated_from_header_without_network_call(self) -> None:
        proc = self.invoke("--help")
        self.assertEqual(proc.returncode, 0)
        self.assertIn("unified observation CLI", proc.stdout)
        self.assertIn("logs    [--level L]", proc.stdout)
        self.assertFalse(self.log.exists())


class SpellcheckShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.log = self.root / "calls.log"
        self.home.mkdir()
        self.bin.mkdir()

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "FAKE_LOG": str(self.log),
            "FAKE_USER_BASE": str(self.root / "user"),
            "FAKE_CODESPELL_TEMPLATE": str(self.root / "codespell-template"),
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def test_existing_codespell_receives_dictionary_ignore_and_optional_write(self) -> None:
        write_executable(self.bin / "codespell", """
            #!/usr/bin/env bash
            printf '%s\n' "$*" >> "$FAKE_LOG"
        """)
        for mode, ending in [((), "scripts/codespell-ignore.txt"), (("--write",), "-w")]:
            with self.subTest(mode=mode):
                self.log.unlink(missing_ok=True)
                proc = run_script("scripts/docs-spellcheck.sh", *mode, env=self.env())
                self.assertEqual(proc.returncode, 0, proc.stderr)
                call = self.log.read_text(encoding="utf-8").strip()
                self.assertIn("README.md docs", call)
                self.assertIn("scripts/codespell-dictionary.txt", call)
                self.assertIn("scripts/codespell-ignore.txt", call)
                self.assertTrue(call.endswith(ending))

    def test_python_fallback_installs_then_executes_user_codespell(self) -> None:
        write_executable(self.root / "codespell-template", """
            #!/usr/bin/env bash
            printf 'installed-codespell %s\n' "$*" >> "$FAKE_LOG"
        """)
        write_executable(self.bin / "python3", """
            #!/usr/bin/env bash
            printf 'python3 %s\\n' "$*" >> "$FAKE_LOG"
            if [[ "$1 $2" == "-m pip" ]]; then
              mkdir -p "$FAKE_USER_BASE/bin"
              cp "$FAKE_CODESPELL_TEMPLATE" "$FAKE_USER_BASE/bin/codespell"
              chmod +x "$FAKE_USER_BASE/bin/codespell"
              exit 0
            fi
            if [[ "$1" == "-" ]]; then
              printf '%s/bin\\n' "$FAKE_USER_BASE"
              exit 0
            fi
            exit 90
        """)
        proc = run_script("scripts/docs-spellcheck.sh", env=self.env())
        self.assertEqual(proc.returncode, 0, proc.stderr)
        calls = self.log.read_text(encoding="utf-8")
        self.assertIn("python3 -m pip install --user --disable-pip-version-check", calls)
        self.assertIn("installed-codespell README.md docs", calls)

    def test_unavailable_tool_reports_failure_after_both_pip_forms(self) -> None:
        write_executable(self.bin / "python3", """
            #!/usr/bin/env bash
            printf 'python3 %s\\n' "$*" >> "$FAKE_LOG"
            if [[ "$1" == "-" ]]; then
              printf '%s/bin\\n' "$FAKE_USER_BASE"
              exit 0
            fi
            exit 1
        """)
        proc = run_script("scripts/docs-spellcheck.sh", env=self.env())
        self.assertEqual(proc.returncode, 1)
        self.assertIn("codespell unavailable", proc.stderr)
        calls = self.log.read_text(encoding="utf-8")
        self.assertEqual(calls.count("python3 -m pip install"), 2)
        self.assertIn("--break-system-packages", calls)


class DevEnvironmentCheckTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.home.mkdir()
        self.bin.mkdir()

    def test_missing_optional_and_required_tools_are_nonblocking(self) -> None:
        env = {
            "HOME": str(self.home),
            "PATH": "/usr/bin:/bin",
            "LANG": "C.UTF-8",
            "LC_ALL": "C.UTF-8",
        }
        proc = run_script("scripts/check-dev-env.sh", env=env)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn("Deneb dev environment check", proc.stdout)
        self.assertIn("[missing] golangci-lint", proc.stdout)
        self.assertIn("Some build targets may fail", proc.stdout)
        self.assertIn("[hint] mergiraf not installed", proc.stdout)
        self.assertIn("Build: make go -> make test", proc.stdout)

    def test_version_fallback_module_verify_and_optional_git_statuses(self) -> None:
        for name, body in {
            "go": """
                #!/usr/bin/env bash
                if [[ "$1 $2" == "mod verify" ]]; then exit 0; fi
                if [[ "$1" == "version" ]]; then echo 'go version go1.25 linux'; fi
            """,
            "make": "#!/usr/bin/env bash\necho 'GNU Make 4.4'\n",
            "golangci-lint": "#!/usr/bin/env bash\necho 'golangci-lint 2.1'\n",
            "rg": "#!/usr/bin/env bash\necho 'ripgrep 14.1'\n",
            "python3": "#!/usr/bin/env bash\necho 'Python 3.12'\n",
            "mergiraf": "#!/usr/bin/env bash\nexit 0\n",
            "clash": "#!/usr/bin/env bash\nexit 0\n",
            "git": """
                #!/usr/bin/env bash
                case "$*" in
                  *"merge.mergiraf.name"*) echo mergiraf ;;
                  *"rerere.enabled"*) echo true ;;
                  *"maintenance.strategy"*) echo incremental ;;
                esac
            """,
        }.items():
            write_executable(self.bin / name, body)
        proc = run_script(
            "scripts/check-dev-env.sh",
            env=isolated_env(self.home, self.bin),
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn("[ok] Go: go version go1.25 linux", proc.stdout)
        self.assertIn("[ok] Go module cache verified", proc.stdout)
        self.assertIn("[ok] mergiraf + merge driver configured", proc.stdout)
        self.assertIn("[ok] git rerere enabled", proc.stdout)
        self.assertIn("[ok] git maintenance registered", proc.stdout)
        self.assertIn("[ok] clash installed", proc.stdout)
        self.assertIn("All tools available. Ready to build.", proc.stdout)


class GoGatewayLauncherTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "repo"
        self.script = self.root / "scripts/deploy/start-go-gateway.sh"
        self.gateway = self.root / "gateway-go/gateway"
        self.bin = self.root / "fake-bin"
        self.home = self.root / "home"
        self.script.parent.mkdir(parents=True)
        self.gateway.parent.mkdir(parents=True)
        self.bin.mkdir(parents=True)
        self.home.mkdir()
        shutil.copy2(REPO_ROOT / "scripts/deploy/start-go-gateway.sh", self.script)
        self.script.chmod(0o755)
        write_executable(self.bin / "sleep", "#!/usr/bin/env bash\nexit 0\n")

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "FAKE_CALL_LOG": str(self.root / "calls.log"),
            "FAKE_COUNTER": str(self.root / "counter"),
            "FAKE_EXITS": "0",
            "FAKE_NOW": "1000",
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def install_gateway(self) -> None:
        write_executable(self.gateway, """
            #!/usr/bin/env bash
            printf 'gateway cwd=%s args=%s\n' "$PWD" "$*" >> "$FAKE_CALL_LOG"
            count=$(cat "$FAKE_COUNTER" 2>/dev/null || echo 0)
            count=$((count + 1)); echo "$count" > "$FAKE_COUNTER"
            IFS=',' read -ra exits <<< "$FAKE_EXITS"
            idx=$((count - 1)); [[ $idx -lt ${#exits[@]} ]] || idx=$((${#exits[@]} - 1))
            exit "${exits[$idx]}"
        """)

    def run_fixture(self, *args: str, env=None):
        import subprocess

        return subprocess.run(
            [str(self.script), *args],
            cwd=self.root,
            env=env or self.env(),
            capture_output=True,
            text=True,
            timeout=10,
            check=False,
        )

    def test_argument_forwarding_and_nonrestart_exit_code(self) -> None:
        self.install_gateway()
        proc = self.run_fixture(
            "--port", "19000",
            "--bind", "tailnet",
            "--daemon",
            "--force",
            "--log-level", "debug",
            "--config", "custom.json",
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn(
            "Starting Go gateway: "
            f"{self.gateway} --port 19000 --bind tailnet --daemon "
            "--log-level debug --config custom.json",
            proc.stdout,
        )
        calls = (self.root / "calls.log").read_text(encoding="utf-8")
        self.assertIn("args=--port 19000 --bind tailnet --daemon", calls)
        self.assertIn("--log-level debug --config custom.json", calls)

    def test_missing_binary_builds_from_repository_root_then_executes_output(self) -> None:
        write_executable(self.bin / "make", """
            #!/usr/bin/env bash
            printf 'make cwd=%s args=%s\n' "$PWD" "$*" >> "$FAKE_CALL_LOG"
            mkdir -p gateway-go
            cat > gateway-go/gateway <<'SH'
#!/usr/bin/env bash
printf 'built-gateway cwd=%s args=%s\n' "$PWD" "$*" >> "$FAKE_CALL_LOG"
exit 0
SH
            chmod +x gateway-go/gateway
        """)
        proc = self.run_fixture("--port", "19001")
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = (self.root / "calls.log").read_text(encoding="utf-8")
        self.assertIn(f"make cwd={self.root} args=go", calls)
        self.assertIn("built-gateway", calls)

    def test_exit_75_restarts_once_when_outside_rate_limit(self) -> None:
        self.install_gateway()
        write_executable(
            self.bin / "date",
            '#!/usr/bin/env bash\necho "$FAKE_NOW"\n',
        )
        proc = self.run_fixture(env=self.env(FAKE_EXITS="75,0", FAKE_NOW="1000"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("Gateway requested restart (exit 75), restarting", proc.stdout)
        self.assertEqual((self.root / "counter").read_text().strip(), "2")

    def test_exit_75_inside_rate_limit_is_not_looped(self) -> None:
        self.install_gateway()
        write_executable(
            self.bin / "date",
            '#!/usr/bin/env bash\necho "$FAKE_NOW"\n',
        )
        proc = self.run_fixture(env=self.env(FAKE_EXITS="75", FAKE_NOW="100"))
        self.assertEqual(proc.returncode, 75)
        self.assertIn("rate limit reached", proc.stdout)
        self.assertIn("last restart 100s ago, limit 600s", proc.stdout)
        self.assertEqual((self.root / "counter").read_text().strip(), "1")

    def test_regular_gateway_failure_is_propagated_exactly(self) -> None:
        self.install_gateway()
        proc = self.run_fixture(env=self.env(FAKE_EXITS="23"))
        self.assertEqual(proc.returncode, 23)
        self.assertNotIn("restarting", proc.stdout)


if __name__ == "__main__":
    unittest.main()
