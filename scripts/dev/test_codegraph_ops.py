"""Behavior tests for CodeGraph daemon eviction and MCP wrapper restore."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from test_support import REPO_ROOT

from codegraph_daemon import evict_stale_daemon
from codegraph_mcp_restore import (
    REPO_CURSOR_MCP,
    is_upgrade_clobber,
    restore_repo,
    restore_user,
)


UPGRADE_CFG = {
    "mcpServers": {
        "codegraph": {
            "type": "stdio",
            "command": "codegraph",
            "args": ["serve", "--mcp", "--path", "/tmp/worktree"],
        }
    }
}


class UpgradeFingerprintTests(unittest.TestCase):
    def test_detects_bare_serve_mcp_and_ignores_wrappers(self) -> None:
        self.assertTrue(is_upgrade_clobber(UPGRADE_CFG))
        self.assertFalse(is_upgrade_clobber(REPO_CURSOR_MCP))
        self.assertFalse(is_upgrade_clobber({"mcpServers": {"plaud": {"command": "npx"}}}))


class RestoreRepoTests(unittest.TestCase):
    def test_rewrites_clobbered_files_and_leaves_wrappers_alone(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cursor = root / ".cursor" / "mcp.json"
            repo = root / ".mcp.json"
            cursor.parent.mkdir()
            cursor.write_text(json.dumps(UPGRADE_CFG), encoding="utf-8")
            repo.write_text(json.dumps(UPGRADE_CFG), encoding="utf-8")
            changed = restore_repo(str(root))
            self.assertEqual(set(changed), {str(cursor), str(repo)})
            self.assertEqual(json.loads(cursor.read_text(encoding="utf-8"))["mcpServers"]["codegraph"]["command"], "bash")
            self.assertIn("cursor-codegraph-serve.sh", json.loads(cursor.read_text(encoding="utf-8"))["mcpServers"]["codegraph"]["args"][0])
            self.assertEqual(restore_repo(str(root)), [])

    def test_rewrites_zcode_server_without_dropping_hooks(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            zcode = root / ".zcode" / "config.json"
            zcode.parent.mkdir()
            zcode.write_text(
                json.dumps(
                    {
                        "mcp": {
                            "servers": {
                                "codegraph": {
                                    "command": "codegraph",
                                    "args": ["serve", "--mcp"],
                                }
                            }
                        },
                        "hooks": {"enabled": True},
                    }
                ),
                encoding="utf-8",
            )
            changed = restore_repo(str(root))
            self.assertEqual(changed, [str(zcode)])
            restored = json.loads(zcode.read_text(encoding="utf-8"))
            self.assertEqual(restored["hooks"], {"enabled": True})
            self.assertIn("codegraph-serve.sh", restored["mcp"]["servers"]["codegraph"]["args"][0])
            self.assertEqual(restored["mcp"]["servers"]["codegraph"]["env"]["CODEGRAPH_AGENT"], "zcode")

    def test_user_restore_installs_wrapper_only_on_clobber(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            user_mcp = home / ".cursor" / "mcp.json"
            user_mcp.parent.mkdir(parents=True)
            user_mcp.write_text(json.dumps(UPGRADE_CFG), encoding="utf-8")
            changed = restore_user(str(REPO_ROOT), home=home)
            wrapper = home / ".local" / "bin" / "deneb-cursor-codegraph-serve"
            self.assertIn(str(user_mcp), changed)
            self.assertTrue(wrapper.is_file())
            restored = json.loads(user_mcp.read_text(encoding="utf-8"))
            self.assertEqual(restored["mcpServers"]["codegraph"]["args"], [str(wrapper)])
            self.assertEqual(restore_user(str(REPO_ROOT), home=home), [])


class DaemonEvictTests(unittest.TestCase):
    def test_absent_and_matching_version_are_noops(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.assertEqual(evict_stale_daemon(str(root), expected="1.5.0"), "absent")
            pidfile = root / ".codegraph" / "daemon.pid"
            pidfile.parent.mkdir()
            pidfile.write_text(json.dumps({"pid": 9, "version": "1.5.0"}), encoding="utf-8")
            self.assertEqual(evict_stale_daemon(str(root), expected="1.5.0"), "fresh")

    def test_mismatch_kills_codegraph_and_removes_runtime_files(self) -> None:
        killed: list[int] = []
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cg = root / ".codegraph"
            cg.mkdir()
            (cg / "daemon.pid").write_text(json.dumps({"pid": 4242, "version": "1.4.1"}), encoding="utf-8")
            (cg / "daemon.sock").write_text("", encoding="utf-8")
            status = evict_stale_daemon(
                str(root),
                expected="1.5.0",
                kill=lambda pid, _sig: killed.append(pid),
                pid_command=lambda pid: f"codegraph serve --mcp --path {root}" if pid == 4242 else "",
            )
            self.assertEqual(status, "evicted")
            self.assertEqual(killed, [4242])
            self.assertFalse((cg / "daemon.pid").exists())
            self.assertFalse((cg / "daemon.sock").exists())

    def test_refuses_to_kill_unrelated_pid(self) -> None:
        killed: list[int] = []
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            pidfile = root / ".codegraph" / "daemon.pid"
            pidfile.parent.mkdir()
            pidfile.write_text(json.dumps({"pid": 7, "version": "1.4.1"}), encoding="utf-8")
            status = evict_stale_daemon(
                str(root),
                expected="1.5.0",
                kill=lambda pid, _sig: killed.append(pid),
                pid_command=lambda _pid: "/usr/bin/sleep 90",
            )
            self.assertEqual(status, "skipped")
            self.assertEqual(killed, [])
            self.assertTrue(pidfile.exists())


class SyncRunContractTests(unittest.TestCase):
    def test_cursor_and_zcode_hooks_call_shared_runner(self) -> None:
        cursor = (REPO_ROOT / "scripts/dev/cursor-codegraph-sync.sh").read_text(encoding="utf-8")
        zcode = (REPO_ROOT / "scripts/dev/zcode-codegraph-sync.sh").read_text(encoding="utf-8")
        runner = (REPO_ROOT / "scripts/dev/codegraph-sync-run.sh").read_text(encoding="utf-8")
        self.assertIn("codegraph-sync-run.sh", cursor)
        self.assertIn("codegraph-sync-run.sh", zcode)
        self.assertIn("rpcmap_codegraph_sync.py", runner)
        self.assertNotIn("rpcmap_codegraph_sync.py", cursor)
        self.assertIn("codegraph-seed-index.sh", (REPO_ROOT / "scripts/dev/cursor-worktree-init.sh").read_text(encoding="utf-8"))
        self.assertIn("codegraph-seed-index.sh", (REPO_ROOT / "scripts/dev/zcode-worktree-init.sh").read_text(encoding="utf-8"))
        self.assertIn("codegraph_root.py", (REPO_ROOT / "scripts/dev/zcode-worktree-init.sh").read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
