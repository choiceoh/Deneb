"""Behavior tests for the three fail-open CodeGraph hook scripts."""

from __future__ import annotations

import hashlib
import json
import os
import shlex
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

from test_support import REPO_ROOT, invoke_main, load_script

sys.path.insert(0, str(REPO_ROOT / "scripts/dev"))
import codegraph_worktree_index as worktree_index

nudge = load_script("scripts/dev/codegraph-nudge.py")
autoindex = load_script("scripts/dev/codegraph-autoindex.py")
remind = load_script("scripts/dev/codegraph-remind.py")


class NudgeParserTests(unittest.TestCase):
    def test_bare_identifier_accepts_code_names_but_rejects_text_searches(self) -> None:
        accepted = ["Foo", "foo_bar", "Handler123", "_private"]
        rejected = ["", "ab", "two words", "pkg.Symbol", "foo-bar", "^regex$", "/path"]
        for value in accepted:
            with self.subTest(value=value):
                self.assertTrue(nudge.bare_identifier(value))
        for value in rejected:
            with self.subTest(value=value):
                self.assertFalse(nudge.bare_identifier(value))

    def test_pattern_from_simple_grep_commands_ignores_flags_and_respects_quotes(self) -> None:
        cases = {
            "rg -n Handler gateway-go": "Handler",
            "grep --fixed-strings 'two words' file": "two words",
            "/usr/bin/egrep -i Symbol .": "Symbol",
            "echo before && ripgrep Target": "Target",
        }
        for command, expected in cases.items():
            with self.subTest(command=command):
                self.assertEqual(nudge.pattern_from_bash(command), expected)

    def test_pattern_parser_fails_open_for_non_grep_unbalanced_or_codegraph(self) -> None:
        for command in (
            "find . -name '*.go'",
            "rg 'unterminated",
            "codegraph node Handler && rg Handler",
            "echo grep",
        ):
            with self.subTest(command=command):
                self.assertEqual(nudge.pattern_from_bash(command), "")


class NudgeResolutionTests(unittest.TestCase):
    def test_when_codegraph_binary_uses_first_resolvable_candidate(self) -> None:
        def which(candidate):
            return "/custom/codegraph" if candidate.endswith(".local/bin/codegraph") else None

        with mock.patch.object(nudge.shutil, "which", side_effect=which) as which_mock:
            self.assertEqual(nudge.codegraph_bin(), "/custom/codegraph")
        self.assertEqual(which_mock.call_args_list[0], mock.call("codegraph"))
        self.assertTrue(which_mock.call_args_list[1].args[0].endswith(".local/bin/codegraph"))

    def test_missing_codegraph_does_not_spawn_a_query(self) -> None:
        with mock.patch.object(nudge, "codegraph_bin", return_value=None):
            with mock.patch.object(nudge.subprocess, "run") as run:
                self.assertFalse(nudge.is_defined_symbol("Handler", "/repo"))
        run.assert_not_called()

    def test_symbol_query_allows_supported_json_shapes_and_requires_exact_case(self) -> None:
        payloads = [
            [{"name": "Handler"}],
            {"results": [{"node": {"name": "Handler"}}]},
            {"nodes": [{"name": "Handler"}]},
        ]
        with mock.patch.object(nudge, "codegraph_bin", return_value="/bin/codegraph"):
            for payload in payloads:
                with self.subTest(payload=payload):
                    completed = subprocess.CompletedProcess([], 0, stdout=json.dumps(payload), stderr="")
                    with mock.patch.object(nudge.subprocess, "run", return_value=completed) as run:
                        self.assertTrue(nudge.is_defined_symbol("Handler", "/repo"))
                    args, kwargs = run.call_args
                    self.assertEqual(args[0], ["bash", "-lc", "codegraph query Handler --json"])
                    self.assertEqual(kwargs["cwd"], "/repo")
                    self.assertEqual(kwargs["timeout"], 8)

            wrong_case = subprocess.CompletedProcess(
                [], 0, stdout='[{"name":"handler"}]', stderr=""
            )
            with mock.patch.object(nudge.subprocess, "run", return_value=wrong_case):
                self.assertFalse(nudge.is_defined_symbol("Handler", "/repo"))

    def test_query_errors_malformed_json_and_timeout_fail_open(self) -> None:
        failures = [
            subprocess.CompletedProcess([], 1, stdout="[]", stderr="failed"),
            subprocess.CompletedProcess([], 0, stdout="", stderr=""),
            subprocess.CompletedProcess([], 0, stdout="not-json", stderr=""),
            subprocess.TimeoutExpired(["codegraph"], 8),
            OSError("missing bash"),
        ]
        with mock.patch.object(nudge, "codegraph_bin", return_value="/bin/codegraph"):
            for failure in failures:
                with self.subTest(failure=repr(failure)):
                    if isinstance(failure, Exception):
                        patcher = mock.patch.object(nudge.subprocess, "run", side_effect=failure)
                    else:
                        patcher = mock.patch.object(nudge.subprocess, "run", return_value=failure)
                    with patcher:
                        self.assertFalse(nudge.is_defined_symbol("Handler", "/repo"))

    def test_rpcmap_resolution_requires_script_success_and_nonempty_json(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            script = root / "scripts/dev/rpcmap.py"
            script.parent.mkdir(parents=True)
            script.write_text("# fixture", encoding="utf-8")

            success = subprocess.CompletedProcess([], 0, stdout='[{"name":"miniapp.x"}]', stderr="")
            with mock.patch.object(nudge.subprocess, "run", return_value=success) as run:
                self.assertTrue(nudge.resolves_via_rpcmap("miniapp.x", str(root)))
            self.assertEqual(
                run.call_args.args[0],
                [sys.executable, str(script), "miniapp.x", "--json", "--root", str(root)],
            )
            self.assertEqual(run.call_args.kwargs["timeout"], 6)

            for completed in (
                subprocess.CompletedProcess([], 0, stdout="[]", stderr=""),
                subprocess.CompletedProcess([], 0, stdout="", stderr=""),
                subprocess.CompletedProcess([], 1, stdout='[{"name":"x"}]', stderr=""),
            ):
                with self.subTest(completed=completed):
                    with mock.patch.object(nudge.subprocess, "run", return_value=completed):
                        self.assertFalse(nudge.resolves_via_rpcmap("miniapp.x", str(root)))
            with mock.patch.object(
                nudge.subprocess,
                "run",
                side_effect=subprocess.TimeoutExpired(["rpcmap"], 6),
            ):
                self.assertFalse(nudge.resolves_via_rpcmap("miniapp.x", str(root)))

        self.assertFalse(nudge.resolves_via_rpcmap("miniapp.x", "/missing/repo"))


class NudgeMainTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "repo"
        self.markers = Path(self.tmp.name) / "markers"
        self.root.mkdir()
        self.markers.mkdir()

    def invoke(self, payload):
        with mock.patch.object(nudge.tempfile, "gettempdir", return_value=str(self.markers)):
            return invoke_main(
                nudge,
                stdin=json.dumps(payload),
                env={"CLAUDE_PROJECT_DIR": str(self.root)},
            )

    def test_when_known_symbol_blocks_once_per_sanitized_session_and_pattern(self) -> None:
        payload = {
            "session_id": "session/one",
            "tool_name": "Grep",
            "tool_input": {"pattern": "Handler"},
        }
        with mock.patch.object(nudge, "is_defined_symbol", return_value=True):
            rc, stdout, stderr = self.invoke(payload)
            self.assertEqual((rc, stdout), (2, ""))
            self.assertIn("codegraph callers Handler", stderr)
            self.assertIn("세션당 1회", stderr)

            self.assertEqual(self.invoke(payload), (0, "", ""))

        marker_dir = self.markers / "codegraph-nudge-session_one"
        marker = marker_dir / hashlib.sha1(b"Handler").hexdigest()
        self.assertTrue(marker.is_file())

    def test_when_bash_pattern_and_dotted_rpc_take_the_expected_resolver_paths(self) -> None:
        canonical_root = os.path.realpath(self.root)
        bash_payload = {
            "tool_name": "Bash",
            "tool_input": {"command": "rg UnknownSymbol gateway-go"},
        }
        with mock.patch.object(nudge, "is_defined_symbol", return_value=False) as symbol:
            self.assertEqual(self.invoke(bash_payload), (0, "", ""))
        symbol.assert_called_once_with("UnknownSymbol", canonical_root)

        rpc_payload = {
            "session_id": "rpc",
            "tool_name": "Grep",
            "tool_input": {"pattern": "miniapp.people.list"},
        }
        with mock.patch.object(nudge, "resolves_via_rpcmap", return_value=True) as resolver:
            rc, _, stderr = self.invoke(rpc_payload)
        self.assertEqual(rc, 2)
        self.assertIn("codegraph node miniapp.people.list", stderr)
        self.assertIn("scripts/dev/rpcmap.py miniapp.people.list", stderr)
        resolver.assert_called_once_with("miniapp.people.list", canonical_root)

    def test_other_tools_regexes_and_unknown_dotted_names_pass(self) -> None:
        payloads = [
            {"tool_name": "Read", "tool_input": {"pattern": "Handler"}},
            {"tool_name": "Grep", "tool_input": {"pattern": "foo.*bar"}},
        ]
        for payload in payloads:
            with self.subTest(payload=payload):
                self.assertEqual(self.invoke(payload), (0, "", ""))

        dotted = {"tool_name": "Grep", "tool_input": {"pattern": "package.json"}}
        with mock.patch.object(nudge, "resolves_via_rpcmap", return_value=False):
            self.assertEqual(self.invoke(dotted), (0, "", ""))


class AutoindexTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.parent = Path(self.tmp.name)
        self.root = self.parent / "worktree"
        self.marker_root = self.parent / "tmp"
        self.root.mkdir()
        self.marker_root.mkdir()
        (self.root / ".git").write_text("gitdir: fixture", encoding="utf-8")

    def invoke(self, *, stdin="{}", popen=None):
        patches = [
            mock.patch.object(worktree_index, "codegraph_bin", return_value="/bin/codegraph"),
            mock.patch.object(worktree_index.tempfile, "gettempdir", return_value=str(self.marker_root)),
        ]
        for patcher in patches:
            patcher.start()
            self.addCleanup(patcher.stop)
        if popen is None:
            popen = mock.Mock(return_value=mock.Mock())
        with mock.patch.object(worktree_index.subprocess, "Popen", popen):
            result = invoke_main(
                autoindex,
                stdin=stdin,
                env={"CLAUDE_PROJECT_DIR": str(self.root)},
            )
        return result, popen

    def test_binary_resolution_and_missing_prerequisites_fail_open(self) -> None:
        with mock.patch.object(worktree_index.shutil, "which", side_effect=lambda p: p if p == "codegraph" else None):
            self.assertEqual(worktree_index.codegraph_bin(), "codegraph")
        with mock.patch.object(worktree_index.shutil, "which", return_value=None):
            self.assertIsNone(worktree_index.codegraph_bin())

        with mock.patch.object(worktree_index, "codegraph_bin", return_value=None):
            with mock.patch.object(worktree_index.subprocess, "Popen") as popen:
                result = invoke_main(
                    autoindex,
                    stdin="{invalid json",
                    env={"CLAUDE_PROJECT_DIR": str(self.root)},
                )
        self.assertEqual(result, (0, "", ""))
        popen.assert_not_called()

        (self.root / ".git").unlink()
        result, popen = self.invoke()
        self.assertEqual(result, (0, "", ""))
        popen.assert_not_called()

    def test_when_existing_index_or_fresh_lock_suppresses_duplicate_background_work(self) -> None:
        (self.root / ".codegraph").mkdir()
        result, popen = self.invoke()
        self.assertEqual(result, (0, "", ""))
        popen.assert_not_called()
        (self.root / ".codegraph").rmdir()

        key = hashlib.sha1(str(self.root.resolve()).encode()).hexdigest()
        lock = self.marker_root / f"codegraph-autoindex-{key}"
        lock.write_text("", encoding="utf-8")
        result, popen = self.invoke()
        self.assertEqual(result, (0, "", ""))
        popen.assert_not_called()

    def test_when_no_donor_launches_first_time_init_and_refreshes_stale_lock(self) -> None:
        key = hashlib.sha1(str(self.root.resolve()).encode()).hexdigest()
        lock = self.marker_root / f"codegraph-autoindex-{key}"
        lock.write_text("stale", encoding="utf-8")
        old = time.time() - 901
        os.utime(lock, (old, old))

        result, popen = self.invoke()
        self.assertEqual(result, (0, "", ""))
        popen.assert_called_once()
        command = popen.call_args.args[0]
        self.assertEqual(command[:2], ["bash", "-lc"])
        canonical_root = shlex.quote(os.path.realpath(self.root))
        self.assertIn(f"cd {canonical_root}", command[2])
        self.assertIn("codegraph init", command[2])
        self.assertIn("rpcmap_codegraph_sync.py", command[2])
        self.assertTrue(popen.call_args.kwargs["start_new_session"])
        self.assertIs(popen.call_args.kwargs["stderr"], subprocess.STDOUT)
        self.assertTrue(popen.call_args.kwargs["stdout"].closed)
        self.assertGreater(lock.stat().st_mtime, old)

    def test_freshest_donor_is_seeded_then_sync_has_init_fallback(self) -> None:
        old_donor = self.parent / "old/.codegraph"
        fresh_donor = self.parent / "fresh/.codegraph"
        old_donor.mkdir(parents=True)
        fresh_donor.mkdir(parents=True)
        now = time.time()
        os.utime(old_donor, (now - 100, now - 100))
        os.utime(fresh_donor, (now, now))

        result, popen = self.invoke()
        self.assertEqual(result, (0, "", ""))
        command = popen.call_args.args[0][2]
        canonical_fresh = shlex.quote(os.path.realpath(fresh_donor))
        canonical_old = shlex.quote(os.path.realpath(old_donor))
        self.assertIn("codegraph-seed-index.sh", command)
        self.assertIn(canonical_fresh, command)
        self.assertNotIn(canonical_old, command)
        self.assertIn("rm -rf", command)
        self.assertIn("codegraph init", command)
        self.assertTrue(popen.call_args.kwargs["stdout"].closed)

    def test_background_spawn_error_is_swallowed(self) -> None:
        popen = mock.Mock(side_effect=OSError("cannot spawn"))
        result, _ = self.invoke(popen=popen)
        self.assertEqual(result, (0, "", ""))
        popen.assert_called_once()


class ReminderTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "repo"
        self.markers = Path(self.tmp.name) / "markers"
        self.root.mkdir()
        self.markers.mkdir()

    def invoke(self, payload):
        with mock.patch.object(remind.tempfile, "gettempdir", return_value=str(self.markers)):
            return invoke_main(
                remind,
                stdin=json.dumps(payload),
                env={"CLAUDE_PROJECT_DIR": str(self.root)},
            )

    def test_first_source_file_emits_exact_allow_contract_then_suppresses(self) -> None:
        payload = {
            "session_id": "session/one",
            "cwd": str(self.root),
            "tool_input": {"file_path": str(self.root / "src/App.PY")},
        }
        rc, stdout, stderr = self.invoke(payload)
        self.assertEqual((rc, stderr), (0, ""))
        output = json.loads(stdout)
        hook = output["hookSpecificOutput"]
        self.assertEqual(hook["hookEventName"], "PreToolUse")
        self.assertEqual(hook["permissionDecision"], "allow")
        self.assertEqual(hook["additionalContext"], remind.REMINDER)
        self.assertTrue((self.markers / "codegraph-remind-session_one").is_file())

        self.assertEqual(self.invoke(payload), (0, "", ""))

    def test_non_source_missing_path_and_outside_repo_do_nothing(self) -> None:
        payloads = [
            {"tool_input": {}},
            {"tool_input": {"file_path": str(self.root / "README.md")}},
            {"tool_input": {"file_path": str(Path(self.tmp.name) / "outside.go")}},
        ]
        for payload in payloads:
            with self.subTest(payload=payload):
                self.assertEqual(self.invoke(payload), (0, "", ""))
        self.assertEqual(list(self.markers.iterdir()), [])

    def test_notebook_path_with_source_extension_is_supported(self) -> None:
        payload = {
            "session_id": "notebook",
            "tool_input": {"notebook_path": str(self.root / "generated/code.kt")},
        }
        rc, stdout, _ = self.invoke(payload)
        self.assertEqual(rc, 0)
        self.assertEqual(json.loads(stdout)["hookSpecificOutput"]["permissionDecision"], "allow")


class CursorCodegraphServeTests(unittest.TestCase):
    def test_mcp_wrappers_keep_worktree_binding_and_tool_surface(self) -> None:
        cursor = json.loads((REPO_ROOT / ".cursor/mcp.json").read_text(encoding="utf-8"))
        repo = json.loads((REPO_ROOT / ".mcp.json").read_text(encoding="utf-8"))
        cursor_cg = cursor["mcpServers"]["codegraph"]
        repo_cg = repo["mcpServers"]["codegraph"]
        self.assertEqual(cursor_cg["command"], "bash")
        self.assertIn("cursor-codegraph-serve.sh", cursor_cg["args"][0])
        self.assertEqual(
            cursor_cg["env"]["CODEGRAPH_MCP_TOOLS"],
            "explore,node,search,impact,callers,callees",
        )
        self.assertNotIn("--path", cursor_cg["args"])
        self.assertEqual(repo_cg["command"], "bash")
        self.assertIn("codegraph-serve.sh", repo_cg["args"][0])
        self.assertEqual(
            repo_cg["env"]["CODEGRAPH_MCP_TOOLS"],
            "explore,node,search,impact,callers,callees",
        )

    def test_print_root_refuses_production_checkout(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            prod = home / "deneb"
            dev = home / "deneb-dev"
            prod.mkdir()
            dev.mkdir()
            script = REPO_ROOT / "scripts/dev/cursor-codegraph-serve.sh"
            env = os.environ.copy()
            env["HOME"] = str(home)
            env["CURSOR_CODEGRAPH_FALLBACK"] = str(prod)
            env.pop("CURSOR_WORKTREE", None)
            env.pop("DENEB_AGENT_ROOT", None)
            env.pop("CURSOR_SESSION_ID", None)
            proc = subprocess.run(
                ["bash", str(script), "--print-root"],
                capture_output=True,
                text=True,
                check=False,
                env=env,
            )
            self.assertEqual(proc.returncode, 0, proc.stderr)
            self.assertEqual(proc.stdout.strip(), str(dev))


class HookEntryPointTests(unittest.TestCase):
    def test_malformed_json_never_breaks_nudge_or_reminder(self) -> None:
        for module in (nudge, remind):
            with self.subTest(script=module.__file__):
                proc = subprocess.run(
                    [sys.executable, module.__file__],
                    input="{bad json",
                    capture_output=True,
                    text=True,
                    check=False,
                )
                self.assertEqual(proc.returncode, 0)
                self.assertEqual(proc.stdout, "")
                self.assertEqual(proc.stderr, "")


if __name__ == "__main__":
    unittest.main()
