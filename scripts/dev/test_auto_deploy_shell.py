"""Retry, lock, dirty-tree, and quiet-period tests for auto-deploy.sh."""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from test_shell_support import isolated_env, run_script, write_executable

AUTO_LOG = Path("/tmp/deneb-auto-deploy.log")
AUTO_LOCK = Path("/tmp/deneb-auto-deploy.lock")


class AutoDeployShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.prod = self.root / "prod"
        self.state = self.root / "state"
        self.bin = self.root / "bin"
        self.calls = self.root / "calls.log"
        self.deployed_marker = self.root / "deployed"
        self.home.mkdir()
        (self.prod / ".git").mkdir(parents=True)
        (self.prod / "scripts/deploy").mkdir(parents=True)
        self.state.mkdir()
        self.bin.mkdir()
        self.saved_tmp = {
            path: path.read_bytes() if path.exists() else None
            for path in (AUTO_LOG, AUTO_LOCK)
        }
        self.addCleanup(self.restore_tmp)
        AUTO_LOG.unlink(missing_ok=True)
        AUTO_LOCK.unlink(missing_ok=True)

        write_executable(self.bin / "flock", """
            #!/usr/bin/env bash
            printf 'flock %s\n' "$*" >> "$FAKE_CALLS"
            exit "${FLOCK_RC:-0}"
        """)
        write_executable(self.bin / "date", """
            #!/usr/bin/env bash
            case "$1" in
              -Iseconds) echo "${FAKE_ISO:-2026-07-11T12:00:00+09:00}" ;;
              +%s) echo "${FAKE_NOW:-2000}" ;;
              *) /bin/date "$@" ;;
            esac
        """)
        write_executable(self.bin / "git", """
            #!/usr/bin/env bash
            printf 'git %s\n' "$*" >> "$FAKE_CALLS"
            case "$*" in
              "branch --show-current") echo "${GIT_BRANCH:-main}" ;;
              "diff --name-only --diff-filter=U") printf '%b' "${GIT_UNMERGED:-}" ;;
              "diff --quiet") exit "${GIT_WORKTREE_DIRTY:-0}" ;;
              "diff --cached --quiet") exit "${GIT_INDEX_DIRTY:-0}" ;;
              "status --porcelain --untracked-files=no") printf '%b' "${GIT_STATUS:- M tracked.txt\\n}" ;;
              "hash-object --stdin") cat >/dev/null; echo "${GIT_HASH:-hash123}" ;;
              "stash push --message "*)
                [[ "${GIT_STASH_PUSH_RC:-0}" == 0 ]] || exit "$GIT_STASH_PUSH_RC"
                echo "Saved working directory"
                ;;
              "stash list")
                if [[ "${GIT_STASH_PRESENT:-1}" == 1 ]]; then
                  echo "stash@{0}: On main: auto-deploy stash ${FAKE_ISO:-2026-07-11T12:00:00+09:00}"
                fi
                ;;
              "stash pop --quiet stash@{0}") exit "${GIT_STASH_POP_RC:-0}" ;;
              "fetch --quiet --tags origin main") exit "${GIT_FETCH_RC:-0}" ;;
              "rev-parse HEAD")
                if [[ -f "$FAKE_DEPLOYED_MARKER" ]]; then echo "${GIT_REMOTE_HEAD:-remote222}";
                else echo "${GIT_LOCAL_HEAD:-local111}"; fi
                ;;
              "rev-parse origin/main") echo "${GIT_REMOTE_HEAD:-remote222}" ;;
              "log -1 --format=%ct "*) echo "${GIT_REMOTE_TS:-1000}" ;;
              *) echo "unexpected git args: $*" >&2; exit 87 ;;
            esac
        """)
        write_executable(self.prod / "scripts/deploy/deploy.sh", """
            #!/usr/bin/env bash
            printf 'deploy cwd=%s\n' "$PWD" >> "$FAKE_CALLS"
            if [[ "${DEPLOY_RC:-0}" == 0 ]]; then
              touch "$FAKE_DEPLOYED_MARKER"
            fi
            exit "${DEPLOY_RC:-0}"
        """)

    def restore_tmp(self) -> None:
        for path, content in self.saved_tmp.items():
            if content is None:
                path.unlink(missing_ok=True)
            else:
                path.write_bytes(content)

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "DENEB_PROD_DIR": str(self.prod),
            "DENEB_STATE_DIR": str(self.state),
            "FAKE_CALLS": str(self.calls),
            "FAKE_DEPLOYED_MARKER": str(self.deployed_marker),
            "FLOCK_RC": "0",
            "GIT_BRANCH": "main",
            "GIT_LOCAL_HEAD": "local111",
            "GIT_REMOTE_HEAD": "remote222",
            "GIT_REMOTE_TS": "1000",
            "GIT_FETCH_RC": "0",
            "GIT_WORKTREE_DIRTY": "0",
            "GIT_INDEX_DIRTY": "0",
            "DENEB_AUTO_DEPLOY_QUIET_SEC": "0",
            "DENEB_AUTO_DEPLOY_RETRY_SEC": "600",
            "FAKE_NOW": "2000",
            "DEPLOY_RC": "0",
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def invoke(self, env=None):
        return run_script(
            "scripts/deploy/auto-deploy.sh",
            env=env or self.env(),
            timeout=10,
        )

    def log_text(self) -> str:
        return AUTO_LOG.read_text(encoding="utf-8") if AUTO_LOG.exists() else ""

    def call_text(self) -> str:
        return self.calls.read_text(encoding="utf-8") if self.calls.exists() else ""

    def test_busy_lock_exits_silently_before_repo_or_git_checks(self) -> None:
        proc = self.invoke(self.env(FLOCK_RC="1"))
        self.assertEqual((proc.returncode, proc.stdout, proc.stderr), (0, "", ""))
        self.assertEqual(self.call_text().strip(), "flock -n 9")
        self.assertFalse(AUTO_LOG.exists())

    def test_missing_repo_is_logged_but_watchdog_stays_green(self) -> None:
        missing = self.root / "missing"
        proc = self.invoke(self.env(DENEB_PROD_DIR=str(missing)))
        self.assertEqual(proc.returncode, 0)
        self.assertIn(f"ERROR: {missing} is not a git repo; skipping", self.log_text())
        self.assertNotIn("git ", self.call_text())

    def test_pause_reason_is_logged_once_then_throttled(self) -> None:
        pause = self.state / "auto-deploy.paused"
        pause.write_text("operator maintenance\nextra ignored\n", encoding="utf-8")
        first = self.invoke()
        second = self.invoke()
        self.assertEqual((first.returncode, second.returncode), (0, 0))
        self.assertEqual(self.log_text().count("auto-deploy paused: operator maintenance"), 1)
        dirty = (self.state / "auto-deploy.dirty-failed").read_text().split()
        self.assertEqual(dirty, ["paused", "2000"])
        self.assertNotIn("git branch", self.call_text())

    def test_non_main_branch_warns_and_never_fetches(self) -> None:
        proc = self.invoke(self.env(GIT_BRANCH="release"))
        self.assertEqual(proc.returncode, 0)
        self.assertIn("production is on 'release', not main; skipping", self.log_text())
        self.assertNotIn("git fetch", self.call_text())

    def test_unmerged_conflicts_are_listed_bounded_and_throttled(self) -> None:
        conflicts = "a.txt\\nb.txt\\nc.txt\\nd.txt\\ne.txt\\nf.txt\\n"
        first = self.invoke(self.env(GIT_UNMERGED=conflicts))
        second = self.invoke(self.env(GIT_UNMERGED=conflicts))
        self.assertEqual((first.returncode, second.returncode), (0, 0))
        log = self.log_text()
        self.assertEqual(log.count("unresolved merge conflicts"), 1)
        for name in ("a.txt", "b.txt", "c.txt", "d.txt", "e.txt"):
            self.assertIn(f"  {name}", log)
        self.assertNotIn("  f.txt", log)
        self.assertNotIn("git fetch", self.call_text())

    def test_dirty_stash_failure_records_key_and_skips_fetch(self) -> None:
        proc = self.invoke(self.env(
            GIT_WORKTREE_DIRTY="1",
            GIT_STASH_PUSH_RC="9",
            GIT_HASH="dirtyhash",
        ))
        self.assertEqual(proc.returncode, 0)
        self.assertIn("worktree dirty, auto-stashing", self.log_text())
        self.assertIn("auto-stash failed; skipping this tick", self.log_text())
        self.assertEqual(
            (self.state / "auto-deploy.dirty-failed").read_text().split(),
            ["dirty:dirtyhash", "2000"],
        )
        self.assertNotIn("git fetch", self.call_text())

    def test_dirty_tree_is_stashed_deployed_and_restored_on_exit(self) -> None:
        proc = self.invoke(self.env(GIT_INDEX_DIRTY="1"))
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = self.call_text()
        self.assertIn("git stash push --message auto-deploy stash", calls)
        self.assertIn("git fetch --quiet --tags origin main", calls)
        self.assertIn("deploy cwd=", calls)
        self.assertIn("git stash list", calls)
        self.assertIn("git stash pop --quiet stash@{0}", calls)
        self.assertIn("auto-stash popped successfully", self.log_text())
        self.assertEqual(
            (self.state / "auto-deploy.deployed-head").read_text().strip(),
            "remote222",
        )

    def test_fetch_failure_is_advisory_and_does_not_record_bad_head(self) -> None:
        proc = self.invoke(self.env(GIT_FETCH_RC="3"))
        self.assertEqual(proc.returncode, 0)
        self.assertIn("git fetch failed; will retry on next tick", self.log_text())
        self.assertFalse((self.state / "auto-deploy.failed-head").exists())
        self.assertNotIn("deploy cwd=", self.call_text())

    def test_first_equal_head_seeds_deployed_state_without_restart(self) -> None:
        env = self.env(GIT_LOCAL_HEAD="same333", GIT_REMOTE_HEAD="same333")
        proc = self.invoke(env)
        self.assertEqual(proc.returncode, 0)
        self.assertEqual(
            (self.state / "auto-deploy.deployed-head").read_text().strip(),
            "same333",
        )
        self.assertNotIn("deploy cwd=", self.call_text())

    def test_equal_local_remote_and_recorded_head_is_quiet_noop(self) -> None:
        (self.state / "auto-deploy.deployed-head").write_text("same333\n")
        proc = self.invoke(self.env(GIT_LOCAL_HEAD="same333", GIT_REMOTE_HEAD="same333"))
        self.assertEqual(proc.returncode, 0)
        self.assertEqual(self.log_text(), "")
        self.assertNotIn("deploy cwd=", self.call_text())

    def test_recent_failed_remote_head_is_throttled(self) -> None:
        (self.state / "auto-deploy.failed-head").write_text("remote222 1900\n")
        proc = self.invoke(self.env(FAKE_NOW="2000"))
        self.assertEqual(proc.returncode, 0)
        self.assertNotIn("deploy cwd=", self.call_text())
        self.assertEqual(self.log_text(), "")

    def test_quiet_period_defers_hot_main_and_reports_age(self) -> None:
        proc = self.invoke(self.env(
            DENEB_AUTO_DEPLOY_QUIET_SEC="300",
            GIT_REMOTE_TS="1900",
            FAKE_NOW="2000",
        ))
        self.assertEqual(proc.returncode, 0)
        self.assertIn("remote222 is 100s old (< 300s quiet period)", self.log_text())
        self.assertNotIn("deploy cwd=", self.call_text())

    def test_successful_deploy_records_actual_post_pull_head_and_clears_failures(self) -> None:
        (self.state / "auto-deploy.failed-head").write_text("old 1\n")
        (self.state / "auto-deploy.dirty-failed").write_text("old 1\n")
        proc = self.invoke()
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertEqual(
            (self.state / "auto-deploy.deployed-head").read_text().strip(),
            "remote222",
        )
        self.assertFalse((self.state / "auto-deploy.failed-head").exists())
        self.assertFalse((self.state / "auto-deploy.dirty-failed").exists())
        self.assertIn("deploy OK (head now remote222)", self.log_text())

    def test_failed_deploy_is_recorded_and_same_head_is_not_retried_immediately(self) -> None:
        first = self.invoke(self.env(DEPLOY_RC="17"))
        second = self.invoke(self.env(DEPLOY_RC="17"))
        self.assertEqual((first.returncode, second.returncode), (0, 0))
        self.assertEqual(
            (self.state / "auto-deploy.failed-head").read_text().split(),
            ["remote222", "2000"],
        )
        self.assertEqual(self.call_text().count("deploy cwd="), 1)
        self.assertIn("deploy FAILED (rc=17)", self.log_text())


if __name__ == "__main__":
    unittest.main()
