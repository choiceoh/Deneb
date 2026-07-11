"""Concurrency and wire-contract tests for the split puppet broker."""

from __future__ import annotations

import contextlib
import io
import json
import os
import subprocess
import sys
import tempfile
import threading
import unittest
import urllib.request
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))

import puppet_broker_http as broker_http
import puppet_broker_state as broker_state


def request_payload(*, stream: bool = True) -> dict:
    return {
        "model": "coding-seat",
        "stream": stream,
        "messages": [
            {"role": "system", "content": "# Agent\nStay deterministic."},
            {"role": "user", "content": "run the task"},
        ],
        "tools": [],
    }


class BrokerStateTest(unittest.TestCase):
    def test_timeout_claim_rejects_late_operator_response(self) -> None:
        broker = broker_state.Broker("agent-seat", "")
        entry = broker.register(request_payload(), "stream")

        response = broker.final_response(entry, "hold timeout")
        ok, message = broker.respond(entry.id, {"text": "too late"})

        self.assertEqual(response, {"error": "hold timeout"})
        self.assertFalse(ok)
        self.assertIn("answered", message)
        self.assertEqual(entry.response, response)

    def test_response_timeout_race_has_exactly_one_winner(self) -> None:
        for _ in range(100):
            broker = broker_state.Broker("agent-seat", "")
            entry = broker.register(request_payload(), "stream")
            gate = threading.Barrier(3)
            outcome: dict[str, object] = {}

            def respond() -> None:
                gate.wait()
                outcome["respond"] = broker.respond(entry.id, {"text": "operator"})

            def timeout() -> None:
                gate.wait()
                outcome["final"] = broker.final_response(entry, "timeout")

            threads = [threading.Thread(target=respond), threading.Thread(target=timeout)]
            for thread in threads:
                thread.start()
            gate.wait()
            for thread in threads:
                thread.join(timeout=2)
                self.assertFalse(thread.is_alive())

            accepted, _message = outcome["respond"]
            final = outcome["final"]
            self.assertEqual(entry.response, final)
            if accepted:
                self.assertEqual(final, {"text": "operator"})
            else:
                self.assertEqual(final, {"error": "timeout"})

    def test_finish_is_idempotent_and_keeps_full_request(self) -> None:
        broker = broker_state.Broker("agent-seat", "")
        entry = broker.register(request_payload(), "complete")
        broker.respond(entry.id, {"text": "done"})
        broker.finish(entry, "done")
        broker.finish(entry, "gone")

        self.assertEqual(len(broker.history), 1)
        self.assertIs(broker.get_any(entry.id), entry)
        self.assertEqual(broker.history[0]["status"], "done")

    def test_journal_rotates_and_truncates_request_tail(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            journal = Path(tmp) / "puppet.jsonl"
            journal.write_text("old journal content", encoding="utf-8")
            broker = broker_state.Broker("agent-seat", str(journal))
            payload = request_payload()
            payload["messages"][-1]["content"] = "x" * 200
            entry = broker.register(payload, "stream")
            broker.respond(entry.id, {"text": "done"})
            with mock.patch.object(broker_state, "JOURNAL_MAX_BYTES", 8):
                with mock.patch.object(broker_state, "JOURNAL_TRUNC", 20):
                    broker.finish(entry, "done")

            self.assertEqual((journal.with_suffix(journal.suffix + ".1")).read_text(), "old journal content")
            record = json.loads(journal.read_text(encoding="utf-8"))
            preview = record["requestTail"][-1]["content"]
            self.assertTrue(preview.startswith("x" * 20))
            self.assertIn("+180 chars", preview)


class BrokerHTTPTest(unittest.TestCase):
    def setUp(self) -> None:
        self.previous_broker = broker_http.BROKER
        broker_http.BROKER = broker_state.Broker("agent-seat", "")

    def tearDown(self) -> None:
        broker_http.BROKER = self.previous_broker

    @staticmethod
    def handler_with_captured_chunks():
        handler = object.__new__(broker_http.Handler)
        chunks: list[bytes] = []
        handler._write_chunk = lambda data: chunks.append(data) or True
        handler._end_chunks = lambda: None
        return handler, chunks

    def test_sse_chunks_long_text_and_tool_arguments_without_loss(self) -> None:
        broker = broker_http.BROKER
        entry = broker.register(request_payload(), "stream")
        long_text = "한" * (broker_state.SSE_DELTA_CHARS + 7)
        long_args = "a" * (broker_state.SSE_DELTA_CHARS + 11)
        response = {
            "text": long_text,
            "tool_calls": [{"name": "exec", "arguments": long_args}],
        }
        handler, chunks = self.handler_with_captured_chunks()

        handler._emit_sse_response(entry, response)

        payloads = [
            json.loads(line.removeprefix("data: "))
            for chunk in chunks
            for line in chunk.decode("utf-8").splitlines()
            if line.startswith("data: {")
        ]
        text = "".join(
            choice["delta"].get("content", "")
            for payload in payloads
            for choice in payload["choices"]
        )
        arguments = "".join(
            call.get("function", {}).get("arguments", "")
            for payload in payloads
            for choice in payload["choices"]
            for call in choice["delta"].get("tool_calls", [])
        )
        self.assertEqual(text, long_text)
        self.assertEqual(arguments, long_args)
        self.assertEqual(chunks[-1], b"data: [DONE]\n\n")
        self.assertEqual(broker.history[-1]["status"], "done")

    def test_complete_hold_timeout_surfaces_permanent_provider_error(self) -> None:
        broker = broker_http.BROKER
        entry = broker.register(request_payload(stream=False), "complete")
        handler = object.__new__(broker_http.Handler)
        replies: list[tuple[int, dict]] = []
        handler._json = lambda code, body: replies.append((code, body))

        with mock.patch.object(broker_http, "COMPLETE_HOLD_MAX", 0):
            handler._hold_complete(entry)

        self.assertEqual(replies[0][0], 400)
        self.assertIn("puppet hold timeout", replies[0][1]["error"]["message"])
        self.assertEqual(broker.history[-1]["status"], "failed")
        ok, message = broker.respond(entry.id, {"text": "late"})
        self.assertFalse(ok)
        self.assertIn("already finished", message)

    def test_split_delta_round_trips_and_never_returns_empty_list(self) -> None:
        text = "z" * (broker_state.SSE_DELTA_CHARS * 2 + 3)
        pieces = broker_state.split_delta(text)
        self.assertEqual("".join(pieces), text)
        self.assertEqual([len(piece) for piece in pieces], [48_000, 48_000, 3])
        self.assertEqual(broker_state.split_delta(""), [""])

    def test_real_http_server_exposes_model_and_state_routes(self) -> None:
        server = broker_http.ThreadingHTTPServer(("127.0.0.1", 0), broker_http.Handler)
        server.daemon_threads = True
        thread = threading.Thread(target=server.serve_forever)
        thread.start()
        base = f"http://127.0.0.1:{server.server_address[1]}"
        try:
            with contextlib.redirect_stderr(io.StringIO()):
                with urllib.request.urlopen(base + "/v1/models", timeout=2) as response:
                    models = json.load(response)
                with urllib.request.urlopen(base + "/puppet/state", timeout=2) as response:
                    state = json.load(response)
                script = Path(__file__).with_name("puppet_broker.py")
                cli = subprocess.run(
                    [sys.executable, str(script), "pending"],
                    env={**os.environ, "DENEB_PUPPET_URL": base},
                    capture_output=True,
                    text=True,
                )
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)
        self.assertIn("coding-seat", {item["id"] for item in models["data"]})
        self.assertEqual(state["model"], "agent-seat")
        self.assertEqual(state["waiting"], 0)
        self.assertEqual(cli.returncode, 1)
        self.assertEqual(cli.stdout.strip(), "(no pending requests)")


class EntryPointContractTest(unittest.TestCase):
    def test_existing_cli_path_still_exposes_all_subcommands(self) -> None:
        script = Path(__file__).with_name("puppet_broker.py")
        proc = subprocess.run(
            [sys.executable, str(script), "--help"],
            check=True,
            capture_output=True,
            text=True,
        )
        for command in ("serve", "pending", "show", "reply", "fail", "history"):
            self.assertIn(command, proc.stdout)


if __name__ == "__main__":
    unittest.main()
