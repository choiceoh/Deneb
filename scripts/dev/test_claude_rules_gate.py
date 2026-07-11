"""Parser and once-per-session tests for the path-scoped rules hook."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from test_support import invoke_main, load_script, write_files

rules_gate = load_script("scripts/dev/claude-rules-gate.py")


class FrontmatterParserTests(unittest.TestCase):
    def test_missing_or_unterminated_frontmatter_is_ignored(self) -> None:
        self.assertEqual(rules_gate.parse_frontmatter("# Rule\nbody"), ("", []))
        self.assertEqual(
            rules_gate.parse_frontmatter("---\ndescription: incomplete\nglobs: '*.go'"),
            ("", []),
        )

    def test_json_array_frontmatter_preserves_description_and_order(self) -> None:
        text = """---
description: "Go and Kotlin rule"
globs: ["gateway-go/**/*.go", "client-android/**/*.kt"]
---
# Body
"""
        self.assertEqual(
            rules_gate.parse_frontmatter(text),
            ("Go and Kotlin rule", ["gateway-go/**/*.go", "client-android/**/*.kt"]),
        )

    def test_yaml_list_and_comma_separated_forms_strip_quotes_and_blanks(self) -> None:
        yaml_text = """---
globs:
  - 'scripts/**/*.py'
  - "gateway-go/*.go"
description: list form
---
"""
        self.assertEqual(
            rules_gate.parse_frontmatter(yaml_text),
            ("list form", ["scripts/**/*.py", "gateway-go/*.go"]),
        )

        csv_text = """---
description: 'csv form'
globs: '*.md', "docs/**/*.txt", ,
---
"""
        self.assertEqual(
            rules_gate.parse_frontmatter(csv_text),
            ("csv form", ["*.md", "docs/**/*.txt"]),
        )

    def test_malformed_json_array_falls_back_to_tolerant_split(self) -> None:
        text = """---
globs: [*.go, 'scripts/*.py']
---
"""
        self.assertEqual(
            rules_gate.parse_frontmatter(text),
            ("", ["*.go", "scripts/*.py"]),
        )

    def test_yaml_list_stops_at_next_key_instead_of_swallowing_it(self) -> None:
        text = """---
globs:
  - first/**
unknown: value
description: after list
---
"""
        self.assertEqual(rules_gate.parse_frontmatter(text), ("after list", ["first/**"]))


class GlobMatchingTests(unittest.TestCase):
    def test_double_star_and_single_star_match_nested_paths(self) -> None:
        self.assertTrue(
            rules_gate.glob_matches(
                "gateway-go/internal/runtime/server/server.go",
                "gateway-go/**/*.go",
            )
        )
        self.assertTrue(rules_gate.glob_matches("scripts/dev/checks.py", "scripts/*.py"))

    def test_matching_is_case_sensitive_and_does_not_accept_wrong_suffix(self) -> None:
        self.assertFalse(rules_gate.glob_matches("Scripts/dev/checks.py", "scripts/**/*.py"))
        self.assertFalse(rules_gate.glob_matches("scripts/dev/checks.pyc", "scripts/**/*.py"))

    def test_bare_double_star_can_span_multiple_segments(self) -> None:
        self.assertTrue(rules_gate.glob_matches("a/deep/tree/file.go", "a/**/file.go"))
        self.assertFalse(rules_gate.glob_matches("b/deep/tree/file.go", "a/**/file.go"))


class RulesGateMainTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "repo"
        self.marker_root = Path(self.tmp.name) / "markers"
        self.root.mkdir()
        self.marker_root.mkdir()

    def payload(self, path: Path, *, session="session/one", field="file_path") -> str:
        return json.dumps({
            "session_id": session,
            "cwd": str(self.root),
            "tool_input": {field: str(path)},
        })

    def install_rules(self) -> None:
        write_files(self.root, {
            "docs/agent-rules/z-last.md": """
                ---
                description: Last alphabetically
                globs: ["gateway-go/**/*.go"]
                ---
            """,
            "docs/agent-rules/a-first.md": """
                ---
                description: First alphabetically
                globs:
                  - gateway-go/internal/**
                ---
            """,
            "docs/agent-rules/README.md": """
                ---
                description: Must be ignored
                globs: ["**"]
                ---
            """,
            "docs/agent-rules/not-a-rule.txt": "ignored",
        })

    def invoke(self, payload: str):
        with mock.patch.object(rules_gate.tempfile, "gettempdir", return_value=str(self.marker_root)):
            return invoke_main(
                rules_gate,
                stdin=payload,
                env={"CLAUDE_PROJECT_DIR": str(self.root)},
            )

    def test_first_matching_edit_blocks_with_sorted_rules_then_retry_passes(self) -> None:
        self.install_rules()
        target = self.root / "gateway-go/internal/domain/item.go"

        rc, stdout, stderr = self.invoke(self.payload(target))
        self.assertEqual(rc, 2)
        self.assertEqual(stdout, "")
        self.assertIn("gateway-go/internal/domain/item.go", stderr)
        self.assertLess(stderr.index("a-first.md"), stderr.index("z-last.md"))
        self.assertIn("First alphabetically", stderr)
        self.assertIn("재시도", stderr)

        rc, stdout, stderr = self.invoke(self.payload(target))
        self.assertEqual((rc, stdout, stderr), (0, "", ""))
        marker_dir = self.marker_root / "claude-rules-gate-session_one"
        self.assertEqual(
            sorted(path.name for path in marker_dir.iterdir()),
            ["a-first.md", "z-last.md"],
        )

    def test_notebook_path_is_supported_and_session_name_is_sanitized(self) -> None:
        write_files(self.root, {
            "docs/agent-rules/notebooks.md": """
                ---
                description: Notebook rule
                globs: ["notebooks/**/*.ipynb"]
                ---
            """,
        })
        target = self.root / "notebooks/research/demo.ipynb"
        rc, _, stderr = self.invoke(self.payload(target, session="bad id:?", field="notebook_path"))
        self.assertEqual(rc, 2)
        self.assertIn("notebooks.md", stderr)
        self.assertTrue((self.marker_root / "claude-rules-gate-bad_id__").is_dir())

    def test_missing_path_outside_repo_or_missing_rules_fail_open(self) -> None:
        empty = json.dumps({"cwd": str(self.root), "tool_input": {}})
        self.assertEqual(self.invoke(empty), (0, "", ""))

        outside = Path(self.tmp.name) / "outside.go"
        self.assertEqual(self.invoke(self.payload(outside)), (0, "", ""))

        inside = self.root / "gateway-go/inside.go"
        self.assertEqual(self.invoke(self.payload(inside)), (0, "", ""))

    def test_unreadable_rule_does_not_hide_other_matching_rules(self) -> None:
        self.install_rules()
        broken = self.root / "docs/agent-rules/a-first.md"
        real_open = open

        def selective_open(path, *args, **kwargs):
            if os.path.realpath(path) == os.path.realpath(broken):
                raise OSError("unreadable")
            return real_open(path, *args, **kwargs)

        with mock.patch("builtins.open", side_effect=selective_open):
            rc, _, stderr = self.invoke(self.payload(self.root / "gateway-go/internal/x.go"))
        self.assertEqual(rc, 2)
        self.assertNotIn("a-first.md", stderr)
        self.assertIn("z-last.md", stderr)

    def test_nonmatching_edit_does_not_create_seen_marker(self) -> None:
        self.install_rules()
        rc, _, stderr = self.invoke(self.payload(self.root / "docs/readme.txt"))
        self.assertEqual((rc, stderr), (0, ""))
        self.assertEqual(list(self.marker_root.iterdir()), [self.marker_root / "claude-rules-gate-session_one"])
        self.assertEqual(list((self.marker_root / "claude-rules-gate-session_one").iterdir()), [])


class EntryPointFailOpenTests(unittest.TestCase):
    def test_malformed_hook_json_exits_zero_without_traceback(self) -> None:
        script = Path(rules_gate.__file__)
        proc = subprocess.run(
            [sys.executable, str(script)],
            input="{not-json",
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(proc.returncode, 0)
        self.assertEqual(proc.stdout, "")
        self.assertEqual(proc.stderr, "")


if __name__ == "__main__":
    unittest.main()
