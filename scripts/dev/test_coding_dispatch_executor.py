"""Tests for the Codex-backed RSI L4 executor."""

from __future__ import annotations

import json
import os
import stat
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

from coding_dispatch_executor import (
    DEFAULT_SESSION_RETENTION_DAYS,
    SESSION_ARCHIVE_SUBDIR,
    TIMEOUT_EXIT,
    archive_session_rollouts,
    build_command,
    preflight,
    resolve_codex,
    rollout_session_cwd,
    run_codex,
    session_retention_days,
)


def fake_executable(path: Path, body: str) -> Path:
    path.write_text("#!/bin/sh\n" + body, encoding="utf-8")
    path.chmod(path.stat().st_mode | stat.S_IXUSR)
    return path


class CodingDispatchExecutorTest(unittest.TestCase):
    def test_when_explicit_binary_must_be_executable(self):
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / "codex"
            path.write_text("", encoding="utf-8")
            self.assertIsNone(resolve_codex(str(path)))
            path.chmod(path.stat().st_mode | stat.S_IXUSR)
            self.assertEqual(resolve_codex(str(path)), str(path))

    def test_standard_user_install_works_with_minimal_service_path(self):
        with tempfile.TemporaryDirectory() as td:
            home = Path(td)
            codex = home / ".local" / "bin" / "codex"
            codex.parent.mkdir(parents=True)
            fake_executable(codex, "exit 0\n")
            with (
                mock.patch("coding_dispatch_executor.shutil.which", return_value=None),
                mock.patch("coding_dispatch_executor.Path.home", return_value=home),
            ):
                self.assertEqual(resolve_codex(), str(codex))

    def test_command_limits_writes_and_reads_prompt_from_stdin(self):
        cmd = build_command("/bin/codex", Path("/worktree"), Path("/prod"))
        self.assertEqual(cmd[:4], ["/bin/codex", "exec", "-C", "/worktree"])
        self.assertIn("workspace-write", cmd)
        self.assertIn("/prod/.git", cmd)
        self.assertIn("sandbox_workspace_write.network_access=true", cmd)
        self.assertEqual(cmd[-1], "-")

    def test_command_persists_sessions_for_behavior_mining(self):
        # --ephemeral would erase the rollout the cross-harness miner needs.
        cmd = build_command("/bin/codex", Path("/worktree"), Path("/prod"))
        self.assertNotIn("--ephemeral", cmd)

    def test_when_preflight_uses_login_status(self):
        with tempfile.TemporaryDirectory() as td:
            good = fake_executable(Path(td) / "good", '[ "$1 $2" = "login status" ]\n')
            bad = fake_executable(Path(td) / "bad", "exit 7\n")
            self.assertTrue(preflight(str(good)))
            self.assertFalse(preflight(str(bad)))

    def test_when_prompt_is_stdin_not_an_argument(self):
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


def write_rollout(path: Path, first_line: str) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(first_line + "\n" + '{"type": "event_msg"}\n', encoding="utf-8")
    return path


def meta_line(cwd: str, top_level: bool = False) -> str:
    if top_level:
        return json.dumps({"type": "session_meta", "cwd": cwd})
    return json.dumps({"type": "session_meta", "payload": {"cwd": cwd}})


class SessionRolloutArchiveTest(unittest.TestCase):
    def setUp(self):
        self._td = tempfile.TemporaryDirectory()
        self.addCleanup(self._td.cleanup)
        root = Path(self._td.name)
        self.codex_home = root / ".codex"
        self.dispatch_root = root / "agent-worktrees"
        self.archive = root / ".deneb" / SESSION_ARCHIVE_SUBDIR
        (self.dispatch_root / "dispatch-a1").mkdir(parents=True)

    def test_archive_layout_preserves_codex_vendor_shape(self):
        # numbat classifies artifacts by vendor directory layout; an archive
        # without a .codex/sessions path component parses to zero sessions.
        self.assertEqual(
            Path(*SESSION_ARCHIVE_SUBDIR.parts[-2:]), Path(".codex") / "sessions"
        )

    def day_dir(self) -> Path:
        return self.codex_home / "sessions" / "2026" / "08" / "28"

    def test_archives_only_rollouts_whose_cwd_is_under_dispatch_root(self):
        dispatch = write_rollout(
            self.day_dir() / "rollout-2026-08-28T01-00-00-aaa.jsonl",
            meta_line(str(self.dispatch_root / "dispatch-a1")),
        )
        operator = write_rollout(
            self.day_dir() / "rollout-2026-08-28T02-00-00-bbb.jsonl",
            meta_line("/home/operator/deneb"),
        )
        unreadable = write_rollout(
            self.day_dir() / "rollout-2026-08-28T03-00-00-ccc.jsonl", "not json"
        )
        archived, pruned = archive_session_rollouts(
            self.codex_home, self.dispatch_root, self.archive
        )
        self.assertEqual((archived, pruned), (1, 0))
        self.assertFalse(dispatch.exists())
        self.assertTrue((self.archive / dispatch.name).exists())
        self.assertTrue(operator.exists())
        self.assertTrue(unreadable.exists())

    def test_pre_0144_top_level_cwd_schema_still_matches(self):
        legacy = write_rollout(
            self.day_dir() / "rollout-2026-07-01T00-00-00-ddd.jsonl",
            meta_line(str(self.dispatch_root / "dispatch-old"), top_level=True),
        )
        archived, _ = archive_session_rollouts(
            self.codex_home, self.dispatch_root, self.archive
        )
        self.assertEqual(archived, 1)
        self.assertTrue((self.archive / legacy.name).exists())

    def test_retention_prunes_old_archives_and_zero_disables(self):
        self.archive.mkdir(parents=True)
        now = time.time()
        old = self.archive / "rollout-2026-01-01T00-00-00-old.jsonl"
        fresh = self.archive / "rollout-2026-08-28T00-00-00-new.jsonl"
        old.write_text("{}", encoding="utf-8")
        fresh.write_text("{}", encoding="utf-8")
        os.utime(old, (now - 91 * 86400, now - 91 * 86400))
        archived, pruned = archive_session_rollouts(
            self.codex_home, self.dispatch_root, self.archive,
            retention_days=90, now_s=now,
        )
        self.assertEqual((archived, pruned), (0, 1))
        self.assertFalse(old.exists())
        self.assertTrue(fresh.exists())
        old.write_text("{}", encoding="utf-8")
        os.utime(old, (now - 400 * 86400, now - 400 * 86400))
        _, pruned = archive_session_rollouts(
            self.codex_home, self.dispatch_root, self.archive,
            retention_days=0, now_s=now,
        )
        self.assertEqual(pruned, 0)
        self.assertTrue(old.exists())

    def test_missing_codex_home_is_a_noop(self):
        archived, pruned = archive_session_rollouts(
            self.codex_home / "nope", self.dispatch_root, self.archive
        )
        self.assertEqual((archived, pruned), (0, 0))
        self.assertFalse(self.archive.exists())

    def test_rollout_session_cwd_reads_both_schemas(self):
        modern = write_rollout(
            self.day_dir() / "rollout-2026-08-28T04-00-00-eee.jsonl", meta_line("/x")
        )
        self.assertEqual(rollout_session_cwd(modern), "/x")
        legacy = write_rollout(
            self.day_dir() / "rollout-2026-08-28T05-00-00-fff.jsonl",
            meta_line("/y", top_level=True),
        )
        self.assertEqual(rollout_session_cwd(legacy), "/y")
        self.assertIsNone(rollout_session_cwd(self.day_dir() / "missing.jsonl"))

    def test_retention_env_override_with_garbage_fallback(self):
        with mock.patch.dict(
            os.environ, {"DENEB_DISPATCH_SESSION_RETENTION_DAYS": "30"}
        ):
            self.assertEqual(session_retention_days(), 30)
        with mock.patch.dict(
            os.environ, {"DENEB_DISPATCH_SESSION_RETENTION_DAYS": "junk"}
        ):
            self.assertEqual(session_retention_days(), DEFAULT_SESSION_RETENTION_DAYS)


if __name__ == "__main__":
    unittest.main()
