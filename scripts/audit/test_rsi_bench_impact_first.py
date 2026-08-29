"""Tests for the impact-first Process metric's evidence layer."""

from __future__ import annotations

import json
import tempfile
import time
import unittest
from pathlib import Path

from rsi_bench.impact_first import (
    classify_order,
    load_impact_first_window,
    scan_agent_log,
    scan_rollout,
)


def _now_ms() -> int:
    return int(time.time() * 1000)


def _tool(ts: int, run: str, name: str, targets: list[str] | None = None, **extra) -> str:
    data = {"turn": 1, "name": name}
    if targets is not None:
        data["targets"] = targets
    data.update(extra)
    return json.dumps({"ts": ts, "type": "turn.tool", "runId": run, "session": "s", "data": data})


def _write_log(path: Path, lines: list[str]) -> None:
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def _call(cmd: str) -> str:
    return json.dumps(
        {
            "timestamp": "2026-08-29T00:00:00Z",
            "type": "response_item",
            "payload": {"type": "function_call", "name": "exec_command", "arguments": json.dumps({"cmd": cmd})},
        }
    )


def _patch() -> str:
    return json.dumps(
        {
            "timestamp": "2026-08-29T00:00:01Z",
            "type": "event_msg",
            "payload": {"type": "patch_apply_end", "stdout": "Success."},
        }
    )


class ClassifyOrderTests(unittest.TestCase):
    def test_no_edit_is_not_counted(self) -> None:
        self.assertIsNone(classify_order([1, 2], []))

    def test_graph_before_edit_is_first(self) -> None:
        self.assertEqual(classify_order([1], [5]), "first")

    def test_graph_after_edit_is_late(self) -> None:
        self.assertEqual(classify_order([9], [5]), "late")

    def test_no_graph_call_is_late(self) -> None:
        self.assertEqual(classify_order([], [5]), "late")


class AgentLogScanTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self.tmp.name)
        self.addCleanup(self.tmp.cleanup)
        self.now = _now_ms()

    def test_impact_before_edit(self) -> None:
        path = self.dir / "a.jsonl"
        _write_log(
            path,
            [
                _tool(self.now, "r1", "codegraph_impact"),
                _tool(self.now, "r1", "edit", ["gateway-go/internal/x.go"]),
            ],
        )
        self.assertEqual(scan_agent_log(path, 0), ["first"])

    def test_edit_without_graph(self) -> None:
        path = self.dir / "b.jsonl"
        _write_log(path, [_tool(self.now, "r1", "write", ["a/b.kt"])])
        self.assertEqual(scan_agent_log(path, 0), ["late"])

    def test_non_source_edit_is_not_a_run(self) -> None:
        """Writing docs is not a missed impact check."""
        path = self.dir / "c.jsonl"
        _write_log(path, [_tool(self.now, "r1", "write", ["docs/notes.md"])])
        self.assertEqual(scan_agent_log(path, 0), [])

    def test_blocked_graph_call_does_not_count_as_consultation(self) -> None:
        path = self.dir / "d.jsonl"
        _write_log(
            path,
            [
                _tool(self.now, "r1", "codegraph_impact", blocked="hook"),
                _tool(self.now, "r1", "edit", ["x.go"]),
            ],
        )
        self.assertEqual(scan_agent_log(path, 0), ["late"])

    def test_runs_are_scored_independently(self) -> None:
        path = self.dir / "e.jsonl"
        _write_log(
            path,
            [
                _tool(self.now, "r1", "codegraph_node"),
                _tool(self.now, "r1", "edit", ["x.go"]),
                _tool(self.now, "r2", "edit", ["y.ts"]),
            ],
        )
        self.assertEqual(sorted(scan_agent_log(path, 0)), ["first", "late"])

    def test_rows_before_the_cutoff_are_ignored(self) -> None:
        path = self.dir / "f.jsonl"
        _write_log(path, [_tool(1000, "r1", "edit", ["x.go"])])
        self.assertEqual(scan_agent_log(path, self.now), [])


class RolloutScanTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self.tmp.name)
        self.addCleanup(self.tmp.cleanup)

    def _rollout(self, name: str, lines: list[str]) -> Path:
        path = self.dir / name
        path.write_text("\n".join(lines) + "\n", encoding="utf-8")
        return path

    def test_cli_impact_before_patch(self) -> None:
        path = self._rollout("rollout-a.jsonl", [_call("codegraph impact Foo"), _patch()])
        self.assertEqual(scan_rollout(path), "first")

    def test_cli_impact_after_patch(self) -> None:
        path = self._rollout("rollout-b.jsonl", [_patch(), _call("codegraph impact Foo")])
        self.assertEqual(scan_rollout(path), "late")

    def test_session_without_edits_is_not_counted(self) -> None:
        path = self._rollout("rollout-c.jsonl", [_call("codegraph impact Foo")])
        self.assertIsNone(scan_rollout(path))

    def test_grep_for_the_word_codegraph_is_not_a_consultation(self) -> None:
        path = self._rollout("rollout-d.jsonl", [_call("rg 'codegraph' docs/"), _patch()])
        self.assertEqual(scan_rollout(path), "late")

    def test_piped_invocation_is_recognized(self) -> None:
        path = self._rollout(
            "rollout-e.jsonl", [_call("cd /repo && codegraph callers Foo | head -20"), _patch()]
        )
        self.assertEqual(scan_rollout(path), "first")


class WindowTests(unittest.TestCase):
    def test_isolated_data_dir_does_not_read_production_logs(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            data = Path(tmp) / "data"
            data.mkdir()
            window = load_impact_first_window(data=data)
            self.assertEqual(window.edit_runs, 0)
            self.assertIsNone(window.rate)
            self.assertEqual(window.lanes_seen, set())

    def test_both_lanes_aggregate(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            data = root / "data"
            logs = root / "agent-logs"
            rollouts = data / "coding_dispatch_sessions" / ".codex" / "sessions"
            logs.mkdir(parents=True)
            rollouts.mkdir(parents=True)
            now = _now_ms()
            _write_log(
                logs / "s.jsonl",
                [_tool(now, "r1", "codegraph_impact"), _tool(now, "r1", "edit", ["x.go"])],
            )
            (rollouts / "rollout-x.jsonl").write_text(
                "\n".join([_patch()]) + "\n", encoding="utf-8"
            )
            window = load_impact_first_window(data=data)
            self.assertEqual(window.edit_runs, 2)
            self.assertEqual(window.impact_first, 1)
            self.assertEqual(window.impact_late, 1)
            self.assertAlmostEqual(window.rate, 0.5)
            self.assertEqual(window.lanes_seen, {"runtime", "dispatch"})


if __name__ == "__main__":
    unittest.main()
