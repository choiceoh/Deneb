#!/usr/bin/env python3
"""Unit tests for the advisory token-economics readout (arXiv:2607.06906)."""

from __future__ import annotations

import contextlib
import io
import json
import os
import tempfile
import time
import unittest
from pathlib import Path

import rsi_bench_main
from rsi_bench.token_economics import agent_logs_dir, load_token_economics


def _write_logs(root: Path, rows: list[dict]) -> None:
    root.mkdir(parents=True, exist_ok=True)
    (root / "s1.jsonl").write_text("".join(json.dumps(r) + "\n" for r in rows), encoding="utf-8")


def _turn(run_id: str, *, ts: int, inp: int, out: int, cread: int = 0, ccreate: int = 0) -> dict:
    return {
        "ts": ts,
        "type": "turn.llm",
        "runId": run_id,
        "session": "client:main",
        "data": {
            "turn": 1,
            "inputTokens": inp,
            "outputTokens": out,
            "cacheReadTokens": cread,
            "cacheCreationTokens": ccreate,
            "textLen": 10,
            "toolCalls": 0,
        },
    }


def _run_end(run_id: str, *, ts: int) -> dict:
    return {"ts": ts, "type": "run.end", "runId": run_id, "session": "client:main", "data": {}}


class TokenEconomicsTests(unittest.TestCase):
    def test_computes_tau_cpm_cache_hit(self) -> None:
        now = int(time.time() * 1000)
        with tempfile.TemporaryDirectory() as tmp:
            logs = Path(tmp)
            _write_logs(
                logs,
                [
                    _turn("r1", ts=now, inp=1000, out=200, cread=5000, ccreate=100),
                    _turn("r1", ts=now, inp=800, out=300, cread=6000),
                    _run_end("r1", ts=now),
                    _turn("r2", ts=now, inp=500, out=100, cread=2000),
                    _run_end("r2", ts=now),
                ],
            )
            te = load_token_economics(logs, days=7)

        self.assertTrue(te.measured)
        self.assertEqual(te.completed_tasks, 2)
        self.assertEqual(te.input_tokens, 2300)
        self.assertEqual(te.output_tokens, 600)
        self.assertEqual(te.cache_read_tokens, 13000)
        self.assertEqual(te.work_tokens, 2900)
        self.assertAlmostEqual(te.tau, 1450.0)  # 2900 / 2
        self.assertAlmostEqual(te.cpm, 2 * 1_000_000 / 2900)
        self.assertAlmostEqual(te.cache_hit, 13000 / 15400)  # cread / (input+cread+ccreate)
        self.assertIn("cpm=", te.summary())

    def test_window_excludes_old_rows(self) -> None:
        now = int(time.time() * 1000)
        old = now - 30 * 86_400 * 1000
        with tempfile.TemporaryDirectory() as tmp:
            logs = Path(tmp)
            _write_logs(
                logs,
                [
                    _turn("old", ts=old, inp=9999, out=9999, cread=9999),
                    _run_end("old", ts=old),
                    _turn("new", ts=now, inp=100, out=50, cread=400),
                    _run_end("new", ts=now),
                ],
            )
            te = load_token_economics(logs, days=7)

        self.assertEqual(te.completed_tasks, 1)  # old run.end excluded
        self.assertEqual(te.work_tokens, 150)  # only the in-window turn

    def test_live_test_sessions_excluded(self) -> None:
        # Mock-native-client smokes (client:lt-*) are fresh sessions with no cache
        # continuity; counting them craters cacheHit and skews CPM. The Go aggregators
        # skip this prefix (aggregate_failed.go) — this readout must too.
        now = int(time.time() * 1000)
        with tempfile.TemporaryDirectory() as tmp:
            logs = Path(tmp)
            logs.mkdir(parents=True, exist_ok=True)
            (logs / "client:main-real.jsonl").write_text(
                json.dumps(_turn("r1", ts=now, inp=100, out=50, cread=900)) + "\n"
                + json.dumps(_run_end("r1", ts=now)) + "\n",
                encoding="utf-8",
            )
            (logs / "client:lt-999.jsonl").write_text(
                json.dumps(_turn("r2", ts=now, inp=5000, out=5, cread=0)) + "\n"
                + json.dumps(_run_end("r2", ts=now)) + "\n",
                encoding="utf-8",
            )
            te = load_token_economics(logs, days=7)

        self.assertEqual(te.completed_tasks, 1)  # only the real session's run.end
        self.assertEqual(te.input_tokens, 100)  # live-test's 5000 excluded
        self.assertAlmostEqual(te.cache_hit, 0.9, places=3)  # not dragged down by lt- 0-cache turn

    def test_unmeasured_when_no_logs(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            te = load_token_economics(Path(tmp) / "missing", days=7)
        self.assertFalse(te.measured)
        self.assertIsNone(te.tau)
        self.assertIsNone(te.cpm)
        self.assertIsNone(te.cache_hit)
        self.assertIn("unmeasured", te.summary())

    def test_tokens_without_completion(self) -> None:
        now = int(time.time() * 1000)
        with tempfile.TemporaryDirectory() as tmp:
            logs = Path(tmp)
            _write_logs(logs, [_turn("r1", ts=now, inp=100, out=50)])  # no run.end
            te = load_token_economics(logs, days=7)
        self.assertTrue(te.measured)
        self.assertEqual(te.completed_tasks, 0)
        self.assertIsNone(te.tau)  # tau undefined without completions
        self.assertEqual(te.work_tokens, 150)
        self.assertIn("tasks=0", te.summary())

    def test_malformed_lines_skipped(self) -> None:
        now = int(time.time() * 1000)
        with tempfile.TemporaryDirectory() as tmp:
            logs = Path(tmp)
            logs.mkdir(parents=True, exist_ok=True)
            (logs / "s1.jsonl").write_text(
                "not json\n"
                + json.dumps(_turn("r1", ts=now, inp=200, out=100))
                + "\n"
                + "{broken\n"
                + json.dumps(_run_end("r1", ts=now))
                + "\n",
                encoding="utf-8",
            )
            te = load_token_economics(logs, days=7)
        self.assertEqual(te.completed_tasks, 1)
        self.assertEqual(te.work_tokens, 300)

    def test_agent_logs_dir_env_override(self) -> None:
        prev = os.environ.get("DENEB_AGENT_LOGS_DIR")
        os.environ["DENEB_AGENT_LOGS_DIR"] = "/tmp/deneb-test-agent-logs"
        try:
            self.assertEqual(agent_logs_dir(), Path("/tmp/deneb-test-agent-logs"))
        finally:
            if prev is None:
                os.environ.pop("DENEB_AGENT_LOGS_DIR", None)
            else:
                os.environ["DENEB_AGENT_LOGS_DIR"] = prev


class SidecarInjectionTests(unittest.TestCase):
    """The advisory readout must ride alongside the report, never inside its scores."""

    def test_main_injects_sidecar_without_touching_scores(self) -> None:
        now = int(time.time() * 1000)
        with tempfile.TemporaryDirectory() as data_tmp, tempfile.TemporaryDirectory() as logs_tmp:
            logs = Path(logs_tmp)
            _write_logs(
                logs,
                [
                    _turn("r1", ts=now, inp=1000, out=200, cread=5000),
                    _run_end("r1", ts=now),
                ],
            )
            prev = os.environ.get("DENEB_AGENT_LOGS_DIR")
            os.environ["DENEB_AGENT_LOGS_DIR"] = str(logs)
            try:
                buf = io.StringIO()
                with contextlib.redirect_stdout(buf):
                    rc = rsi_bench_main.main(["--json", "--data-dir", data_tmp])
            finally:
                if prev is None:
                    os.environ.pop("DENEB_AGENT_LOGS_DIR", None)
                else:
                    os.environ["DENEB_AGENT_LOGS_DIR"] = prev

        self.assertEqual(rc, 0)
        payload = json.loads(buf.getvalue())
        # Sidecar present and correct.
        te = payload["token_economics"]
        self.assertTrue(te["advisory"])
        self.assertEqual(te["completed_tasks"], 1)
        self.assertEqual(te["tokens_per_task"], 1200.0)  # (1000+200)/1
        # Scored model is untouched: no token key leaks into scores or domains.
        self.assertNotIn("token_economics", payload["score"])
        for domain in payload["domains"]:
            self.assertNotIn("token", domain["id"])
            for metric in domain["metrics"]:
                self.assertNotIn("token", metric["id"])


if __name__ == "__main__":
    unittest.main()
