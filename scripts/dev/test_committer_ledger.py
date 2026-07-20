"""Commit↔session provenance ledger tests for scripts/committer.

The ledger contract: a successful commit appends one JSONL line
{sha, branch, worktree, session, at} to $DENEB_COMMIT_LEDGER_DIR/
commit-sessions.jsonl; the ledger is best-effort and must never fail a
commit (missing dir, unset session env), and a failed commit records nothing.
"""

from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

from test_shell_support import isolated_env, run_script


class CommitterLedgerTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.home.mkdir()
        self.ledger_dir = self.root / "state"
        self.ledger_dir.mkdir()
        self.repo = self.root / "repo"
        self.repo.mkdir()
        self._git("init", "-q", "-b", "test-branch")
        self._git("config", "user.email", "test@example.com")
        self._git("config", "user.name", "Test")
        (self.repo / "seed.txt").write_text("seed\n", encoding="utf-8")
        self._git("add", "seed.txt")
        self._git("commit", "-q", "-m", "seed")

    def _git(self, *args: str) -> str:
        proc = subprocess.run(
            ["git", *args], cwd=self.repo, capture_output=True, text=True, check=True
        )
        return proc.stdout.strip()

    def _env(self, **values: str) -> dict[str, str]:
        defaults = {"DENEB_COMMIT_LEDGER_DIR": str(self.ledger_dir)}
        defaults.update(values)
        return isolated_env(self.home, **defaults)

    def _ledger_lines(self) -> list[dict]:
        path = self.ledger_dir / "commit-sessions.jsonl"
        if not path.exists():
            return []
        return [
            json.loads(line)
            for line in path.read_text(encoding="utf-8").splitlines()
            if line.strip()
        ]

    def test_successful_commit_appends_provenance_line(self) -> None:
        (self.repo / "a.txt").write_text("hello\n", encoding="utf-8")
        proc = run_script(
            "scripts/committer",
            "test(scope): add a",
            "a.txt",
            env=self._env(CLAUDE_CODE_SESSION_ID="sess-1234"),
            cwd=self.repo,
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        lines = self._ledger_lines()
        self.assertEqual(len(lines), 1, lines)
        line = lines[0]
        self.assertEqual(line["sha"], self._git("rev-parse", "HEAD"))
        self.assertEqual(line["branch"], "test-branch")
        self.assertEqual(line["worktree"], "repo")
        self.assertEqual(line["session"], "sess-1234")
        self.assertGreater(line["at"], 0)

    def test_missing_ledger_dir_and_session_never_fail_the_commit(self) -> None:
        (self.repo / "b.txt").write_text("b\n", encoding="utf-8")
        proc = run_script(
            "scripts/committer",
            "test(scope): add b",
            "b.txt",
            # No session env; ledger dir points at a nonexistent path — the
            # commit must succeed and simply record nothing.
            env=self._env(DENEB_COMMIT_LEDGER_DIR=str(self.root / "absent")),
            cwd=self.repo,
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertFalse((self.root / "absent").exists())

        # Session env absent but ledger dir present: the line is still written
        # (branch identifies ZCode/Cursor lanes) with an empty session.
        (self.repo / "c.txt").write_text("c\n", encoding="utf-8")
        proc = run_script(
            "scripts/committer", "test(scope): add c", "c.txt",
            env=self._env(), cwd=self.repo,
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        lines = self._ledger_lines()
        self.assertEqual(len(lines), 1, lines)
        self.assertEqual(lines[0]["session"], "")

    def test_failed_commit_records_nothing(self) -> None:
        proc = run_script(
            "scripts/committer",
            "test(scope): nothing staged",
            "seed.txt",  # tracked but unchanged — committer exits 1 on empty stage
            env=self._env(CLAUDE_CODE_SESSION_ID="sess-x"),
            cwd=self.repo,
        )
        self.assertEqual(proc.returncode, 1, proc.stdout + proc.stderr)
        self.assertEqual(self._ledger_lines(), [])


if __name__ == "__main__":
    unittest.main()
