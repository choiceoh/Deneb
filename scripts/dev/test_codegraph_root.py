"""Root resolution and explore-reroute contracts for every coding agent."""

from __future__ import annotations

import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from test_support import REPO_ROOT

from codegraph_folder_docs import folder_docs
from codegraph_mcp_proxy import (
    ExploreReroute,
    encode_message,
    explore_symbol,
    extract_explore_symbol,
    is_node_miss,
    read_message,
    single_symbol_query,
)
from codegraph_root import pick_root, write_active_root


class SymbolQueryTests(unittest.TestCase):
    def test_reroutes_specific_identifiers_only(self) -> None:
        self.assertEqual(single_symbol_query("GatewayHub"), "GatewayHub")
        self.assertEqual(single_symbol_query("run_agent"), "run_agent")
        self.assertEqual(single_symbol_query("auth"), "")
        self.assertEqual(single_symbol_query("how does the hub work"), "")
        self.assertEqual(single_symbol_query("miniapp.todo.create"), "")

    def test_extracts_nl_and_dotted_rpc_names(self) -> None:
        self.assertEqual(extract_explore_symbol("how does GatewayHub work"), "GatewayHub")
        self.assertEqual(extract_explore_symbol("miniapp.todo.create"), "miniapp.todo.create")
        self.assertEqual(extract_explore_symbol("GatewayHub GatewayTab"), "")
        self.assertEqual(extract_explore_symbol("how does the hub work"), "")


class ExploreRerouteTests(unittest.TestCase):
    def _explore(self, query: str, req_id: int = 1) -> dict:
        return {
            "jsonrpc": "2.0",
            "id": req_id,
            "method": "tools/call",
            "params": {"name": "codegraph_explore", "arguments": {"query": query}},
        }

    def test_nl_and_area_words_pass_through_with_maxfiles_cap(self) -> None:
        hub = ExploreReroute()
        msg = self._explore("how does gateway hub work")
        outgoing = hub.on_client(msg)
        self.assertEqual(outgoing[0]["params"]["name"], "codegraph_explore")
        self.assertEqual(outgoing[0]["params"]["arguments"]["query"], "how does gateway hub work")
        self.assertEqual(outgoing[0]["params"]["arguments"]["maxFiles"], 6)
        self.assertEqual(explore_symbol(self._explore("auth")), "")
        explicit = self._explore("how does gateway hub work")
        explicit["params"]["arguments"]["maxFiles"] = 12
        self.assertEqual(hub.on_client(explicit)[0]["params"]["arguments"]["maxFiles"], 12)

    def test_nl_with_one_symbol_reroutes(self) -> None:
        hub = ExploreReroute()
        outgoing = hub.on_client(self._explore("how does GatewayHub work"))
        self.assertEqual(outgoing[0]["params"]["name"], "codegraph_node")
        self.assertEqual(outgoing[0]["params"]["arguments"]["symbol"], "GatewayHub")

    def test_symbol_hit_rewrites_to_node_and_restores_client_id(self) -> None:
        hub = ExploreReroute()
        outgoing = hub.on_client(self._explore("GatewayHub"))
        self.assertEqual(len(outgoing), 1)
        self.assertEqual(outgoing[0]["params"]["name"], "codegraph_node")
        self.assertEqual(outgoing[0]["params"]["arguments"]["symbol"], "GatewayHub")
        self.assertNotEqual(outgoing[0]["id"], 1)
        to_client, to_server = hub.on_server(
            {
                "jsonrpc": "2.0",
                "id": outgoing[0]["id"],
                "result": {"content": [{"type": "text", "text": "type GatewayHub struct"}]},
            }
        )
        self.assertEqual(to_server, [])
        self.assertEqual(to_client[0]["id"], 1)
        self.assertIn("단일 심볼", to_client[0]["result"]["content"][0]["text"])
        self.assertIn("GatewayHub", to_client[0]["result"]["content"][0]["text"])

    def test_node_miss_falls_back_to_original_explore(self) -> None:
        hub = ExploreReroute()
        outgoing = hub.on_client(self._explore("GatewayHub"))
        to_client, to_server = hub.on_server(
            {
                "jsonrpc": "2.0",
                "id": outgoing[0]["id"],
                "result": {"content": [{"type": "text", "text": 'Symbol "GatewayHub" not found in the codebase'}]},
            }
        )
        self.assertEqual(to_client, [])
        self.assertEqual(to_server[0]["params"]["name"], "codegraph_explore")
        self.assertEqual(to_server[0]["params"]["arguments"]["query"], "GatewayHub")
        follow_id = to_server[0]["id"]
        done, extra = hub.on_server(
            {"jsonrpc": "2.0", "id": follow_id, "result": {"content": [{"type": "text", "text": "area"}]}}
        )
        self.assertEqual(extra, [])
        self.assertEqual(done[0]["id"], 1)
        self.assertEqual(done[0]["result"]["content"][0]["text"], "area")

    def test_empty_node_result_is_a_miss(self) -> None:
        self.assertTrue(is_node_miss({"result": {"content": [{"type": "text", "text": ""}]}}))
        self.assertTrue(is_node_miss({"error": {"message": "boom"}}))
        self.assertFalse(is_node_miss({"result": {"content": [{"type": "text", "text": "ok"}]}}))

    def test_content_length_roundtrip(self) -> None:
        msg = {"jsonrpc": "2.0", "id": 7, "method": "ping"}
        framed = encode_message(msg)
        parsed = read_message(io.BytesIO(framed))
        self.assertEqual(parsed, msg)


class RootPickTests(unittest.TestCase):
    def test_prod_fallback_uses_dev_and_session_pin_beats_cwd(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            prod = home / "deneb"
            dev = home / "deneb-dev"
            wt = home / ".cursor/worktrees/Deneb/sess1"
            prod.mkdir()
            dev.mkdir()
            wt.mkdir(parents=True)
            self.assertEqual(
                pick_root(str(prod), agent="cursor", env={}, home=home),
                str(dev.resolve()),
            )
            write_active_root("cursor", str(wt), sid="sess1", home=home)
            chosen = pick_root(
                str(dev),
                agent="cursor",
                env={"CURSOR_SESSION_ID": "sess1"},
                home=home,
            )
            self.assertEqual(chosen, str(wt.resolve()))

    def test_linked_worktree_cwd_beats_stale_global_pin(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            here = home / "this-session"
            other = home / ".cursor/worktrees/Deneb/other"
            here.mkdir()
            other.mkdir(parents=True)
            write_active_root("cursor", str(other), home=home)
            with mock.patch("codegraph_root.is_linked_worktree", side_effect=lambda p: Path(p).resolve() == here.resolve()):
                chosen = pick_root(str(here), agent="cursor", env={}, home=home)
            self.assertEqual(chosen, str(here.resolve()))

    def test_zcode_pin_is_not_visible_to_cursor(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            cursor_wt = home / ".cursor/worktrees/Deneb/c1"
            zcode_wt = home / ".zcode/worktrees/Deneb/z1"
            fallback = home / "deneb-dev"
            cursor_wt.mkdir(parents=True)
            zcode_wt.mkdir(parents=True)
            fallback.mkdir()
            write_active_root("zcode", str(zcode_wt), sid="z1", home=home)
            chosen = pick_root(str(fallback), agent="cursor", env={}, home=home)
            self.assertEqual(chosen, str(fallback.resolve()))

    def test_auto_without_agent_env_does_not_steal_pins(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            wt = home / ".cursor/worktrees/Deneb/c1"
            fallback = home / "checkout"
            wt.mkdir(parents=True)
            fallback.mkdir()
            write_active_root("cursor", str(wt), home=home)
            chosen = pick_root(str(fallback), agent="auto", env={}, home=home)
            self.assertEqual(chosen, str(fallback.resolve()))

    def test_codegraph_agent_env_selects_zcode_pin(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            wt = home / ".zcode/worktrees/Deneb/z1"
            fallback = home / "checkout"
            wt.mkdir(parents=True)
            fallback.mkdir()
            write_active_root("zcode", str(wt), home=home)
            chosen = pick_root(
                str(fallback),
                agent="auto",
                env={"CODEGRAPH_AGENT": "zcode"},
                home=home,
            )
            self.assertEqual(chosen, str(wt.resolve()))


class WrapperContractTests(unittest.TestCase):
    def test_every_committed_mcp_config_uses_a_serve_wrapper(self) -> None:
        cursor = json.loads((REPO_ROOT / ".cursor/mcp.json").read_text(encoding="utf-8"))
        generic = json.loads((REPO_ROOT / ".mcp.json").read_text(encoding="utf-8"))
        zcode = json.loads((REPO_ROOT / ".zcode/config.json").read_text(encoding="utf-8"))
        self.assertIn("cursor-codegraph-serve.sh", cursor["mcpServers"]["codegraph"]["args"][0])
        self.assertIn("codegraph-serve.sh", generic["mcpServers"]["codegraph"]["args"][0])
        self.assertIn("codegraph-serve.sh", zcode["mcp"]["servers"]["codegraph"]["args"][0])
        self.assertEqual(generic["mcpServers"]["codegraph"]["command"], "bash")
        self.assertNotEqual(generic["mcpServers"]["codegraph"]["command"], "codegraph")
        vscode = json.loads((REPO_ROOT / ".vscode/mcp.json").read_text(encoding="utf-8"))
        trae = json.loads((REPO_ROOT / ".trae/mcp.json").read_text(encoding="utf-8"))
        codex = (REPO_ROOT / ".codex/config.toml").read_text(encoding="utf-8")
        self.assertIn("codegraph-serve.sh", vscode["servers"]["codegraph"]["args"][0])
        self.assertIn("codegraph-serve.sh", trae["mcpServers"]["codegraph"]["args"][0])
        self.assertIn("codegraph-serve.sh", codex)
        self.assertNotIn("command = \"codegraph\"", codex)


class FolderDocsTests(unittest.TestCase):
    def test_attaches_nearest_claude_and_skips_traversal(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "gateway-go").mkdir()
            (root / "gateway-go" / "CLAUDE.md").write_text(
                "# Runtime\n\n## Safety\nNever join `..`.\n\n## Other\nIgnore me.\n",
                encoding="utf-8",
            )
            text = "see gateway-go/internal/runtime/server.go:10"
            extra = folder_docs(text, str(root), "safety path")
            self.assertIn("gateway-go/CLAUDE.md", extra)
            self.assertIn("Never join", extra)
            self.assertNotIn("Ignore me", extra)
            self.assertEqual(folder_docs("see `gateway-go/../../etc/evil.go:1`", str(root), ""), "")

    def test_reroute_appends_folder_docs(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "pkg").mkdir()
            (root / "pkg" / "CLAUDE.md").write_text("pkg map\n", encoding="utf-8")
            hub = ExploreReroute(root=str(root))
            outgoing = hub.on_client(
                {
                    "jsonrpc": "2.0",
                    "id": 1,
                    "method": "tools/call",
                    "params": {"name": "codegraph_explore", "arguments": {"query": "GatewayHub"}},
                }
            )
            to_client, _ = hub.on_server(
                {
                    "jsonrpc": "2.0",
                    "id": outgoing[0]["id"],
                    "result": {"content": [{"type": "text", "text": "pkg/hub.go:1 type GatewayHub"}]},
                }
            )
            self.assertIn("pkg/CLAUDE.md", to_client[0]["result"]["content"][0]["text"])


class DoctorTests(unittest.TestCase):
    def test_mcp_ok_on_this_repo_and_detects_clobber(self) -> None:
        from codegraph_doctor import check_mcp, diagnose

        mcp = check_mcp(str(REPO_ROOT))
        self.assertTrue(mcp["ok"], mcp["detail"])
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cursor = root / ".cursor" / "mcp.json"
            cursor.parent.mkdir()
            cursor.write_text(
                json.dumps({"mcpServers": {"codegraph": {"command": "codegraph", "args": ["serve", "--mcp"]}}}),
                encoding="utf-8",
            )
            self.assertFalse(check_mcp(str(root))["ok"])
            ids = {item["id"] for item in diagnose(str(root))}
            self.assertEqual(ids, {"cli", "root", "index", "mcp"})


if __name__ == "__main__":
    unittest.main()
