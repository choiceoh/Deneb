"""Tests for the single-writer RSI L4 dispatch RPC client."""

from __future__ import annotations

import io
import json
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import mock

import self_correction_dispatch as dispatch


class FakeResponse:
    def __init__(self, payload: dict):
        self.payload = payload

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def read(self) -> bytes:
        return json.dumps(self.payload).encode()


class SelfCorrectionDispatchTest(unittest.TestCase):
    def test_when_record_uses_authenticated_dispatch_rpc(self):
        with TemporaryDirectory() as td:
            Path(td, "client_token").write_text("secret-token\n", encoding="utf-8")
            seen = {}

            def urlopen(request, timeout):
                seen["request"] = request
                seen["timeout"] = timeout
                return FakeResponse({"ok": True, "payload": {"ok": True}})

            with mock.patch.object(dispatch.urllib.request, "urlopen", side_effect=urlopen):
                rc = dispatch.main([
                    "--state-dir", td, "record", "--id", "sc-1", "--phase", "started",
                    "--attempt-id", "attempt-1", "--branch", "dispatch/sc-1",
                ])
            self.assertEqual(rc, 0)
            request = seen["request"]
            self.assertEqual(request.get_header("X-deneb-client-token"), "secret-token")
            body = json.loads(request.data)
            self.assertEqual(body["method"], "miniapp.self_improvement_coding.dispatch")
            self.assertEqual(body["params"]["attemptId"], "attempt-1")

    def test_when_list_filters_phases_and_prints_stable_fields(self):
        response = FakeResponse({
            "ok": True,
            "payload": {"candidates": [
                {"id": "sc-1", "attemptId": "a1", "dispatchPhase": "merged", "commitSha": "sha1"},
                {"id": "sc-2", "attemptId": "a2", "dispatchPhase": "failed"},
            ]},
        })
        out = io.StringIO()
        with (
            mock.patch.object(dispatch.urllib.request, "urlopen", return_value=response),
            redirect_stdout(out),
        ):
            rc = dispatch.main(["list", "--phase", "merged"])
        self.assertEqual(rc, 0)
        self.assertEqual(out.getvalue(), "sc-1\ta1\tmerged\tsha1\t\n")

    def test_list_json_preserves_authoritative_dispatch_fields(self):
        response = FakeResponse({
            "ok": True,
            "payload": {"candidates": [
                {"id": "sc-1", "attemptId": "a1", "dispatchPhase": "started", "branch": "dispatch/a1"},
                {"id": "sc-2", "attemptId": "a2", "dispatchPhase": "failed"},
            ]},
        })
        out = io.StringIO()
        with (
            mock.patch.object(dispatch.urllib.request, "urlopen", return_value=response),
            redirect_stdout(out),
        ):
            rc = dispatch.main(["list", "--phase", "started", "--json"])
        self.assertEqual(rc, 0)
        self.assertEqual(json.loads(out.getvalue()), [{
            "id": "sc-1", "attemptId": "a1", "dispatchPhase": "started", "branch": "dispatch/a1",
        }])


if __name__ == "__main__":
    unittest.main()
