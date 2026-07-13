"""Tests for the Codex-backed RSI L4 executor."""

from __future__ import annotations

import stat
import tempfile
import time
import unittest
from pathlib import Path

from coding_dispatch_executor import (
    TIMEOUT_EXIT,
    build_command,
    preflight,
    resolve_codex,
    run_codex,
)


def fake_executable(path: Path, body: str) -> Path:
    path.write_text("#!/bin/sh\n" + body, encoding="utf-8")
    path.chmod(path.stat().st_mode | stat.S_IXUSR)
    return path


class CodingDispatchExecutorTest(unittest.TestCase):
    def test_explicit_binary_must_be_executable(self):
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "codex"
            path.write_text("", encoding="utf-8")
            self.assertIsNone(resolve_codex(str(path)))
            path.chmod(path.stat().st_mode | stat.S_IXUSR)
            self.assertEqual(resolve_codex(str(path)), str(path))

    def test_command_limits_writes_and_reads_prompt_from_stdin(self):
        cmd = build_command("/bin/codex", Path("/worktree"), Path("/prod"))
        self.assertEqual(cmd[:4], ["/bin/codex", "exec", "-C", "/worktree"])
        self.assertIn("workspace-write", cmd)
        self.assertIn("/prod/.git", cmd)
        self.assertIn("sandbox_workspace_write.network_access=true", cmd)
        self.assertEqual(cmd[-1], "-")

    def test_preflight_uses_login_status(self):
        with tempfile.TemporaryDirectory() as td:
            good = fake_executable(Path(td) / "good", '[ "$1 $2" = "login status" ]\n')
            bad = fake_executable(Path(td) / "bad", "exit 7\n")
            self.assertTrue(preflight(str(good)))
            self.assertFalse(preflight(str(bad)))

    def test_prompt_is_stdin_not_an_argument(self):
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            args_path = root / "args"
            stdin_path = root / "stdin"
            fake = fake_executable(
                root / "codex",
                f'printf "%s\\n" "$@" > "{args_path}"\ncat > "{stdin_path}"\n',
            )
            rc = run_codex(str(fake), root / "wt", root / "prod", "secret prompt", 5)
            self.assertEqual(rc, 0)
            self.assertEqual(stdin_path.read_text(encoding="utf-8"), "secret prompt")
            self.assertNotIn("secret prompt", args_path.read_text(encoding="utf-8"))

    def test_timeout_terminates_the_executor_group(self):
        with tempfile.TemporaryDirectory() as td:
            fake = fake_executable(Path(td) / "codex", "sleep 30\n")
            started = time.monotonic()
            rc = run_codex(str(fake), Path(td) / "wt", Path(td) / "prod", "go", 0.05)
            self.assertEqual(rc, TIMEOUT_EXIT)
            self.assertLess(time.monotonic() - started, 2)


if __name__ == "__main__":
    unittest.main()
