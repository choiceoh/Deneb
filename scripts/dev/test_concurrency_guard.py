"""Behavior tests for the fail-open concurrency guard (deny/ask decisions)."""

from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path

from test_support import REPO_ROOT, invoke_main, load_script

guard = load_script("scripts/dev/deneb-concurrency-guard.py")


def payload(tool, tool_input, cwd, session_id="sess-self"):
    return json.dumps({
        "tool_name": tool,
        "tool_input": tool_input,
        "cwd": cwd,
        "session_id": session_id,
    })


class GuardTestCase(unittest.TestCase):
    def setUp(self) -> None:
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.home = Path(tmp.name)
        (self.home / "deneb/.claude/worktrees/wt1").mkdir(parents=True)
        (self.home / "deneb-dev").mkdir()
        (self.home / ".claude").mkdir()
        self.env = {"HOME": str(self.home)}

    def run_guard(self, stdin):
        rc, out, err = invoke_main(guard, stdin=stdin, env=self.env)
        self.assertEqual(rc, 0, err)
        return json.loads(out)["hookSpecificOutput"] if out.strip() else None

    def decision(self, stdin):
        result = self.run_guard(stdin)
        return result["permissionDecision"] if result else "allow"


class ProdEditTests(GuardTestCase):
    def test_prod_tree_edit_and_write_are_denied(self) -> None:
        prod_file = str(self.home / "deneb/gateway-go/main.go")
        for tool in ("Edit", "Write", "MultiEdit"):
            with self.subTest(tool=tool):
                stdin = payload(tool, {"file_path": prod_file}, str(self.home))
                self.assertEqual(self.decision(stdin), "deny")

    def test_prod_worktrees_and_dev_tree_pass(self) -> None:
        allowed = [
            str(self.home / "deneb/.claude/worktrees/wt1/gateway-go/main.go"),
            str(self.home / "deneb-dev/gateway-go/main.go"),
            str(self.home / "other-project/main.go"),
        ]
        for file_path in allowed:
            with self.subTest(file_path=file_path):
                stdin = payload("Edit", {"file_path": file_path}, str(self.home))
                self.assertEqual(self.decision(stdin), "allow")

    def test_relative_path_resolves_against_cwd(self) -> None:
        stdin = payload("Edit", {"file_path": "gateway-go/main.go"},
                        str(self.home / "deneb"))
        self.assertEqual(self.decision(stdin), "deny")


class GitScopingTests(GuardTestCase):
    def bash(self, command, cwd=None):
        return payload("Bash", {"command": command},
                       cwd or str(self.home / "deneb-dev"))

    def test_repo_wide_staging_asks(self) -> None:
        for command in ("git add -A", "git add .", "git add --all",
                        "git commit -am 'fix'", "git commit -a -m 'fix'",
                        "cd sub && git add -A",
                        "git -C ~/deneb-dev add --all"):
            with self.subTest(command=command):
                self.assertEqual(self.decision(self.bash(command)), "ask")

    def test_scoped_git_and_lookalikes_pass(self) -> None:
        for command in ("git add gateway-go/main.go",
                        "git commit -m 'revert the -a behavior'",
                        "git commit --amend --no-edit",
                        "git status", "ls -a"):
            with self.subTest(command=command):
                self.assertEqual(self.decision(self.bash(command)), "allow")

    def test_non_deneb_context_is_ignored(self) -> None:
        stdin = self.bash("git add -A", cwd=str(self.home / "other-project"))
        self.assertEqual(self.decision(stdin), "allow")


class ProdWriteBashTests(GuardTestCase):
    def bash(self, command):
        return payload("Bash", {"command": command}, str(self.home / "deneb-dev"))

    def prod(self, rel):
        return str(self.home / "deneb" / rel)

    def test_writes_into_prod_ask(self) -> None:
        commands = [
            f"cp local.py {self.prod('scripts/dev/x.py')}",  # the 2026-07-18 incident shape
            f"cp a.py b.py {self.prod('scripts/dev/')}",
            f"rm {self.prod('scripts/dev/x.py')}",
            f"mv {self.prod('scripts/dev/x.py')} /tmp/x.py",
            f"echo hi > {self.prod('notes.txt')}",
            f"echo hi >{self.prod('notes.txt')}",
            f"echo hi 2> {self.prod('err.log')}",
            f"cat local | tee {self.prod('notes.txt')}",
            f"sed -i s/a/b/ {self.prod('scripts/dev/x.py')}",
            f"touch {self.prod('marker')}",
            f"make build && cp dist/out {self.prod('dist/out')}",
        ]
        for command in commands:
            with self.subTest(command=command):
                self.assertEqual(self.decision(self.bash(command)), "ask")

    def test_reads_and_safe_destinations_pass(self) -> None:
        home = self.home
        commands = [
            f"cat {self.prod('scripts/dev/x.py')}",
            f"grep -rn foo {self.prod('scripts')}",
            f"cp {self.prod('scripts/dev/x.py')} /tmp/x.py",  # prod as SOURCE only
            f"cp {self.prod('a')} {home / 'deneb-dev/a'}",
            f"cp x {home / 'deneb/.claude/worktrees/wt1/x'}",  # worktree dest
            f"cp x {home / 'deneb-dev/scripts/x'}",
            f"git -C {home / 'deneb'} log --oneline",
            f"sed s/a/b/ {self.prod('f')}",  # sed without -i reads only
            f"ls > /tmp/deneb-listing.txt && cat {self.prod('f')}",
        ]
        for command in commands:
            with self.subTest(command=command):
                self.assertEqual(self.decision(self.bash(command)), "allow")


class LivetestLockTests(GuardTestCase):
    COMMAND = "scripts/dev/live-test.sh restart && scripts/dev/live-test.sh smoke"

    def lock_path(self) -> Path:
        return self.home / ".claude/deneb-livetest.lock"

    def write_lock(self, session_id, age_seconds) -> None:
        self.lock_path().write_text(json.dumps(
            {"session_id": session_id, "ts": time.time() - age_seconds}))

    def bash(self, session_id="sess-self"):
        return payload("Bash", {"command": self.COMMAND},
                       str(self.home / "deneb-dev"), session_id=session_id)

    def test_foreign_fresh_lock_asks_and_is_not_stolen(self) -> None:
        self.write_lock("sess-other", age_seconds=60)
        self.assertEqual(self.decision(self.bash()), "ask")
        held = json.loads(self.lock_path().read_text())
        self.assertEqual(held["session_id"], "sess-other")

    def test_own_stale_or_missing_lock_passes_and_takes_lock(self) -> None:
        cases = [("own", lambda: self.write_lock("sess-self", 60)),
                 ("stale", lambda: self.write_lock("sess-other", 700)),
                 ("missing", lambda: None)]
        for label, arrange in cases:
            with self.subTest(label=label):
                arrange()
                self.assertEqual(self.decision(self.bash()), "allow")
                held = json.loads(self.lock_path().read_text())
                self.assertEqual(held["session_id"], "sess-self")


class FailOpenTests(unittest.TestCase):
    def test_malformed_stdin_exits_zero(self) -> None:
        script = REPO_ROOT / "scripts/dev/deneb-concurrency-guard.py"
        done = subprocess.run([sys.executable, str(script)], input="not json",
                              capture_output=True, text=True, timeout=10)
        self.assertEqual(done.returncode, 0)
        self.assertEqual(done.stdout.strip(), "")


class DecisionMessageTests(GuardTestCase):
    """The 2026-07-19 clarity fix: a guard block must be distinguishable from a
    user cancel, in both the JSON reason and stderr."""

    def test_deny_reason_marks_it_as_guard_not_user_cancel(self) -> None:
        prod_file = str(self.home / "deneb/gateway-go/main.go")
        stdin = payload("Edit", {"file_path": prod_file}, str(self.home))
        out = self.run_guard(stdin)
        self.assertEqual(out["permissionDecision"], "deny")
        self.assertIn("사용자 취소 아님", out["permissionDecisionReason"])
        self.assertIn("차단", out["permissionDecisionReason"])

    def test_reason_is_mirrored_to_stderr(self) -> None:
        prod_file = str(self.home / "deneb/gateway-go/main.go")
        stdin = payload("Edit", {"file_path": prod_file}, str(self.home))
        _, _, err = invoke_main(guard, stdin=stdin, env=self.env)
        self.assertIn("deneb-concurrency-guard", err)
        self.assertIn("사용자 취소 아님", err)


if __name__ == "__main__":
    unittest.main()
