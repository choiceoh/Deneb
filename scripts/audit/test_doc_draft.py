"""Grounding, failure, determinism, and CLI tests for doc-draft."""

from __future__ import annotations

import contextlib
import io
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

DEV_DIR = Path(__file__).resolve().parents[1] / "dev"
sys.path.insert(0, str(DEV_DIR))

from test_support import JSONResponse, invoke_main, load_script, write_files  # noqa: E402

doc_draft = load_script("scripts/audit/doc-draft.py")


class ModelTransportTests(unittest.TestCase):
    def test_wormhole_token_expands_environment_from_closed_config_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            config = Path(tmp) / "config.json"
            config.write_text('{"token":"$DOC_DRAFT_TOKEN"}', encoding="utf-8")
            with mock.patch.object(doc_draft, "WORMHOLE_CFG", str(config)):
                with mock.patch.dict(doc_draft.os.environ, {"DOC_DRAFT_TOKEN": "expanded-secret"}):
                    self.assertEqual(doc_draft.wormhole_token(), "expanded-secret")

    def test_model_request_preserves_wire_shape_auth_and_long_timeout(self) -> None:
        captured = {}

        def urlopen(request, timeout):
            captured["request"] = request
            captured["timeout"] = timeout
            return JSONResponse({"choices": [{"message": {"content": "draft text"}}]})

        with mock.patch.object(doc_draft, "wormhole_token", return_value="wormhole-token"):
            with mock.patch.object(doc_draft.urllib.request, "urlopen", side_effect=urlopen):
                result = doc_draft.call_model("glm", "system", "user", max_tokens=4321)

        self.assertEqual(result, "draft text")
        request = captured["request"]
        self.assertEqual(request.full_url, doc_draft.WORMHOLE_URL)
        self.assertEqual(request.get_header("Authorization"), "Bearer wormhole-token")
        self.assertEqual(request.get_header("Content-type"), "application/json")
        self.assertEqual(captured["timeout"], 300)
        self.assertEqual(json.loads(request.data), {
            "model": "glm",
            "messages": [
                {"role": "system", "content": "system"},
                {"role": "user", "content": "user"},
            ],
            "max_tokens": 4321,
            "stream": False,
        })


class SubprocessBoundaryTests(unittest.TestCase):
    def test_run_returns_stdout_even_for_nonzero_command(self) -> None:
        completed = subprocess.CompletedProcess(["tool"], 7, stdout="partial output", stderr="failure")
        with mock.patch.object(doc_draft.subprocess, "run", return_value=completed) as run:
            self.assertEqual(doc_draft._run(["tool"], cwd="/repo", timeout=3), "partial output")
        run.assert_called_once_with(
            ["tool"], cwd="/repo", capture_output=True, text=True, timeout=3
        )

    def test_run_converts_spawn_error_and_timeout_to_grounding_text(self) -> None:
        failures = [
            OSError("missing executable"),
            subprocess.TimeoutExpired(["slow"], 9),
        ]
        for failure in failures:
            with self.subTest(failure=repr(failure)):
                with mock.patch.object(doc_draft.subprocess, "run", side_effect=failure):
                    result = doc_draft._run(["tool"], timeout=9)
                self.assertTrue(result.startswith("(command failed:"))
                self.assertTrue(result.endswith(")"))

    def test_codegraph_uses_login_path_context_and_prefers_stdout(self) -> None:
        completed = subprocess.CompletedProcess([], 0, stdout="graph output", stderr="diagnostic")
        with mock.patch.dict(doc_draft.os.environ, {"PATH": "/usr/bin"}, clear=True):
            with mock.patch.object(doc_draft.subprocess, "run", return_value=completed) as run:
                result = doc_draft._codegraph("mail", "runtime/mail")
        self.assertEqual(result, "graph output")
        args, kwargs = run.call_args
        self.assertEqual(args[0], [
            "codegraph",
            "explore",
            "mail subsystem: entry points and end-to-end flow (runtime/mail)",
        ])
        self.assertEqual(kwargs["cwd"], doc_draft.REPO)
        self.assertEqual(kwargs["timeout"], 120)
        self.assertTrue(kwargs["env"]["PATH"].endswith(":/usr/bin"))
        self.assertIn(".local/bin", kwargs["env"]["PATH"])

    def test_codegraph_uses_stderr_fallback_caps_output_and_handles_failures(self) -> None:
        stderr_only = subprocess.CompletedProcess([], 1, stdout="", stderr="reason")
        with mock.patch.object(doc_draft.subprocess, "run", return_value=stderr_only):
            self.assertEqual(doc_draft._codegraph("x", "y"), "reason")

        huge = subprocess.CompletedProcess([], 0, stdout="x" * 12_005, stderr="")
        with mock.patch.object(doc_draft.subprocess, "run", return_value=huge):
            self.assertEqual(len(doc_draft._codegraph("x", "y")), 12_000)

        for failure in (OSError("missing"), subprocess.TimeoutExpired(["codegraph"], 120)):
            with self.subTest(failure=repr(failure)):
                with mock.patch.object(doc_draft.subprocess, "run", side_effect=failure):
                    self.assertEqual(doc_draft._codegraph("x", "y"), "(codegraph unavailable)")


class RepositoryGroundingTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.repo = Path(self.tmp.name) / "repo"
        self.gateway = self.repo / "gateway-go"
        self.internal = self.gateway / "internal"
        self.rules = self.repo / "docs/agent-rules"
        self.internal.mkdir(parents=True)
        self.rules.mkdir(parents=True)
        patcher = mock.patch.multiple(
            doc_draft,
            REPO=str(self.repo),
            GATEWAY=str(self.gateway),
            INTERNAL=str(self.internal),
            RULES_DIR=str(self.rules),
        )
        patcher.start()
        self.addCleanup(patcher.stop)

    def test_package_discovery_includes_only_directories_with_non_test_go(self) -> None:
        write_files(self.internal, {
            "domain/example/root.go": "package example\n",
            "domain/example/root_test.go": "package example\n",
            "domain/example/sub/worker.go": "package sub\n",
            "domain/example/testonly/worker_test.go": "package testonly\n",
            "domain/example/readme.md": "not Go",
        })
        self.assertEqual(
            doc_draft._pkg_dirs("domain/example"),
            ["domain/example", "domain/example/sub"],
        )

    def test_go_doc_caps_lines_and_reports_exact_omission(self) -> None:
        with mock.patch.object(doc_draft, "_run", return_value="one\ntwo\nthree\nfour\nfive") as run:
            result = doc_draft._go_doc("domain/example", cap_lines=3)
        self.assertEqual(
            result,
            "one\ntwo\nthree\n... (+2 more lines of exported API)",
        )
        run.assert_called_once_with(
            ["go", "doc", "-all", "./internal/domain/example"],
            cwd=str(self.gateway),
            timeout=90,
        )

    def test_file_map_ignores_tests_sorts_by_loc_and_uses_repo_relative_paths(self) -> None:
        write_files(self.internal, {
            "domain/example/small.go": "package example\nvar X = 1\n",
            "domain/example/big.go": "package example\n\nfunc A() {}\nfunc B() {}\n",
            "domain/example/big_test.go": "package example\n" * 20,
        })
        lines = doc_draft._file_map("domain/example").splitlines()
        self.assertEqual(lines[0].strip(), "4 LOC  gateway-go/internal/domain/example/big.go")
        self.assertEqual(lines[1].strip(), "2 LOC  gateway-go/internal/domain/example/small.go")
        self.assertNotIn("_test.go", "\n".join(lines))

    def test_rule_globs_reads_only_markdown_and_extracts_internal_patterns(self) -> None:
        write_files(self.rules, {
            "mail.md": "globs:\n  - gateway-go/internal/runtime/mail/**/*.go\n",
            "wiki.md": "path internal/domain/wiki/* and internal/domain/wiki/store.go\n",
            "ignored.txt": "internal/ignored/**",
        })
        self.assertEqual(doc_draft._rule_globs(), [
            "internal/runtime/mail/**/*.go",
            "internal/domain/wiki/*",
            "internal/domain/wiki/store.go",
        ])

    def test_gap_report_is_loc_sorted_and_marks_only_uncovered_packages(self) -> None:
        write_files(self.internal, {
            "big/main.go": "package big\n\nfunc A() {}\nfunc B() {}\nfunc C() {}\n",
            "covered/main.go": "package covered\n\nfunc A() {}\nfunc B() {}\n",
            "documented/main.go": "package documented\nfunc A() {}\n",
            "documented/CLAUDE.md": "narrative",
            "deep/a/b/c/main.go": "package c\n" * 30,
            "testonly/main_test.go": "package testonly\n" * 40,
        })
        write_files(self.rules, {
            "covered.md": "globs:\n  - gateway-go/internal/covered/**/*.go\n",
        })
        stdout = io.StringIO()
        with contextlib.redirect_stdout(stdout):
            doc_draft.list_gaps(top=10)
        output = stdout.getvalue()
        rows = [
            line for line in output.splitlines()
            if "  big" in line or "  covered" in line or "  documented" in line
        ]
        self.assertEqual(len(rows), 3)
        self.assertIn("← GAP", next(line for line in rows if "  big" in line))
        self.assertNotIn("← GAP", next(line for line in rows if "  covered" in line))
        self.assertNotIn("← GAP", next(line for line in rows if "  documented" in line))
        self.assertLess(output.index("  big"), output.index("  covered"))
        self.assertNotIn("deep/a/b/c", output)
        self.assertNotIn("testonly", output)


class ContextAndPromptTests(unittest.TestCase):
    def test_gather_context_orders_file_map_api_sections_then_call_graph(self) -> None:
        with mock.patch.object(doc_draft, "_pkg_dirs", return_value=["domain/a", "domain/b"]):
            with mock.patch.object(doc_draft, "_file_map", return_value="FILE MAP"):
                with mock.patch.object(doc_draft, "_go_doc", side_effect=["API A", "API B"]) as go_doc:
                    with mock.patch.object(doc_draft, "_codegraph", return_value="GRAPH"):
                        result = doc_draft.gather_context("example", "domain")
        self.assertLess(result.index("# FILE MAP"), result.index("API A"))
        self.assertLess(result.index("API A"), result.index("API B"))
        self.assertLess(result.index("API B"), result.index("# CALL GRAPH"))
        self.assertTrue(result.endswith("GRAPH"))
        self.assertEqual(go_doc.call_args_list, [mock.call("domain/a"), mock.call("domain/b")])

    def test_context_budget_keeps_call_graph_when_api_sections_are_omitted(self) -> None:
        with mock.patch.object(doc_draft, "CONTEXT_CHAR_BUDGET", 6_100):
            with mock.patch.object(doc_draft, "_pkg_dirs", return_value=["a", "b"]):
                with mock.patch.object(doc_draft, "_file_map", return_value="MAP"):
                    with mock.patch.object(doc_draft, "_go_doc") as go_doc:
                        with mock.patch.object(doc_draft, "_codegraph", return_value="GRAPH"):
                            result = doc_draft.gather_context("name", "target")
        go_doc.assert_called_once_with("a")
        self.assertIn("(+2 packages; remaining API omitted for budget)", result)
        self.assertTrue(result.endswith("GRAPH"))

    def test_prompt_is_grounded_in_target_context_and_exemplar(self) -> None:
        system, user = doc_draft.build_prompt("mail-flow", "runtime/mail", "GROUNDING", "EXEMPLAR")
        self.assertIn("Ground EVERY claim", system)
        self.assertIn("Output ONLY the markdown document", system)
        self.assertIn("internal/runtime/mail", user)
        self.assertIn("EXEMPLAR", user)
        self.assertIn("GROUNDING", user)
        self.assertTrue(user.rstrip().endswith("`mail-flow`."))

    def test_fence_stripping_is_idempotent_and_always_ends_with_newline(self) -> None:
        fenced = "```markdown\n---\n# Title\n```"
        self.assertEqual(doc_draft.strip_fences(fenced), "---\n# Title\n")
        self.assertEqual(doc_draft.strip_fences("  # Title  "), "# Title\n")
        self.assertEqual(doc_draft.strip_fences("```\n# Unclosed"), "# Unclosed\n")


class MainContractTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.repo = Path(self.tmp.name) / "repo"
        self.gateway = self.repo / "gateway-go"
        self.internal = self.gateway / "internal"
        self.rules = self.repo / "docs/agent-rules"
        self.target = self.internal / "domain/example"
        self.target.mkdir(parents=True)
        self.rules.mkdir(parents=True)
        (self.rules / doc_draft.DEFAULT_EXEMPLAR).write_text("# Exemplar\n", encoding="utf-8")
        patcher = mock.patch.multiple(
            doc_draft,
            REPO=str(self.repo),
            GATEWAY=str(self.gateway),
            INTERNAL=str(self.internal),
            RULES_DIR=str(self.rules),
        )
        patcher.start()
        self.addCleanup(patcher.stop)

    def test_list_gaps_mode_does_not_require_target_or_model(self) -> None:
        with mock.patch.object(doc_draft, "list_gaps") as list_gaps:
            rc, stdout, stderr = invoke_main(doc_draft, ["--list-gaps"])
        self.assertEqual((rc, stdout, stderr), (0, "", ""))
        list_gaps.assert_called_once_with()

    def test_missing_selector_and_unknown_target_keep_exit_contracts(self) -> None:
        rc, stdout, stderr = invoke_main(doc_draft, [])
        self.assertEqual((rc, stdout), (2, ""))
        self.assertIn("either --list-gaps", stderr)

        rc, stdout, stderr = invoke_main(
            doc_draft,
            ["--target", "missing", "--name", "unknown"],
        )
        self.assertEqual((rc, stdout), (2, ""))
        self.assertIn("no such package: internal/missing", stderr)

    def test_model_failure_preserves_existing_draft_file(self) -> None:
        output = self.repo / "existing.draft.md"
        output.write_text("previous complete draft\n", encoding="utf-8")
        with mock.patch.object(doc_draft, "gather_context", return_value="CONTEXT"):
            with mock.patch.object(doc_draft, "call_model", side_effect=TimeoutError("model slow")):
                rc, stdout, stderr = invoke_main(
                    doc_draft,
                    [
                        "--target", "domain/example",
                        "--name", "example",
                        "--out", str(output),
                    ],
                )
        self.assertEqual((rc, stdout), (1, ""))
        self.assertIn("model call failed: model slow", stderr)
        self.assertEqual(output.read_text(encoding="utf-8"), "previous complete draft\n")

    def test_successful_draft_strips_fence_writes_requested_path_and_reports_it(self) -> None:
        output = self.repo / "out/custom.draft.md"
        output.parent.mkdir()
        raw = "```markdown\n---\ndescription: fixture\n---\n# Fixture\n```"
        with mock.patch.object(doc_draft, "gather_context", return_value="CONTEXT"):
            with mock.patch.object(doc_draft, "call_model", return_value=raw) as call:
                rc, stdout, stderr = invoke_main(
                    doc_draft,
                    [
                        "--target", "domain/example",
                        "--name", "example",
                        "--model", "local-model",
                        "--out", str(output),
                    ],
                )
        self.assertEqual(rc, 0)
        self.assertIn("gathering grounding", stderr)
        self.assertIn("drafting with local-model", stderr)
        self.assertEqual(
            output.read_text(encoding="utf-8"),
            "---\ndescription: fixture\n---\n# Fixture\n",
        )
        self.assertIn("doc-draft: wrote out/custom.draft.md", stdout)
        self.assertIn("→ DRAFT", stdout)
        system, user = call.call_args.args[1:3]
        self.assertIn("Ground EVERY claim", system)
        self.assertIn("CONTEXT", user)

    def test_real_help_entrypoint_preserves_modes_and_generation_options(self) -> None:
        proc = subprocess.run(
            [sys.executable, doc_draft.__file__, "--help"],
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(proc.returncode, 0)
        for option in ("--list-gaps", "--target", "--name", "--model", "--exemplar", "--out"):
            self.assertIn(option, proc.stdout)


if __name__ == "__main__":
    unittest.main()
