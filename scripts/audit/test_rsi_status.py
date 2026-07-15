"""Tests for the RPC-backed RSI status formatter."""

from __future__ import annotations

import io
import json
import tempfile
import unittest
import urllib.error
from pathlib import Path
from unittest import mock

import rsi_status


NOW = 1_700_000_000_000


def sample_payload() -> dict:
    return {
        "turning": 1,
        "health": {"evolves7d": 2, "thrash": False},
        "layers": [
            {
                "key": "L1",
                "title": "skill evolution",
                "state": "LIVE",
                "diagnosis": "turning | safely",
                "detail": "skill policy",
                "metrics": [{"label": "evolved", "value": "2"}],
            },
            {
                "key": "GRAD",
                "title": "graduation ladder",
                "state": "DATA-GATED",
                "diagnosis": "evidence accumulating",
                "metrics": [],
            },
        ],
    }


class FakeResponse:
    def __init__(self, payload: dict):
        self.payload = payload

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def read(self, size: int = -1) -> bytes:
        body = json.dumps(self.payload).encode()
        return body if size < 0 else body[:size]


class SnapshotTest(unittest.TestCase):
    def test_projection_uses_server_turning_and_preformatted_metrics(self):
        snapshot = rsi_status.snapshot_from_payload(sample_payload())
        self.assertEqual(snapshot.turning, 1)
        self.assertEqual(snapshot.layers[0].metrics, {"evolved": "2"})
        self.assertEqual(snapshot.health["evolves7d"], 2)

    def test_projection_rejects_unknown_state(self):
        payload = sample_payload()
        payload["layers"][0]["state"] = "MAYBE"
        with self.assertRaisesRegex(rsi_status.StatusError, "unknown state"):
            rsi_status.snapshot_from_payload(payload)

    def test_projection_rejects_duplicate_metric_labels(self):
        payload = sample_payload()
        payload["layers"][0]["metrics"] *= 2
        with self.assertRaisesRegex(rsi_status.StatusError, "duplicate metric"):
            rsi_status.snapshot_from_payload(payload)

    def test_projection_rejects_duplicate_layers(self):
        payload = sample_payload()
        payload["layers"].append(dict(payload["layers"][0]))
        with self.assertRaisesRegex(rsi_status.StatusError, "duplicate layer"):
            rsi_status.snapshot_from_payload(payload)


class TransportTest(unittest.TestCase):
    def test_fetch_uses_authenticated_canonical_rpc(self):
        seen = {}

        def urlopen(request, timeout):
            seen["request"] = request
            seen["timeout"] = timeout
            return FakeResponse({"ok": True, "payload": sample_payload()})

        with mock.patch.object(rsi_status.urllib.request, "urlopen", side_effect=urlopen):
            snapshot = rsi_status.fetch_status("http://gateway.test/", "secret", 3)
        self.assertEqual(snapshot.turning, 1)
        self.assertEqual(seen["timeout"], 3)
        self.assertEqual(seen["request"].get_header("X-deneb-client-token"), "secret")
        body = json.loads(seen["request"].data)
        self.assertEqual(body["method"], "miniapp.rsi.status")

    def test_fetch_surfaces_transport_failure(self):
        with (
            mock.patch.object(
                rsi_status.urllib.request,
                "urlopen",
                side_effect=urllib.error.URLError("offline"),
            ),
            self.assertRaisesRegex(rsi_status.StatusError, "unavailable"),
        ):
            rsi_status.fetch_status("http://gateway.test", "")

    def test_fetch_rejects_oversize_response(self):
        response = mock.MagicMock()
        response.__enter__.return_value = response
        response.__exit__.return_value = False
        response.read.return_value = b"x" * (rsi_status.MAX_RESPONSE_BYTES + 1)
        with (
            mock.patch.object(rsi_status.urllib.request, "urlopen", return_value=response),
            self.assertRaisesRegex(rsi_status.StatusError, "response exceeds"),
        ):
            rsi_status.fetch_status("http://gateway.test", "")
        response.read.assert_called_once_with(rsi_status.MAX_RESPONSE_BYTES + 1)


class RenderingTest(unittest.TestCase):
    def setUp(self):
        self.snapshot = rsi_status.snapshot_from_payload(sample_payload())

    def test_markdown_uses_server_snapshot_and_excludes_grad_from_denominator(self):
        doc = rsi_status.render_markdown(self.snapshot, NOW, "http://gateway.test/")
        self.assertIn("**Turning: 1/1**", doc)
        self.assertIn("| L1 — skill evolution | LIVE | turning \\| safely |", doc)
        self.assertIn('"evolved": "2"', doc)
        self.assertIn('"evolves7d": 2', doc)
        self.assertIn("via `miniapp.rsi.status`", doc)

    def test_json_mode_is_machine_readable_without_status_suffix(self):
        out = io.StringIO()
        with mock.patch.object(rsi_status, "fetch_status", return_value=self.snapshot):
            rc = rsi_status.main(["--json"], stdout=out, stderr=io.StringIO())
        self.assertEqual(rc, 0)
        self.assertEqual(json.loads(out.getvalue())["turning"], 1)

    def test_cli_atomically_writes_markdown(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            output = Path(temp_dir) / "generated" / "rsi-status.md"
            with mock.patch.object(rsi_status, "fetch_status", return_value=self.snapshot):
                rc = rsi_status.main(
                    ["--now-ms", str(NOW), "--write-markdown", str(output)],
                    stdout=io.StringIO(),
                    stderr=io.StringIO(),
                )
            self.assertEqual(rc, 0)
            self.assertIn("# Deneb RSI live status", output.read_text(encoding="utf-8"))
            self.assertFalse(output.with_name(output.name + ".tmp").exists())

    def test_cli_fails_closed_when_gateway_is_unavailable(self):
        err = io.StringIO()
        with mock.patch.object(
            rsi_status,
            "fetch_status",
            side_effect=rsi_status.StatusError("offline"),
        ):
            rc = rsi_status.main([], stdout=io.StringIO(), stderr=err)
        self.assertEqual(rc, 2)
        self.assertIn("offline", err.getvalue())


if __name__ == "__main__":
    unittest.main()
