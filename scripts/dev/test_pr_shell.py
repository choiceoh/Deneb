"""Safety-boundary tests for the PR watch-and-land shell workflow."""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from test_shell_support import isolated_env, run_script, write_executable


class PRShellTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.bin = self.root / "bin"
        self.log = self.root / "calls.log"
        self.home.mkdir()
        self.bin.mkdir()

        write_executable(self.bin / "gh", """
            #!/usr/bin/env bash
            printf 'gh %s\n' "$*" >> "$FAKE_LOG"
            if [[ "$1 $2" == "pr checks" ]]; then
              if [[ " $* " == *" --watch "* ]]; then
                exit "${GH_WATCH_RC:-0}"
              fi
              printf '%b' "${GH_CHECKS:-}"
              exit 0
            fi
            if [[ "$1 $2" == "pr view" ]]; then
              case "$*" in
                *"--json headRefName"*) printf '%s\n' "${GH_BRANCH:-codex/health}" ;;
                *"--json headRefOid"*) printf '%s\n' "${GH_HEAD:-head111}" ;;
                *"--json state"*) printf '%s\n' "${GH_STATE:-OPEN}" ;;
                *"--json mergeCommit"*) printf '%s\n' "${GH_MERGE_SHA:-merge999}" ;;
                *"--json url"*) printf '%s\n' "${GH_URL:-https://example.test/pr/42}" ;;
                *) echo 'unexpected gh view' >&2; exit 92 ;;
              esac
              exit 0
            fi
            if [[ "$1 $2" == "pr merge" ]]; then
              exit "${GH_MERGE_RC:-0}"
            fi
            echo "unexpected gh args: $*" >&2
            exit 93
        """)
        write_executable(self.bin / "git", """
            #!/usr/bin/env bash
            printf 'git %s\n' "$*" >> "$FAKE_LOG"
            case "$*" in
              "rev-parse --quiet --verify HEAD")
                printf '%s\n' "${GIT_HEAD_OID:-}"
                ;;
              "rev-parse --show-toplevel")
                printf '%s\n' "${GIT_TOPLEVEL:-}"
                ;;
              "rev-parse --quiet --verify refs/heads/"*)
                [[ -n "${GIT_LOCAL_OID:-head111}" ]] || exit 1
                printf '%s\n' "${GIT_LOCAL_OID:-head111}"
                ;;
              "fetch origin main refs/pull/"*"/head --quiet")
                if [[ "${GIT_FETCH_PR_RC:-0}" != 0 ]]; then exit "$GIT_FETCH_PR_RC"; fi
                ;;
              "fetch origin main --quiet") ;;
              "merge-base --is-ancestor "*) exit "${GIT_ANCESTOR_RC:-0}" ;;
              "merge-base "*" origin/main") printf '%s\n' "${GIT_MERGE_BASE:-base000}" ;;
              "diff --name-only "*" head111") printf '%b' "${PR_FILES:-}" ;;
              "diff --name-only "*" origin/main") printf '%b' "${MAIN_FILES:-}" ;;
              "push origin --delete "*) exit "${GIT_DELETE_RC:-0}" ;;
              *) echo "unexpected git args: $*" >&2; exit 94 ;;
            esac
        """)

    def env(self, **values: str) -> dict[str, str]:
        defaults = {
            "FAKE_LOG": str(self.log),
            "GH_WATCH_RC": "0",
            "GH_BRANCH": "codex/health",
            "GH_HEAD": "head111",
            "GH_STATE": "OPEN",
            "GH_MERGE_SHA": "merge999",
            "GH_URL": "https://example.test/pr/42",
            "GIT_LOCAL_OID": "head111",
            "GIT_ANCESTOR_RC": "0",
        }
        defaults.update(values)
        return isolated_env(self.home, self.bin, **defaults)

    def invoke(self, *args: str, env=None):
        return run_script("scripts/dev/pr.sh", *args, env=env or self.env())

    def calls(self) -> list[str]:
        if not self.log.exists():
            return []
        return self.log.read_text(encoding="utf-8").splitlines()

    def test_missing_arguments_and_unknown_command_keep_exit_two_contract(self) -> None:
        for args in [(), ("watch",), ("invalid", "42")]:
            with self.subTest(args=args):
                self.log.unlink(missing_ok=True)
                proc = self.invoke(*args)
                self.assertEqual(proc.returncode, 2)
                self.assertIn("{watch|land|attach} <pr-number>", proc.stderr)
        self.assertEqual(self.calls(), [])

    def test_attach_skips_when_the_checkout_is_not_at_the_pr_head(self) -> None:
        """A brief computed from the wrong tree would describe the wrong code."""
        proc = self.invoke("attach", "42", env=self.env(GIT_HEAD_OID="other222"))
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn("impact brief skipped", proc.stderr)
        self.assertNotIn("gh pr edit", "\n".join(self.calls()))

    def test_land_is_not_blocked_when_the_brief_cannot_be_built(self) -> None:
        """Review evidence is best-effort; a green PR still lands without it."""
        proc = self.invoke("land", "42", env=self.env(GIT_HEAD_OID="head111", GIT_TOPLEVEL=""))
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn("PR #42 landed", proc.stdout)

    def test_watch_stays_a_pure_poll(self) -> None:
        """watch is called repeatedly; the impact brief belongs on land, not here."""
        self.invoke("watch", "42")
        self.assertEqual(self.calls(), ["gh pr checks 42 --watch --interval 30"])

    def test_watch_green_reports_success_without_extra_api_calls(self) -> None:
        proc = self.invoke("watch", "42")
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout, "PR #42: checks green\n")
        self.assertEqual(self.calls(), ["gh pr checks 42 --watch --interval 30"])

    def test_watch_failure_prints_only_red_or_pending_checks(self) -> None:
        env = self.env(
            GH_WATCH_RC="1",
            GH_CHECKS="unit\\tfail\\nformat\\tpass\\ndocs\\tskipping\\nqueue\\tpending\\n",
        )
        proc = self.invoke("watch", "17", env=env)
        self.assertEqual(proc.returncode, 1)
        self.assertEqual(proc.stdout, "")
        self.assertIn("PR #17: checks NOT green", proc.stderr)
        self.assertIn("  unit: fail", proc.stderr)
        self.assertIn("  queue: pending", proc.stderr)
        self.assertNotIn("format", proc.stderr)
        self.assertNotIn("docs", proc.stderr)
        self.assertEqual(self.calls(), [
            "gh pr checks 17 --watch --interval 30",
            "gh pr checks 17",
        ])

    def test_land_refuses_when_local_branch_no_longer_matches_pr_head(self) -> None:
        proc = self.invoke("land", "42", env=self.env(GIT_LOCAL_OID="other222"))
        self.assertEqual(proc.returncode, 1)
        self.assertIn("PR #42 head is head111", proc.stderr)
        self.assertIn("local branch codex/health is other222", proc.stderr)
        self.assertIn("parallel session?", proc.stderr)
        self.assertNotIn("gh pr merge", self.calls())
        self.assertNotIn("git fetch", "\n".join(self.calls()))

    def test_already_merged_pr_skips_staleness_guard_and_merge(self) -> None:
        proc = self.invoke("land", "42", env=self.env(GH_STATE="MERGED"))
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn("PR #42 landed: merge999 is on origin/main", proc.stdout)
        self.assertTrue(proc.stdout.rstrip().endswith("https://example.test/pr/42"))
        calls = self.calls()
        self.assertNotIn("gh pr merge 42 --squash", calls)
        self.assertNotIn("git merge-base head111 origin/main", calls)
        self.assertIn("git fetch origin main --quiet", calls)
        self.assertIn("git merge-base --is-ancestor merge999 origin/main", calls)
        self.assertIn("git push origin --delete codex/health", calls)

    def test_when_open_nonoverlapping_pr_fetches_checks_and_squash_merges(self) -> None:
        env = self.env(
            PR_FILES="scripts/dev/tool.py\\nREADME.md\\n",
            MAIN_FILES="gateway-go/main.go\\ndocs/guide.md\\n",
        )
        proc = self.invoke("land", "42", env=env)
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = self.calls()
        self.assertIn("git fetch origin main refs/pull/42/head --quiet", calls)
        self.assertIn("git merge-base head111 origin/main", calls)
        self.assertIn("gh pr merge 42 --squash", calls)
        self.assertIn("git merge-base --is-ancestor merge999 origin/main", calls)

    def test_when_overlapping_main_change_refuses_merge_and_lists_each_file(self) -> None:
        env = self.env(
            PR_FILES="shared/a.txt\\nshared/b.txt\\nonly-pr.txt\\n",
            MAIN_FILES="shared/b.txt\\nshared/a.txt\\nonly-main.txt\\n",
        )
        proc = self.invoke("land", "42", env=env)
        self.assertEqual(proc.returncode, 1)
        self.assertIn("shares files with newer origin/main commits", proc.stderr)
        self.assertIn("    shared/a.txt", proc.stderr)
        self.assertIn("    shared/b.txt", proc.stderr)
        self.assertIn("PR_LAND_ALLOW_STALE=1", proc.stderr)
        self.assertNotIn("gh pr merge 42 --squash", self.calls())

    def test_explicit_stale_override_allows_overlap(self) -> None:
        env = self.env(
            PR_FILES="shared.txt\\n",
            MAIN_FILES="shared.txt\\n",
            PR_LAND_ALLOW_STALE="1",
        )
        proc = self.invoke("land", "42", env=env)
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("gh pr merge 42 --squash", self.calls())
        self.assertNotIn("shares files", proc.stderr)

    def test_missing_local_branch_does_not_trigger_parallel_session_guard(self) -> None:
        env = self.env(GIT_LOCAL_OID="", PR_FILES="a\\n", MAIN_FILES="b\\n")
        proc = self.invoke("land", "42", env=env)
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("gh pr merge 42 --squash", self.calls())

    def test_pr_fetch_fallback_keeps_landing_when_pull_ref_fetch_is_unavailable(self) -> None:
        env = self.env(
            GIT_FETCH_PR_RC="1",
            PR_FILES="only-pr\\n",
            MAIN_FILES="only-main\\n",
        )
        proc = self.invoke("land", "42", env=env)
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        calls = self.calls()
        self.assertIn("git fetch origin main refs/pull/42/head --quiet", calls)
        self.assertGreaterEqual(calls.count("git fetch origin main --quiet"), 2)

    def test_when_merge_commit_must_really_be_ancestor_of_origin_main(self) -> None:
        env = self.env(
            GH_STATE="MERGED",
            GIT_ANCESTOR_RC="1",
        )
        proc = self.invoke("land", "42", env=env)
        self.assertEqual(proc.returncode, 1)
        self.assertIn("shows MERGED but merge999 is NOT on origin/main", proc.stderr)
        self.assertIn("stacked base?", proc.stderr)
        self.assertNotIn("git push origin --delete", "\n".join(self.calls()))

    def test_remote_branch_delete_failure_is_best_effort_after_verified_merge(self) -> None:
        env = self.env(GH_STATE="MERGED", GIT_DELETE_RC="1")
        proc = self.invoke("land", "42", env=env)
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)
        self.assertIn("branch codex/health cleaned up", proc.stdout)
        self.assertIn("git push origin --delete codex/health", self.calls())


if __name__ == "__main__":
    unittest.main()
