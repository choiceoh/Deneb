"""Behavior contracts for Deneb's agent development tool entrypoint."""
from __future__ import annotations

from contextlib import redirect_stdout
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

import agent_dev as dev
from agent_dev_catalog import BY_ID, Tool


class AgentDevTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def invoke(self, arguments):
        output = io.StringIO()
        with redirect_stdout(output):
            code = dev.main(arguments)
        return code, json.loads(output.getvalue())

    def write(self, relative, content):
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        return path

    def test_catalog_sources_make_targets_and_package_scripts_are_current(self):
        self.assertEqual(dev.registry_errors(), [])
        code, result = self.invoke(["audit"])
        self.assertEqual(code, 0)
        self.assertEqual(result["status"], "ok")
        self.assertGreater(result["unmanaged_entrypoints"], 0)

    def test_discovery_reads_scripts_and_manifests_without_executing_them(self):
        marker = self.root / "must-not-exist"
        self.write("scripts/new.py", '"""New search probe."""\nraise RuntimeError("must not import")\nif __name__ == "__main__": pass\n')
        self.write("scripts/helper.py", "def helper(): pass\n")
        self.write("scripts/test_hidden.py", 'if __name__ == "__main__": pass\n')
        self.write("scripts/.private/hidden.sh", "# hidden\n")
        (self.root / "scripts/outside").symlink_to(self.root, target_is_directory=True)
        self.write("Makefile", f"DANGER := $(shell touch {marker})\nnew-target:\n\tfalse\n")
        self.write("andromeda/package.json", json.dumps({"scripts": {"new": f"touch {marker}"}}))
        with mock.patch.object(dev, "ROOT", self.root), mock.patch.object(dev, "TOOLS", ()):
            found = {row["id"] for row in dev.discover()}
            self.assertEqual(found, {"scripts/new.py", "make:new-target", "andromeda:new"})
            self.assertIn("New search probe", dev.describe("scripts/new.py")["source_help"])
        self.assertFalse(marker.exists())

    def test_unmanaged_source_can_be_inspected_but_not_run_or_escape_the_root(self):
        for identifier in ("scripts/new.py", "../outside.py", "/etc/passwd"):
            with self.subTest(identifier=identifier):
                code, result = self.invoke(["run", identifier])
                self.assertEqual((code, result["status"]), (2, "blocked"))
        code, result = self.invoke(["describe", "../outside.py"])
        self.assertEqual((code, result["status"]), (2, "blocked"))

    def test_search_pages_and_korean_intent_are_machine_readable(self):
        code, first = self.invoke(["search", "", "--limit", "2"])
        _, second = self.invoke(["search", "", "--limit", "2", "--offset", "2"])
        self.assertEqual(code, 0)
        self.assertEqual(first["next_offset"], 2)
        self.assertFalse({r["id"] for r in first["items"]} & {r["id"] for r in second["items"]})
        _, result = self.invoke(["search", "데스크톱"])
        self.assertIn("andromeda.verify", [row["id"] for row in result["items"]])

    def test_invalid_cli_input_always_returns_json(self):
        for args in (["unknown"], ["search", "--limit", "0"], ["run", "--timeout", "nan", "git"],
                     ["run", "--max-output", "1", "git"], ["run", "env.status", "extra"]):
            with self.subTest(args=args):
                code, result = self.invoke(args)
                self.assertEqual((code, result["status"], result["schema_version"]), (2, "blocked", 1))

    def test_entrypoint_uses_its_checkout_from_a_different_directory(self):
        result = subprocess.run([sys.executable, str(dev.ROOT / "dev"), "run", "--dry-run", "andromeda.verify"],
                                cwd=self.root, capture_output=True, text=True, check=True)
        planned = json.loads(result.stdout)
        self.assertEqual(planned["cwd"], str(dev.ROOT / "andromeda"))
        self.assertEqual(planned["argv"], ["pnpm", "verify"])

    def test_invalid_explicit_python_never_falls_back(self):
        with mock.patch.dict(os.environ, {"DENEB_DEV_PYTHON": str(self.root / "missing")}):
            code, result = self.invoke([])
        self.assertEqual((code, result["status"]), (2, "blocked"))

    def test_checkout_python_is_selected_and_added_to_child_path(self):
        python = self.write(".venv/bin/python", "#!/bin/sh\nexit 0\n")
        python.chmod(0o755)
        with mock.patch.object(dev, "ROOT", self.root), mock.patch.dict(os.environ, {"DENEB_DEV_PYTHON": ""}):
            self.assertEqual(dev.python_runtime(), (str(python), "checkout .venv"))
            self.assertEqual(dev.child_environment()["PATH"].split(os.pathsep)[0], str(python.parent))

    def test_dry_run_never_calls_tools_or_package_probes(self):
        with mock.patch.object(dev, "prerequisites", side_effect=AssertionError("probe")), \
             mock.patch.object(subprocess, "Popen", side_effect=AssertionError("execution")):
            result, code = dev.execute(BY_ID["ci.fast"], [], dry_run=True)
        self.assertEqual(code, 0)
        self.assertEqual(result["argv"][:2], ["make", "ci/fast"])
        self.assertEqual(result["readiness"], "not_checked")

    def test_literal_arguments_and_json_stdout_are_preserved_once(self):
        arguments = ["{python}", "$(touch escaped)", "; echo bad", "a b", "--flag"]
        tool = Tool("probe", "probe", ("{python}", "-c", "import json,sys; print(json.dumps(sys.argv[1:]))"))
        result, code = dev.execute(tool, arguments)
        self.assertEqual(code, 0)
        self.assertEqual(result["data"], arguments)
        self.assertIsNone(result["stdout"])
        self.assertEqual(result["validation"], "not_assessed")

    def test_managed_python_and_make_path_are_used_without_changing_parent_environment(self):
        python = self.write("profile/current/venv/bin/python", "#!/bin/sh\nexit 0\n")
        python.chmod(0o755)
        (self.root / "profile/current/bin").mkdir()
        original = os.environ["PATH"]
        with mock.patch.object(dev, "ROOT", self.root), \
             mock.patch.dict(os.environ, {"DENEB_DEV_HOME": str(self.root / "profile"), "DENEB_DEV_PYTHON": "", "GOROOT": "/foreign/go"}):
            self.assertEqual(dev.python_runtime(), (str(python), "managed Deneb environment"))
            self.assertNotIn("GOROOT", dev.child_environment())
            self.assertEqual(dev.child_environment()["GOTOOLCHAIN"], "local")
            self.assertEqual(os.environ["GOROOT"], "/foreign/go")
            command = dev.command_for(BY_ID["ci"], ["ARGS=--scripts"])
            self.assertEqual(command[:2], ["make", "ci"])
            self.assertTrue(command[2].startswith("PATH=" + str(python.parent)))
            self.assertEqual(command[-1], "ARGS=--scripts")
        self.assertEqual(os.environ["PATH"], original)

    def test_subprocess_nonzero_exit_and_stderr_are_preserved(self):
        tool = Tool("probe", "probe", ("{python}", "-c", "import sys; print('bad', file=sys.stderr); sys.exit(7)"))
        result, code = dev.execute(tool, [])
        self.assertEqual((code, result["process_exit_code"], result["status"]), (7, 7, "failed"))
        self.assertIn("bad", result["stderr"])

    def test_output_tail_is_bounded_and_marked_truncated(self):
        tool = Tool("probe", "probe", ("{python}", "-c", "print('x' * 5000 + 'END')"))
        result, _ = dev.execute(tool, [], limit=256)
        self.assertEqual(len(result["stdout"].encode()), 256)
        self.assertTrue(result["truncated"]["stdout"])
        self.assertTrue(result["stdout"].endswith("END\n"))
        self.assertNotIn("data", result)

    def test_missing_executable_and_locked_package_block_before_execution(self):
        with self.assertRaisesRegex(dev.ToolError, "missing executables"):
            dev.execute(Tool("missing", "missing", (str(self.root / "missing"),)), [])
        with mock.patch.object(dev, "package_inventory", return_value={"packages": []}), \
             mock.patch.object(subprocess, "Popen", side_effect=AssertionError("must not run")):
            with self.assertRaisesRegex(dev.ToolError, "locked Python packages"):
                dev.execute(BY_ID["python.lint"], [])

    def test_missing_desktop_dependencies_have_project_scoped_recovery(self):
        with mock.patch.object(dev, "ROOT", self.root), mock.patch.object(dev.shutil, "which", return_value="/bin/true"):
            with self.assertRaises(dev.ToolError) as caught:
                dev.execute(BY_ID["andromeda.verify"], [])
        self.assertIn("andromeda", caught.exception.recovery[0])

    def test_advisory_missing_tools_and_empty_fast_gate_are_incomplete(self):
        cases = [("env.doctor", "[missing] Go"), ("ci.fast", "nothing to gate.")]
        for identifier, output in cases:
            with self.subTest(identifier=identifier):
                tool = Tool(identifier, "probe", ("{python}", "-c", f"print({output!r})"))
                result, code = dev.execute(tool, [])
                self.assertEqual((code, result["status"], result["process_exit_code"]), (3, "incomplete", 0))

    def test_removed_make_target_or_package_script_fails_audit(self):
        self.write("Makefile", "different-target:\n")
        self.write("andromeda/package.json", '{"scripts": {}}')
        self.write("docs/tools/agent-dev.md", "docs")
        tools = (BY_ID["gateway.build"], BY_ID["andromeda.verify"])
        with mock.patch.object(dev, "ROOT", self.root), mock.patch.object(dev, "TOOLS", tools), \
             mock.patch.object(dev, "BY_ID", {tool.id: tool for tool in tools}):
            errors = dev.registry_errors()
        self.assertTrue(any("missing Makefile target go" in error for error in errors))
        self.assertTrue(any("missing package script verify" in error for error in errors))

    def test_adapter_cwd_is_used_for_execution(self):
        (self.root / "andromeda").mkdir()
        tool = Tool("probe", "probe", ("{python}", "-c", "import os; print(os.getcwd())"), cwd="andromeda")
        with mock.patch.object(dev, "ROOT", self.root):
            result, code = dev.execute(tool, [])
        self.assertEqual(code, 0)
        self.assertEqual(result["stdout"].strip(), str((self.root / "andromeda").resolve()))

    def test_timeout_kills_grandchild_even_when_it_ignores_term(self):
        marker = self.root / "escaped-child"
        child = "import signal,time,pathlib; signal.signal(signal.SIGTERM, signal.SIG_IGN); time.sleep(0.6); pathlib.Path(" + repr(str(marker)) + ").touch()"
        parent = "import subprocess,sys,time; subprocess.Popen([sys.executable,'-c'," + repr(child) + "]); time.sleep(10)"
        tool = Tool("probe", "probe", ("{python}", "-c", parent))
        result, code = dev.execute(tool, [], timeout=0.25)
        self.assertEqual((code, result["status"]), (124, "timeout"))
        time.sleep(0.65)
        self.assertFalse(marker.exists())

    def test_status_reads_manifests_without_running_native_tools(self):
        inventory = {"executable": sys.executable, "packages": []}
        self.write("gateway-go/go.mod", "module example\ngo 1.99.0\n")
        self.write("andromeda/package.json", '{"packageManager":"pnpm@99.0.0"}')
        self.write("pyproject.toml", '[tool.ruff]\ntarget-version = "py312"\n')
        with mock.patch.object(dev, "ROOT", self.root), \
             mock.patch.object(dev, "package_inventory", return_value=inventory), \
             mock.patch.object(subprocess, "Popen", side_effect=AssertionError("must not execute")):
            result = dev.status()
        self.assertEqual(result["requirements"]["go"], "1.99.0")
        self.assertEqual(result["requirements"]["andromeda_package_manager"], "pnpm@99.0.0")
        self.assertEqual(result["python"], inventory)
        self.assertNotIn(dev.PYTHON_REPAIR[0], result["recovery"])
        self.assertIn(["pnpm", "--dir", "andromeda", "install", "--frozen-lockfile"], result["recovery"])

    def test_malformed_repository_manifest_returns_structured_failure(self):
        self.write("andromeda/package.json", "{")
        with mock.patch.object(dev, "ROOT", self.root):
            code, result = self.invoke(["audit"])
        self.assertEqual((code, result["status"]), (1, "failed"))
        self.assertIn(["./dev", "audit"], result["recovery"])


if __name__ == "__main__":
    unittest.main()
