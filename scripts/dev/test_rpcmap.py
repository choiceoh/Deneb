"""Static extraction and deterministic CLI tests for rpcmap."""

from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from test_support import invoke_main, load_script, write_files

rpcmap = load_script("scripts/dev/rpcmap.py")


class MapExtractionTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        write_files(self.root, {
            "gateway-go/internal/runtime/rpc/methods.go": """
                package rpc

                var methods = map[string]any{
                    "miniapp.z.last": zHandler,
                    "miniapp.a.first": aHandler,
                }

                func add(m map[string]any) {
                    m["miniapp.assigned"] = assignedHandler
                }
            """,
            "gateway-go/internal/runtime/rpc/ignored_test.go": """
                package rpc
                var ignored = map[string]any{"miniapp.test.only": testHandler}
            """,
            "gateway-go/internal/runtime/rpc/readme.txt": """
                "miniapp.not.go": textHandler,
            """,
            "gateway-go/internal/pipeline/chat/tools.go": """
                package chat

                var wiki = ToolDef{
                    Name: "wiki",
                    Description: "search the wiki",
                    Fn: tools.ToolWiki(
                }

                var mail = ToolDef{ Name: "mail", Fn: MailTool( }
                var badName = ToolDef{ Name: "bad" }
                var badFn = ToolDef{ Fn: MustNotCrossStruct( }
            """,
            "gateway-go/internal/pipeline/chat/tools_test.go": """
                package chat
                var ignored = ToolDef{ Name: "test-only", Fn: TestTool( }
            """,
            "gateway-go/internal/pipeline/chat/broadcast.go": """
                package chat
                func emit() {
                    broadcast("chat.delivery_failed", ChatDeliveryFailedEvent{})
                    broadcast("sessions.changed", SessionsChangedEvent{})
                }
            """,
            "gateway-go/internal/runtime/health.go": """
                package runtime
                func notify() {
                    broadcast("model.role_health", map[string]any{})
                }
            """,
        })

    def test_when_excludes_test_and_non_go_files_from_discovery(self) -> None:
        found = [Path(path).relative_to(self.root).as_posix() for path in rpcmap._go_files(
            str(self.root), rpcmap.RPC_DIRS + rpcmap.TOOL_DIRS + rpcmap.EVENT_DIRS
        )]
        self.assertEqual(
            sorted(found),
            [
                "gateway-go/internal/pipeline/chat/broadcast.go",
                "gateway-go/internal/pipeline/chat/tools.go",
                "gateway-go/internal/runtime/health.go",
                "gateway-go/internal/runtime/rpc/methods.go",
            ],
        )

    def test_returns_rpc_tools_and_events_from_multiline_forms(self) -> None:
        result = rpcmap.build_map(str(self.root))
        self.assertEqual(
            [(name, handler) for name, handler, _path, _line in result["rpc"]],
            [
                ("miniapp.a.first", "aHandler"),
                ("miniapp.assigned", "assignedHandler"),
                ("miniapp.z.last", "zHandler"),
            ],
        )
        self.assertEqual(
            [(name, handler) for name, handler, _path, _line in result["tool"]],
            [("mail", "MailTool"), ("wiki", "ToolWiki")],
        )
        self.assertEqual(
            sorted((name, handler) for name, handler, _p, _l in result["event"]),
            [
                ("chat.delivery_failed", "ChatDeliveryFailedEvent"),
                ("model.role_health", "map"),
                ("sessions.changed", "SessionsChangedEvent"),
            ],
        )
        self.assertTrue(all(not path.startswith("/") for rows in result.values() for _, _, path, _ in rows))

    def test_rejects_tool_matches_across_closing_struct_brace(self) -> None:
        result = rpcmap.build_map(str(self.root))
        names = {name for name, _handler, _path, _line in result["tool"]}
        self.assertNotIn("bad", names)
        self.assertNotIn("test-only", names)

    def test_result_is_stably_sorted_and_deduplicated_even_if_file_walk_repeats(self) -> None:
        rpc_file = self.root / "gateway-go/internal/runtime/rpc/methods.go"
        tool_file = self.root / "gateway-go/internal/pipeline/chat/tools.go"
        event_file = self.root / "gateway-go/internal/pipeline/chat/broadcast.go"
        with mock.patch.object(
            rpcmap,
            "_go_files",
            side_effect=[
                [str(rpc_file), str(rpc_file)],
                [str(tool_file), str(tool_file)],
                [str(event_file), str(event_file)],
            ],
        ):
            result = rpcmap.build_map(str(self.root))
        self.assertEqual(len(result["rpc"]), 3)
        self.assertEqual(len(result["tool"]), 2)
        self.assertEqual(result["rpc"], sorted(result["rpc"]))
        self.assertEqual(result["tool"], sorted(result["tool"]))
        self.assertEqual(result["event"], sorted(result["event"]))

    def test_formatter_preserves_handler_file_line_and_followup_command(self) -> None:
        formatted = rpcmap._fmt(
            "rpc",
            [("miniapp.people.list", "peopleList", "internal/people.go", 42)],
        )
        self.assertEqual(
            formatted,
            "  [rpc] miniapp.people.list\n"
            "        → peopleList   (internal/people.go:42)\n"
            "          codegraph node peopleList",
        )


class CLIContractTests(unittest.TestCase):
    MAP = {
        "rpc": [
            ("miniapp.mail.list", "gmailList", "runtime/gmail.go", 10),
            ("miniapp.mail.list.detail", "gmailDetail", "runtime/gmail.go", 20),
            ("miniapp.people.list", "peopleList", "runtime/people.go", 30),
        ],
        "tool": [
            ("mail", "mailTool", "pipeline/mail.go", 5),
            ("wiki", "wikiTool", "pipeline/wiki.go", 7),
        ],
        "event": [
            ("chat.delivery_failed", "ChatDeliveryFailedEvent", "pipeline/chat.go", 15),
        ],
    }

    def invoke(self, args):
        with mock.patch.object(rpcmap, "build_map", return_value=self.MAP):
            return invoke_main(rpcmap, args)

    def test_no_selector_returns_usage_count_and_exit_two(self) -> None:
        rc, stdout, stderr = self.invoke([])
        self.assertEqual(rc, 2)
        self.assertEqual(stdout, "")
        self.assertIn("3 RPC methods, 2 tools, 1 events indexed", stderr)
        self.assertIn("Give a name", stderr)

    def test_returns_exact_name_match_over_substring_hits(self) -> None:
        rc, stdout, stderr = self.invoke(["miniapp.mail.list"])
        self.assertEqual((rc, stderr), (0, ""))
        self.assertIn("→ gmailList", stdout)
        self.assertNotIn("gmailDetail", stdout)

    def test_handler_reverse_lookup_is_exact_and_can_return_multiple_kinds(self) -> None:
        mapping = {
            "rpc": [("miniapp.mail.send", "shared", "rpc.go", 1)],
            "tool": [("mail-send", "shared", "tool.go", 2)],
            "event": [],
        }
        with mock.patch.object(rpcmap, "build_map", return_value=mapping):
            rc, stdout, stderr = invoke_main(rpcmap, ["--handler", "shared"])
        self.assertEqual((rc, stderr), (0, ""))
        self.assertIn("[rpc] miniapp.mail.send", stdout)
        self.assertIn("[tool] mail-send", stdout)

    def test_returns_deterministic_unicode_safe_json_list(self) -> None:
        rc, stdout, stderr = self.invoke(["--list", "--json"])
        self.assertEqual((rc, stderr), (0, ""))
        payload = json.loads(stdout)
        self.assertEqual([row["kind"] for row in payload], ["rpc", "rpc", "rpc", "tool", "tool", "event"])
        self.assertEqual(payload[0], {
            "kind": "rpc",
            "name": "miniapp.mail.list",
            "handler": "gmailList",
            "file": "runtime/gmail.go",
            "line": 10,
        })

    def test_json_miss_returns_empty_array_and_exit_one(self) -> None:
        rc, stdout, stderr = self.invoke(["missing", "--json"])
        self.assertEqual((rc, stderr), (1, ""))
        self.assertEqual(json.loads(stdout), [])

    def test_explains_static_runtime_limit_when_text_lookup_misses(self) -> None:
        rc, stdout, stderr = self.invoke(["observatory.dynamic"])
        self.assertEqual((rc, stdout), (1, ""))
        self.assertIn("no match. (3 RPC methods, 2 tools, 1 events indexed)", stderr)
        self.assertIn("런타임 합성", stderr)

    def test_real_help_entrypoint_preserves_cli_options(self) -> None:
        proc = subprocess.run(
            [sys.executable, rpcmap.__file__, "--help"],
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(proc.returncode, 0)
        for option in ("--handler", "--list", "--json", "--root"):
            self.assertIn(option, proc.stdout)


if __name__ == "__main__":
    unittest.main()
