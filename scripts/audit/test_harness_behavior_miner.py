"""Deterministic tests for the cross-harness behavior miner.

Load-bearing assertions: command segmentation strips wrappers/env prefixes and
skips shell control constructs; exit codes come from the structured field or
the Codex preview text (never invented); subagent artifacts merge into their
parent session with timestamp ordering while Cursor's id-less groups fall back
to artifact-stem keys; verify-after-last-write only fires when a gate command
follows the final write; the episodes join drives the committed/no-commit
contrast; and the mine() orchestration runs end-to-end with an injected runner
(no numbat binary required) while surfacing parse failures instead of
dropping them silently.
"""

from __future__ import annotations

import json
import pathlib
import tempfile
import unittest

from harness_behavior_miner import (
    aggregate,
    command_segments,
    discover_artifacts,
    load_episodes,
    merge_sessions,
    mine,
    outcome_contrast,
    render_report,
    result_exit_code,
    segment_argv0,
    session_metrics,
)


def ev(etype: str, ts: str | None = None, **fields) -> dict:
    event = {"event_type": etype}
    if ts:
        event["timestamp"] = ts
    event.update(fields)
    return event


class CommandAnalysisTest(unittest.TestCase):
    def test_segments_split_on_operators_and_newlines(self) -> None:
        segs = command_segments("cd /repo && make check | tail -5\ngit status; ls")
        self.assertEqual(segs, ["cd /repo", "make check", "tail -5", "git status", "ls"])

    def test_heredoc_bodies_and_comments_are_dropped(self) -> None:
        cmd = (
            "# setup comment\n"
            "python3 - <<'PY'\n"
            "import json\n"
            "print(json.dumps({}))\n"
            "PY\n"
            "git status"
        )
        segs = command_segments(cmd)
        self.assertEqual(segs, ["python3 - <<'PY'", "git status"])

    def test_quoted_multiline_strings_stay_one_segment(self) -> None:
        cmd = 'python3 -c "\nimport json\nprint(json.dumps({}))\n" && ls'
        segs = command_segments(cmd)
        self.assertEqual(len(segs), 2)
        self.assertEqual(segment_argv0(segs[0]), "python3")
        self.assertEqual(segs[1], "ls")

    def test_pure_punctuation_segments_are_not_commands(self) -> None:
        self.assertIsNone(segment_argv0("} > out.txt"))
        self.assertIsNone(segment_argv0("-"))

    def test_argv0_strips_wrappers_env_and_paths(self) -> None:
        self.assertEqual(segment_argv0("FOO=1 sudo /usr/bin/python3 x.py"), "python3")
        self.assertEqual(segment_argv0("timeout 30 scripts/dev/live-test.sh smoke"), "timeout")
        self.assertIsNone(segment_argv0("cd /somewhere"))
        self.assertIsNone(segment_argv0("for f in a b"))
        self.assertEqual(segment_argv0("do echo hi"), "echo")

    def test_exit_code_structured_then_preview_text(self) -> None:
        self.assertEqual(result_exit_code({"exit_code": 2}), 2)
        self.assertEqual(result_exit_code({"exit_code": "0"}), 0)
        self.assertEqual(
            result_exit_code({"content_preview": "Wall time: 0.1 Process exited with code 1 Output:"}),
            1,
        )
        self.assertIsNone(result_exit_code({"content_preview": "plain output"}))


class SessionMetricsTest(unittest.TestCase):
    def make_record(self, events: list[dict]) -> dict:
        return {
            "agent": "claude",
            "session_id": "s1",
            "artifacts": ["a.jsonl"],
            "events": events,
            "start": None,
            "end": None,
            "project_path": "/repo",
        }

    def test_verify_after_last_write_requires_gate_after_final_write(self) -> None:
        base = [
            ev("prompt.user"),
            ev("command.exec", command="make check"),
            ev("file.write", file_path="x.go"),
        ]
        not_verified = session_metrics(self.make_record(list(base)))
        self.assertFalse(not_verified["verify_after_last_write"])
        verified = session_metrics(
            self.make_record(base + [ev("command.exec", command="go test ./...")])
        )
        self.assertTrue(verified["verify_after_last_write"])
        self.assertEqual(verified["verify_cmds"], 2)

    def test_retries_count_adjacent_identical_execs(self) -> None:
        record = self.make_record(
            [
                ev("command.exec", command="go build ./..."),
                ev("command.exec", command="go build ./..."),
                ev("command.exec", command="ls"),
                ev("command.exec", command="ls"),
                ev("command.exec", command="ls -la"),
            ]
        )
        self.assertEqual(session_metrics(record)["retries"], 2)

    def test_exit_codes_and_categories(self) -> None:
        record = self.make_record(
            [
                ev("command.exec", command="git commit -m x"),
                ev("command.result", content_preview="Process exited with code 0"),
                ev("command.exec", command="python3 -c 'print(1)' | head -3"),
                ev("command.result", content_preview="Process exited with code 1"),
                ev("command.result", content_preview="no code here"),
            ]
        )
        m = session_metrics(record)
        self.assertEqual(m["results_with_code"], 2)
        self.assertEqual(m["results_fail"], 1)
        self.assertEqual(m["commit_cmds"], 1)
        self.assertEqual(m["inline_scripts"], 1)
        self.assertEqual(m["limit_pipes"], 1)

    def test_duration_prefers_group_bounds_then_event_span(self) -> None:
        record = self.make_record(
            [
                ev("command.exec", "2026-08-01T00:00:00Z", command="ls"),
                ev("command.exec", "2026-08-01T00:10:00Z", command="pwd"),
            ]
        )
        self.assertEqual(session_metrics(record)["duration_s"], 600.0)
        record["start"], record["end"] = "2026-08-01T00:00:00Z", "2026-08-01T00:30:00Z"
        self.assertEqual(session_metrics(record)["duration_s"], 1800.0)

    def test_episode_join_adds_outcome_fields(self) -> None:
        record = self.make_record([ev("prompt.user")])
        m = session_metrics(record, {"commits": ["abc", "def"], "branch": "b"})
        self.assertEqual(m["episode_commits"], 2)
        self.assertEqual(m["episode_branch"], "b")


class MergeTest(unittest.TestCase):
    def test_subagent_artifacts_merge_by_session_id_in_timestamp_order(self) -> None:
        main_group = {
            "session_id": "parent",
            "source_agent": "claude-code",
            "start": "2026-08-01T00:00:00Z",
            "end": "2026-08-01T01:00:00Z",
            "events": [ev("command.exec", "2026-08-01T00:30:00Z", command="later")],
        }
        sub_group = {
            "session_id": "parent",
            "source_agent": "claude-code",
            "events": [
                ev("command.exec", "2026-08-01T00:10:00Z", command="earlier", sub_agent="sub1")
            ],
        }
        records = merge_sessions(
            [
                ("claude", pathlib.Path("main.jsonl"), [main_group]),
                ("claude", pathlib.Path("sub.jsonl"), [sub_group]),
            ]
        )
        self.assertEqual(len(records), 1)
        rec = records[0]
        self.assertEqual([e["command"] for e in rec["events"]], ["earlier", "later"])
        self.assertEqual(rec["start"], "2026-08-01T00:00:00Z")
        self.assertEqual(session_metrics(rec)["subagents"], 1)

    def test_cursor_groups_without_ids_key_on_artifact_stem(self) -> None:
        group = {"source_agent": "cursor", "events": [ev("command.exec", command="ls")]}
        records = merge_sessions(
            [("cursor", pathlib.Path("deadbeef-1234.jsonl"), [group])]
        )
        self.assertEqual(records[0]["session_id"], "deadbeef-1234")


class AggregateTest(unittest.TestCase):
    def metrics(self, **overrides) -> dict:
        base = {
            "agent": "claude",
            "session_id": "s",
            "events": 10,
            "user_prompts": 1,
            "execs": 10,
            "file_reads": 6,
            "file_writes": 2,
            "verify_cmds": 1,
            "verify_after_last_write": True,
            "commit_cmds": 1,
            "push_cmds": 0,
            "inline_scripts": 2,
            "limit_pipes": 1,
            "retries": 1,
            "results_with_code": 5,
            "results_fail": 1,
            "subagents": 0,
            "duration_s": 600.0,
            "top_cmds": {"git": 4, "ls": 2},
        }
        base.update(overrides)
        return base

    def test_rates_and_denominators(self) -> None:
        agg = aggregate([self.metrics(), self.metrics(verify_cmds=0, verify_after_last_write=False, file_writes=0)])
        self.assertEqual(agg["sessions"], 2)
        self.assertEqual(agg["pct_verify"], 50.0)
        # Only the session that wrote files enters the after-write denominator.
        self.assertEqual(agg["pct_verify_after_last_write"], 100.0)
        self.assertEqual(agg["exit_coverage"], 50.0)
        self.assertEqual(agg["fail_rate"], 20.0)
        self.assertEqual(agg["reads_per_write"], 6.0)
        self.assertEqual(agg["top_cmds"][0][0], "git")

    def test_empty_is_explicitly_empty(self) -> None:
        self.assertEqual(aggregate([]), {"sessions": 0})

    def test_outcome_contrast_splits_on_episode_commits(self) -> None:
        committed = self.metrics(episode_commits=2)
        uncommitted = self.metrics(episode_commits=0, verify_cmds=0)
        unjoined = self.metrics()
        contrast = outcome_contrast([committed, uncommitted, unjoined])
        self.assertEqual(contrast["joined"], 2)
        self.assertEqual(contrast["unjoined"], 1)
        self.assertEqual(contrast["committed"]["sessions"], 1)
        self.assertEqual(contrast["uncommitted"]["pct_verify"], 0.0)


class EndToEndTest(unittest.TestCase):
    def test_mine_with_injected_runner_and_fixture_home(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = pathlib.Path(tmp)
            cc_dir = home / ".claude" / "projects" / "proj"
            cc_dir.mkdir(parents=True)
            (cc_dir / "s1.jsonl").write_text("{}", encoding="utf-8")
            (cc_dir / "broken.jsonl").write_text("{}", encoding="utf-8")
            memdir = home / ".claude" / "deneb-session-memory"
            memdir.mkdir(parents=True)
            (memdir / "episodes.jsonl").write_text(
                json.dumps({"session_id": "s1", "commits": ["c1"], "branch": "b"}) + "\n",
                encoding="utf-8",
            )

            def fake_runner(_bin: str, artifact: pathlib.Path):
                if artifact.name == "broken.jsonl":
                    return [], f"{artifact}: bad JSON"
                return [
                    {
                        "session_id": "s1",
                        "source_agent": "claude-code",
                        "events": [
                            ev("prompt.user", "2026-08-01T00:00:00Z"),
                            ev("file.write", "2026-08-01T00:01:00Z", file_path="x"),
                            ev("command.exec", "2026-08-01T00:02:00Z", command="make check"),
                        ],
                    }
                ], None

            result = mine(
                home,
                ("claude",),
                numbat_bin="unused",
                episodes_path=memdir / "episodes.jsonl",
                runner=fake_runner,
                log=lambda _msg: None,
            )
            self.assertEqual(result["corpus"]["claude"], {"artifacts": 2, "failures": 1})
            self.assertEqual(len(result["failures"]), 1)
            self.assertEqual(len(result["metrics"]), 1)
            metric = result["metrics"][0]
            self.assertEqual(metric["episode_commits"], 1)
            self.assertTrue(metric["verify_after_last_write"])
            report = render_report(
                result["aggregates"], result["contrast"], result["corpus"]
            )
            self.assertIn("## Behavior profile", report)
            self.assertIn("| claude | 2 | 1 | 1 |", report)
            self.assertIn("Parse failures above are dropped artifacts", report)

    def test_discovery_respects_since_days(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = pathlib.Path(tmp)
            cc_dir = home / ".claude" / "projects" / "p"
            cc_dir.mkdir(parents=True)
            path = cc_dir / "old.jsonl"
            path.write_text("{}", encoding="utf-8")
            mtime = path.stat().st_mtime
            found = discover_artifacts(
                home, ("claude",), since_days=1, now_s=mtime + 2 * 86400
            )
            self.assertEqual(found["claude"], [])
            found = discover_artifacts(
                home, ("claude",), since_days=3, now_s=mtime + 2 * 86400
            )
            self.assertEqual(found["claude"], [path])

    def test_load_episodes_last_write_wins_and_skips_junk(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "episodes.jsonl"
            path.write_text(
                "\n".join(
                    [
                        json.dumps({"session_id": "s1", "commits": []}),
                        "not json",
                        json.dumps({"session_id": "s1", "commits": ["c1"]}),
                    ]
                )
                + "\n",
                encoding="utf-8",
            )
            episodes = load_episodes(path)
            self.assertEqual(episodes["s1"]["commits"], ["c1"])


if __name__ == "__main__":
    unittest.main()
