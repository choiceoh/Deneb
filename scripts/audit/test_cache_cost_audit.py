"""Stdlib regression tests for the cache-cost audit (arXiv:2607.12161 adoption)."""

from __future__ import annotations

import io
import json
import sys
import tempfile
import unittest
from pathlib import Path

AUDIT_DIR = Path(__file__).resolve().parent
if str(AUDIT_DIR) not in sys.path:
    sys.path.insert(0, str(AUDIT_DIR))

import cache_cost_audit as audit


def entry(ts: int, etype: str, run_id: str, data: dict) -> str:
    return json.dumps(
        {"ts": ts, "type": etype, "runId": run_id, "session": "s", "data": data}
    )


def run_start(ts: int, run_id: str, model: str = "k3", provider: str = "kimi") -> str:
    return entry(ts, "run.start", run_id, {"model": model, "provider": provider})


def turn_llm(
    ts: int,
    run_id: str,
    turn: int,
    uncached: int,
    output: int = 10,
    read: int = 0,
    creation: int = 0,
) -> str:
    return entry(
        ts,
        "turn.llm",
        run_id,
        {
            "turn": turn,
            "inputTokens": uncached,
            "outputTokens": output,
            "cacheReadTokens": read,
            "cacheCreationTokens": creation,
        },
    )


def turn_tool(ts: int, run_id: str, name: str, input_hash: str, output_len: int) -> str:
    return entry(
        ts,
        "turn.tool",
        run_id,
        {"turn": 1, "name": name, "inputHash": input_hash, "outputLen": output_len},
    )


class ReportFromFixture(unittest.TestCase):
    def build(self, lines: list[str], **kwargs) -> audit.Report:
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, "session.jsonl").write_text("\n".join(lines) + "\n")
            return audit.build_report(Path(tmp), since_days=0, **kwargs)

    def test_decomposition_and_price_ratio(self) -> None:
        lines = [
            run_start(1000, "r1"),
            turn_llm(1001, "r1", 1, uncached=1000, output=50, read=0, creation=2000),
            turn_llm(1002, "r1", 2, uncached=100, output=50, read=3000, creation=0),
        ]
        report = self.build(lines)
        cost = report.models["kimi/k3"]
        self.assertEqual(cost.runs, 1)
        self.assertEqual(cost.turns, 2)
        self.assertEqual(cost.uncached, 1100)
        self.assertEqual(cost.read, 3000)
        self.assertEqual(cost.creation, 2000)
        self.assertEqual(cost.nominal_input, 6100)
        # 1.0*1100 + 1.25*2000 + 0.1*3000 = 3900
        self.assertAlmostEqual(cost.weighted_input, 3900.0)
        self.assertAlmostEqual(cost.price_ratio, 3900.0 / 6100.0)

    def test_stalled_transition_flags_flat_read(self) -> None:
        # Turn 2 prompt = 45525 + 39936 = 85461; turn 3 reuses only 39936 —
        # the production kimi signature this audit exists to expose.
        lines = [
            run_start(1000, "r1"),
            turn_llm(1001, "r1", 1, uncached=39971),
            turn_llm(1002, "r1", 2, uncached=45525, read=39936),
            turn_llm(1003, "r1", 3, uncached=46225, read=39936),
        ]
        report = self.build(lines)
        self.assertEqual(report.transitions, 2)
        # Transition 1->2 reuses 39936/39971 (healthy); 2->3 reuses only
        # 39936 of the 85461-token prompt -> stalled.
        self.assertEqual(report.stalled_transitions, 1)
        # Unreused: (39971 - 39936) + (85461 - 39936).
        self.assertEqual(report.unreused_tokens, 35 + 45525)
        # The same accounting is attributed to the run's model.
        cost = report.models["kimi/k3"]
        self.assertEqual(cost.transitions, 2)
        self.assertEqual(cost.stalled_transitions, 1)
        self.assertEqual(cost.unreused_tokens, 35 + 45525)

    def test_healthy_growth_is_not_stalled(self) -> None:
        lines = [
            run_start(1000, "r1"),
            turn_llm(1001, "r1", 1, uncached=100, creation=10000),
            turn_llm(1002, "r1", 2, uncached=200, read=10100),
        ]
        report = self.build(lines)
        self.assertEqual(report.transitions, 1)
        self.assertEqual(report.stalled_transitions, 0)

    def test_run_boundary_reuse_bucketed_by_gap(self) -> None:
        lines = [
            run_start(0, "r1"),
            turn_llm(1, "r1", 1, uncached=100, read=900),
            # 2 minutes later, same model: turn-1 reads 80% of its prompt.
            run_start(120_000, "r2"),
            turn_llm(120_001, "r2", 1, uncached=200, read=800),
            # 10 minutes after that: different model -> excluded.
            entry(720_000, "run.start", "r3", {"model": "glm", "provider": "wh"}),
            turn_llm(720_001, "r3", 1, uncached=100, read=500),
        ]
        report = self.build(lines)
        self.assertEqual(list(report.boundary_reuse.keys()), [("kimi/k3", "<5m")])
        (ratio,) = report.boundary_reuse[("kimi/k3", "<5m")]
        self.assertAlmostEqual(ratio, 0.8)

    def test_rewrite_amplification_needs_creation_and_three_runs(self) -> None:
        lines = []
        ts = 0
        for i in range(3):
            rid = f"r{i}"
            lines.append(run_start(ts, rid))
            # Each run re-creates the whole 1000-token context.
            lines.append(turn_llm(ts + 1, rid, 1, uncached=0, creation=1000))
            ts += 10_000
        report = self.build(lines)
        self.assertEqual(len(report.amplification), 1)
        self.assertAlmostEqual(report.amplification[0], 3.0)

    def test_tool_repeats_split_at_cutoff(self) -> None:
        lines = [run_start(0, "r1"), turn_tool(1, "r1", "read_file", "h1", 5000)]
        # Two llm turns, then a repeat: gap 2 <= cutoff.
        lines += [turn_llm(2, "r1", 1, 10), turn_llm(3, "r1", 2, 10)]
        lines.append(turn_tool(4, "r1", "read_file", "h1", 5000))
        # Five more llm turns, then another repeat: gap 7-0 > cutoff, stub-sized.
        lines += [turn_llm(5 + i, "r1", 3 + i, 10) for i in range(5)]
        lines.append(turn_tool(20, "r1", "read_file", "h1", 5000))
        # Small first result: beyond-cutoff repeat but not stub-sized.
        lines.insert(1, turn_tool(0, "r1", "peek", "h2", 100))
        lines.append(turn_tool(21, "r1", "peek", "h2", 100))
        report = self.build(lines)
        stat = report.tools["read_file"]
        self.assertEqual(stat.calls, 3)
        self.assertEqual(stat.repeats_within_cutoff, 1)
        self.assertEqual(stat.repeats_beyond_cutoff, 1)
        self.assertEqual(stat.stub_sized_beyond_cutoff, 1)
        peek = report.tools["peek"]
        self.assertEqual(peek.repeats_beyond_cutoff, 1)
        self.assertEqual(peek.stub_sized_beyond_cutoff, 0)

    def test_malformed_lines_are_isolated(self) -> None:
        lines = [
            "{not json",
            run_start(1000, "r1"),
            turn_llm(1001, "r1", 1, uncached=100, read=900),
        ]
        report = self.build(lines)
        self.assertEqual(report.skipped_lines, 1)
        self.assertEqual(report.models["kimi/k3"].turns, 1)

    def test_session_prefix_filter(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, "client:main.jsonl").write_text(
                run_start(1000, "r1") + "\n" + turn_llm(1001, "r1", 1, 100) + "\n"
            )
            Path(tmp, "system:background.jsonl").write_text(
                run_start(1000, "r2") + "\n" + turn_llm(1001, "r2", 1, 100) + "\n"
            )
            report = audit.build_report(
                Path(tmp), since_days=0, session_prefix="client:"
            )
        self.assertEqual(report.files, 1)
        self.assertEqual(report.models["kimi/k3"].runs, 1)


class CliContract(unittest.TestCase):
    def test_text_and_json_render(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            Path(tmp, "s.jsonl").write_text(
                run_start(1000, "r1")
                + "\n"
                + turn_llm(1001, "r1", 1, uncached=100, read=900, creation=50)
                + "\n"
            )
            text = io.StringIO()
            self.assertEqual(
                audit.main(["--log-dir", tmp, "--since-days", "0"], stream=text), 0
            )
            self.assertIn("Model cost decomposition", text.getvalue())
            self.assertIn("kimi/k3", text.getvalue())

            raw = io.StringIO()
            self.assertEqual(
                audit.main(
                    ["--log-dir", tmp, "--since-days", "0", "--json"], stream=raw
                ),
                0,
            )
            payload = json.loads(raw.getvalue())
            self.assertEqual(payload["models"]["kimi/k3"]["nominalInput"], 1050)

    def test_missing_dir_fails_cleanly(self) -> None:
        self.assertEqual(audit.main(["--log-dir", "/nonexistent-cache-audit"]), 2)


if __name__ == "__main__":
    unittest.main()
