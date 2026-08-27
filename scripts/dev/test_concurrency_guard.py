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


class FileClaimTests(GuardTestCase):
    """Check 6: two agents editing the same repo file from different worktrees.

    Worktree isolation prevents corruption, not collision — the two branches
    meet at merge time. Four coding harnesses run against this repo
    concurrently, so this is routine, not hypothetical.
    """

    def setUp(self) -> None:
        super().setUp()
        # Two worktrees, each a git checkout, holding the SAME repo-relative file.
        self.wt_a = self.home / "deneb/.claude/worktrees/wt1"
        self.wt_b = self.home / "deneb/.claude/worktrees/wt2"
        for wt in (self.wt_a, self.wt_b):
            wt.mkdir(parents=True, exist_ok=True)
            (wt / ".git").write_text("gitdir: /elsewhere\n", encoding="utf-8")
            (wt / "gateway-go/internal").mkdir(parents=True, exist_ok=True)
            (wt / "gateway-go/internal/foo.go").write_text("package foo\n", encoding="utf-8")

    def edit(self, worktree, session_id):
        return payload(
            "Edit",
            {"file_path": str(worktree / "gateway-go/internal/foo.go")},
            str(worktree),
            session_id=session_id,
        )

    def test_second_session_editing_the_same_repo_file_asks(self) -> None:
        self.assertEqual(self.decision(self.edit(self.wt_a, "sess-a")), "allow")
        # Different worktree, different branch, SAME repo-relative path.
        result = self.run_guard(self.edit(self.wt_b, "sess-b"))
        self.assertEqual(result["permissionDecision"], "ask")
        reason = result["permissionDecisionReason"]
        self.assertIn("gateway-go/internal/foo.go", reason)
        self.assertIn("sess-a", reason)

    def test_same_session_never_blocks_itself(self) -> None:
        """A session edits its own files repeatedly; self-blocking would make
        the guard unusable."""
        for _ in range(3):
            self.assertEqual(self.decision(self.edit(self.wt_a, "sess-a")), "allow")
        # Even from a second worktree, one session is not competing with itself.
        self.assertEqual(self.decision(self.edit(self.wt_b, "sess-a")), "allow")

    def test_different_files_do_not_collide(self) -> None:
        other = self.wt_b / "gateway-go/internal/bar.go"
        other.write_text("package foo\n", encoding="utf-8")
        self.assertEqual(self.decision(self.edit(self.wt_a, "sess-a")), "allow")
        self.assertEqual(
            self.decision(payload("Edit", {"file_path": str(other)}, str(self.wt_b), "sess-b")),
            "allow",
        )

    def test_expired_claim_stops_warning(self) -> None:
        """A dead session must not hold a file forever."""
        claims = self.home / ".claude/deneb-file-claims.json"
        claims.write_text(json.dumps({
            "gateway-go/internal/foo.go": {
                "session_id": "sess-ghost",
                "ts": time.time() - 999_999,
                "cwd": "/gone",
            }
        }), encoding="utf-8")
        self.assertEqual(self.decision(self.edit(self.wt_b, "sess-b")), "allow")

    def test_files_outside_a_checkout_are_not_claimed(self) -> None:
        loose = self.home / "notes.md"
        loose.write_text("x\n", encoding="utf-8")
        stdin = payload("Edit", {"file_path": str(loose)}, str(self.home), "sess-a")
        self.assertEqual(self.decision(stdin), "allow")
        self.assertFalse((self.home / ".claude/deneb-file-claims.json").exists())

    def test_corrupt_ledger_does_not_block_edits(self) -> None:
        """The guard is fail-open everywhere else; the ledger is no exception."""
        (self.home / ".claude/deneb-file-claims.json").write_text("{not json", encoding="utf-8")
        self.assertEqual(self.decision(self.edit(self.wt_a, "sess-a")), "allow")


class FileClaimShellTests(GuardTestCase):
    """Check 4b: shell writes, which are the MAJORITY of real edits.

    A claim check that watches only Write/Edit/MultiEdit misses `sed -i`,
    redirects, heredocs, and heredoc-fed interpreters — measured on a real
    session, nearly every edit was one of those.
    """

    def setUp(self) -> None:
        super().setUp()
        self.wt_a = self.home / "deneb/.claude/worktrees/wt1"
        self.wt_b = self.home / "deneb/.claude/worktrees/wt2"
        for wt in (self.wt_a, self.wt_b):
            wt.mkdir(parents=True, exist_ok=True)
            (wt / ".git").write_text("gitdir: /elsewhere\n", encoding="utf-8")
            (wt / "gateway-go").mkdir(parents=True, exist_ok=True)

    def hold_with_session_a(self) -> None:
        self.decision(payload(
            "Edit", {"file_path": str(self.wt_a / "gateway-go/foo.go")},
            str(self.wt_a), "sess-a",
        ))

    def bash_decision(self, command, session_id="sess-b"):
        return self.decision(payload("Bash", {"command": command}, str(self.wt_b), session_id))

    def test_every_shell_write_form_reaches_the_claim_check(self) -> None:
        target = str(self.wt_b / "gateway-go/foo.go")
        forms = {
            "sed -i": f"sed -i 's/a/b/' {target}",
            "redirect": f"echo x > {target}",
            "append": f"printf y >> {target}",
            "heredoc": f"cat > {target} <<'EOF'\nx\nEOF",
            "tee": f"echo x | tee {target}",
            "cp dest": f"cp /etc/hostname {target}",
            # The one a shell parser cannot see: the write lives inside a script
            # fed to an interpreter on stdin.
            "python heredoc": "python3 - <<'PY'\nopen('gateway-go/foo.go','w').write('x')\nPY",
            "relative path": "sed -i 's/a/b/' gateway-go/foo.go",
        }
        for label, command in forms.items():
            with self.subTest(form=label):
                self.setUp()  # fresh ledger per form
                self.hold_with_session_a()
                self.assertEqual(self.bash_decision(command), "ask", label)

    def test_read_only_commands_never_register_a_claim(self) -> None:
        """A mention is not an edit. Registering claims from greps would warn
        other sessions about files nobody is touching."""
        for command in (
            "grep -rn foo gateway-go/foo.go",
            "cat gateway-go/foo.go",
            "python3 -c \"print(open('gateway-go/foo.go').read())\"",
            "git diff gateway-go/foo.go",
        ):
            self.bash_decision(command, "sess-a")
        self.assertFalse((self.home / ".claude/deneb-file-claims.json").exists(), "read-only commands claimed files")


class FileClaimWarnOnceTests(GuardTestCase):
    def setUp(self) -> None:
        super().setUp()
        self.wt_a = self.home / "deneb/.claude/worktrees/wt1"
        self.wt_b = self.home / "deneb/.claude/worktrees/wt2"
        for wt in (self.wt_a, self.wt_b):
            wt.mkdir(parents=True, exist_ok=True)
            (wt / ".git").write_text("gitdir: /elsewhere\n", encoding="utf-8")
            (wt / "gateway-go").mkdir(parents=True, exist_ok=True)

    def edit(self, wt, session_id):
        return self.decision(payload(
            "Edit", {"file_path": str(wt / "gateway-go/foo.go")}, str(wt), session_id))

    def test_a_warned_session_is_not_asked_again(self) -> None:
        """Re-asking on every write would fire dozens of times while one
        function is edited, and a guard that cries that often gets approved
        reflexively — worse than not having it."""
        self.edit(self.wt_a, "sess-a")
        self.assertEqual(self.edit(self.wt_b, "sess-b"), "ask")
        for _ in range(5):
            self.assertEqual(self.edit(self.wt_b, "sess-b"), "allow")

    def test_a_third_session_is_still_warned(self) -> None:
        """Settling with one asker must not disarm the file for everyone."""
        self.edit(self.wt_a, "sess-a")
        self.assertEqual(self.edit(self.wt_b, "sess-b"), "ask")
        self.assertEqual(self.edit(self.wt_b, "sess-c"), "ask")

    def test_holder_edits_do_not_reset_the_warned_list(self) -> None:
        """The holder refreshing its claim must not make an already-warned
        session start asking again."""
        self.edit(self.wt_a, "sess-a")
        self.assertEqual(self.edit(self.wt_b, "sess-b"), "ask")
        self.edit(self.wt_a, "sess-a")  # holder keeps working
        self.assertEqual(self.edit(self.wt_b, "sess-b"), "allow")


class ClaimKeyIdentityTests(GuardTestCase):
    """The key's identity half: same origin unifies, different repos separate.

    Both directions were caught live on 2026-08-27: a bare relative path made
    Deneb's README collide with SolarFlow's, and a byte-compared origin split
    ~/deneb (…/deneb) from ~/deneb-dev (…/Deneb.git) — one repo, two spellings.
    """

    def make_repo(self, name, origin=None, worktree_of=None):
        root = self.home / name
        root.mkdir(parents=True, exist_ok=True)
        if worktree_of is not None:
            gitdir = self.home / worktree_of / f".git/worktrees/{name}"
            gitdir.mkdir(parents=True, exist_ok=True)
            (root / ".git").write_text(f"gitdir: {gitdir}\n", encoding="utf-8")
        else:
            (root / ".git").mkdir(exist_ok=True)
            (root / ".git/HEAD").write_text("ref: refs/heads/main\n", encoding="utf-8")
            if origin:
                (root / ".git/config").write_text(
                    f'[remote "origin"]\n\turl = {origin}\n', encoding="utf-8")
        return root

    def key_for(self, repo_root):
        keyed = guard.claim_key(str(repo_root / "README.md"))
        self.assertIsNotNone(keyed)
        return keyed[0]

    def test_different_repos_never_share_a_key(self) -> None:
        a = self.make_repo("deneb-x", origin="https://github.com/choiceoh/deneb")
        b = self.make_repo("solarflow", origin="https://github.com/choiceoh/solarflow")
        self.assertNotEqual(self.key_for(a), self.key_for(b))

    def test_origin_spellings_unify(self) -> None:
        spellings = (
            "https://github.com/choiceoh/deneb",
            "https://github.com/choiceoh/Deneb.git",
            "git@github.com:choiceoh/Deneb.git",
        )
        keys = set()
        for i, url in enumerate(spellings):
            keys.add(self.key_for(self.make_repo(f"clone{i}", origin=url)))
        self.assertEqual(len(keys), 1, keys)

    def test_worktree_shares_its_parent_repos_identity(self) -> None:
        main = self.make_repo("mainrepo", origin="https://github.com/choiceoh/deneb")
        wt = self.make_repo("wt-branch", worktree_of="mainrepo")
        self.assertEqual(self.key_for(main), self.key_for(wt))

    def test_local_only_repo_still_gets_a_stable_identity(self) -> None:
        a = self.make_repo("local-a")
        b = self.make_repo("local-b")
        self.assertNotEqual(self.key_for(a), self.key_for(b))
