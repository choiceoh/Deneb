"""Behavior tests for the native-client HTTP transport used by live tooling."""

from __future__ import annotations

import asyncio
import io
import json
import os
import tempfile
import unittest
import urllib.error
from pathlib import Path
from unittest import mock

from test_support import JSONResponse, load_script

native = load_script("scripts/mock_native_client.py")


class CaptureContractTests(unittest.TestCase):
    def test_latency_first_token_and_usage_match_quality_runner_contract(self) -> None:
        capture = native.ChatCapture(
            start_time=10.0,
            end_time=10.375,
            deltas=[{"text": "first", "ts": 10.125}],
            token_usage_data={"inputTokens": 9, "outputTokens": 4},
        )
        self.assertAlmostEqual(capture.latency_ms, 375.0)
        self.assertAlmostEqual(capture.first_token_ms, 125.0)
        self.assertEqual(capture.token_usage, {"inputTokens": 9, "outputTokens": 4})

    def test_returns_zero_first_token_for_empty_capture_without_aliasing(self) -> None:
        first = native.ChatCapture()
        second = native.ChatCapture()
        first.events.append({"event": "one"})
        first.errors.append("failure")
        self.assertEqual(first.first_token_ms, 0)
        self.assertEqual(second.events, [])
        self.assertEqual(second.errors, [])


class ResolutionTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.home = self.root / "home"
        self.state = self.root / "state"
        (self.home / ".deneb").mkdir(parents=True)
        self.state.mkdir()

    def env(self, **values: str) -> dict[str, str]:
        base = {
            "HOME": str(self.home),
            "DENEB_LIVETEST_CLIENT_TOKEN": "",
            "DENEB_LIVETEST_STATE_DIR": "",
            "DENEB_LIVETEST_GW_URL": "",
        }
        base.update(values)
        return base

    def test_when_environment_token_has_precedence_and_is_trimmed(self) -> None:
        (self.state / "client_token").write_text("state-token\n", encoding="utf-8")
        (self.home / ".deneb/client_token").write_text("home-token\n", encoding="utf-8")
        with mock.patch.dict(
            os.environ,
            self.env(
                DENEB_LIVETEST_CLIENT_TOKEN="  environment-token  ",
                DENEB_LIVETEST_STATE_DIR=str(self.state),
            ),
            clear=True,
        ):
            self.assertEqual(native.resolve_client_token(), "environment-token")

    def test_state_token_precedes_home_then_missing_files_return_empty(self) -> None:
        state_token = self.state / "client_token"
        home_token = self.home / ".deneb/client_token"
        state_token.write_text(" state ", encoding="utf-8")
        home_token.write_text(" home ", encoding="utf-8")
        with mock.patch.dict(
            os.environ,
            self.env(DENEB_LIVETEST_STATE_DIR=str(self.state)),
            clear=True,
        ):
            self.assertEqual(native.resolve_client_token(), "state")
            state_token.unlink()
            self.assertEqual(native.resolve_client_token(), "home")
            home_token.unlink()
            self.assertEqual(native.resolve_client_token(), "")

    def test_unreadable_or_directory_token_candidate_falls_through(self) -> None:
        (self.state / "client_token").mkdir()
        (self.home / ".deneb/client_token").write_text("fallback\n", encoding="utf-8")
        with mock.patch.dict(
            os.environ,
            self.env(DENEB_LIVETEST_STATE_DIR=str(self.state)),
            clear=True,
        ):
            self.assertEqual(native.resolve_client_token(), "fallback")

    def test_gateway_base_prefers_override_and_normalizes_trailing_slashes(self) -> None:
        with mock.patch.dict(os.environ, self.env(), clear=True):
            self.assertEqual(native._gateway_base("10.0.0.5", 19000), "http://10.0.0.5:19000")
        with mock.patch.dict(
            os.environ,
            self.env(DENEB_LIVETEST_GW_URL="  https://gateway.example/v1///  "),
            clear=True,
        ):
            self.assertEqual(native._gateway_base("ignored", 1), "https://gateway.example/v1")


class HTTPHelperTests(unittest.TestCase):
    def test_http_get_json_passes_timeout_and_decodes_body(self) -> None:
        with mock.patch.object(
            native.urllib.request,
            "urlopen",
            return_value=JSONResponse({"status": "ok", "pool": 4}),
        ) as urlopen:
            actual = native._http_get_json("http://gateway/health", timeout=1.25)
        self.assertEqual(actual, {"status": "ok", "pool": 4})
        urlopen.assert_called_once_with("http://gateway/health", timeout=1.25)

    def test_rpc_request_has_stable_frame_headers_params_and_timeout(self) -> None:
        captured = {}

        def open_request(request, timeout):
            captured["request"] = request
            captured["timeout"] = timeout
            return JSONResponse({"ok": True, "payload": {"value": 7}})

        with mock.patch.object(native.time, "time", return_value=123.456):
            with mock.patch.object(native.urllib.request, "urlopen", side_effect=open_request):
                result = native._miniapp_rpc(
                    "http://gateway",
                    "secret",
                    "miniapp.items.list",
                    {"limit": 3},
                    9.5,
                )

        request = captured["request"]
        frame = json.loads(request.data)
        headers = {key.lower(): value for key, value in request.header_items()}
        self.assertEqual(request.full_url, "http://gateway/api/v1/miniapp/rpc")
        self.assertEqual(request.method, "POST")
        self.assertEqual(headers["content-type"], "application/json")
        self.assertEqual(headers[native.CLIENT_TOKEN_HEADER.lower()], "secret")
        self.assertEqual(captured["timeout"], 9.5)
        self.assertEqual(frame, {
            "type": "req",
            "id": "lt-123456",
            "method": "miniapp.items.list",
            "params": {"limit": 3},
        })
        self.assertEqual(result, {"ok": True, "payload": {"value": 7}, "error": None})

    def test_empty_params_are_omitted_and_nondict_payload_is_safely_empty(self) -> None:
        requests = []

        def open_request(request, timeout):
            requests.append((json.loads(request.data), timeout))
            return JSONResponse({"ok": True, "payload": ["not", "an", "object"]})

        with mock.patch.object(native.urllib.request, "urlopen", side_effect=open_request):
            result = native._miniapp_rpc("http://gateway", "token", "miniapp.ping", {}, 2)
        self.assertNotIn("params", requests[0][0])
        self.assertEqual(requests[0][1], 2)
        self.assertEqual(result, {"ok": True, "payload": {}, "error": None})

    def test_malformed_nonobject_rpc_json_returns_protocol_error(self) -> None:
        for body in ([], "unexpected", 7, None):
            with self.subTest(body=body):
                with mock.patch.object(
                    native.urllib.request,
                    "urlopen",
                    return_value=JSONResponse(body),
                ):
                    result = native._miniapp_rpc(
                        "http://gateway", "token", "miniapp.test", {}, 3
                    )

                self.assertEqual(
                    result,
                    {
                        "ok": False,
                        "payload": {},
                        "error": {
                            "message": "invalid RPC response: expected JSON object"
                        },
                    },
                )

    def test_protocol_and_nonframe_errors_are_normalized(self) -> None:
        bodies = [
            {"ok": False, "error": {"code": "invalid", "message": "bad request"}},
            {"error": "unauthorized"},
            {"ok": False},
        ]
        expected = [
            {"code": "invalid", "message": "bad request"},
            {"message": "unauthorized"},
            {"message": "rpc failed"},
        ]
        for body, error in zip(bodies, expected):
            with self.subTest(body=body):
                with mock.patch.object(
                    native.urllib.request,
                    "urlopen",
                    return_value=JSONResponse(body),
                ):
                    result = native._miniapp_rpc(
                        "http://gateway", "token", "miniapp.test", {}, 3
                    )
                self.assertFalse(result["ok"])
                self.assertEqual(result["payload"], {})
                self.assertEqual(result["error"], error)

    def test_http_error_includes_body_then_reason_and_transport_errors_do_not_raise(self) -> None:
        cases = [
            (
                urllib.error.HTTPError(
                    "http://gateway",
                    401,
                    "Unauthorized",
                    {},
                    io.BytesIO(b'{"error":"expired"}'),
                ),
                'HTTP 401: {"error":"expired"}',
            ),
            (TimeoutError("request timed out"), "request timed out"),
        ]
        for failure, message in cases:
            with self.subTest(failure=type(failure).__name__):
                with mock.patch.object(
                    native.urllib.request,
                    "urlopen",
                    side_effect=failure,
                ):
                    result = native._miniapp_rpc(
                        "http://gateway", "token", "miniapp.test", {}, 3
                    )
                self.assertEqual(result["error"]["message"], message)
                self.assertFalse(result["ok"])

    def test_http_error_with_unreadable_body_falls_back_to_reason(self) -> None:
        failure = urllib.error.HTTPError(
            "http://gateway",
            503,
            "Service Unavailable",
            {},
            None,
        )
        failure.read = mock.Mock(side_effect=OSError("body stream closed"))
        with mock.patch.object(
            native.urllib.request,
            "urlopen",
            side_effect=failure,
        ):
            result = native._miniapp_rpc(
                "http://gateway", "token", "miniapp.test", {}, 3
            )
        self.assertEqual(
            result,
            {
                "ok": False,
                "payload": {},
                "error": {"message": "HTTP 503: Service Unavailable"},
            },
        )
        failure.read.assert_called_once_with()


class PrerequisiteTests(unittest.TestCase):
    def test_unreachable_gateway_returns_actionable_start_hint_before_token_check(self) -> None:
        with mock.patch.object(native, "_gateway_base", return_value="http://dev:19000"):
            with mock.patch.object(native, "_http_get_json", side_effect=OSError("refused")):
                with mock.patch.object(native, "resolve_client_token") as token:
                    ok, detail = native.check_prerequisites("dev", 19000)
        self.assertFalse(ok)
        self.assertIn("http://dev:19000/health", detail)
        self.assertIn("live-test.sh start", detail)
        self.assertIn("refused", detail)
        token.assert_not_called()

    def test_when_reachable_gateway_requires_token_and_success_is_exact(self) -> None:
        with mock.patch.object(native, "_http_get_json", return_value={"status": "ok"}):
            with mock.patch.object(native, "resolve_client_token", return_value=""):
                missing = native.check_prerequisites()
            with mock.patch.object(native, "resolve_client_token", return_value="secret"):
                ready = native.check_prerequisites()
        self.assertFalse(missing[0])
        self.assertIn("no native client token found", missing[1])
        self.assertEqual(ready, (True, "ok"))


class NativeClientLifecycleTests(unittest.TestCase):
    def make_client(self, token="token"):
        env = {
            "DENEB_LIVETEST_CLIENT_TOKEN": token,
            "DENEB_LIVETEST_GW_URL": "http://dev-gateway:19999/",
        }
        with mock.patch.dict(os.environ, env, clear=False):
            return native.NativeTestClient(host="ignored", port=1, bot_username="legacy")

    def test_init_connect_disconnect_and_close_preserve_legacy_surface(self) -> None:
        client = self.make_client()
        self.assertEqual(client.base, "http://dev-gateway:19999")
        self.assertEqual(client.token, "token")
        self.assertTrue(client.session_key.startswith(f"client:lt-{os.getpid()}"))

        with mock.patch.object(
            native,
            "_http_get_json",
            return_value={"modelName": "local-model"},
        ):
            self.assertEqual(asyncio.run(client.connect()), "native:local-model")
        self.assertTrue(client._connected)
        asyncio.run(client.disconnect())
        self.assertFalse(client._connected)
        client._connected = True
        asyncio.run(client.close())
        self.assertFalse(client._connected)

    def test_connect_is_resilient_to_health_failure_and_refreshes_missing_token(self) -> None:
        client = self.make_client(token="")
        self.assertEqual(client.token, "")
        with mock.patch.object(native, "_http_get_json", side_effect=OSError("down")):
            with mock.patch.object(native, "resolve_client_token", return_value="late-token"):
                label = asyncio.run(client.connect())
        self.assertEqual(label, "native:unknown")
        self.assertEqual(client.token, "late-token")
        self.assertTrue(client._connected)

    def test_when_session_rotation_prefixing_and_counter_are_deterministic(self) -> None:
        client = self.make_client()
        client.set_chat_id(42)
        self.assertEqual(client.session_key, "client:lt-42")
        self.assertEqual(asyncio.run(client.create_session("named")), "client:named")
        self.assertEqual(
            asyncio.run(client.create_session("client:already")),
            "client:already",
        )
        with mock.patch.object(native.os, "getpid", return_value=321):
            native._session_counter = 0
            first = asyncio.run(client.create_session())
            second = asyncio.run(client.create_session("   "))
        self.assertEqual(first, "client:lt-321-1")
        self.assertEqual(second, "client:lt-321-2")

    def test_reset_session_sends_reset_with_bounded_timeout(self) -> None:
        client = self.make_client()
        client.chat = mock.AsyncMock(return_value=native.ChatCapture())
        asyncio.run(client.reset_session())
        client.chat.assert_awaited_once_with("/reset", timeout=30.0)


class NativeClientChatTests(unittest.TestCase):
    def make_client(self, token="secret"):
        with mock.patch.dict(
            os.environ,
            {
                "DENEB_LIVETEST_CLIENT_TOKEN": token,
                "DENEB_LIVETEST_GW_URL": "http://gateway",
            },
            clear=False,
        ):
            return native.NativeTestClient()

    def test_missing_token_returns_failed_capture_without_rpc(self) -> None:
        client = self.make_client(token="")
        with mock.patch.object(native, "resolve_client_token", return_value=""):
            with mock.patch.object(native, "_miniapp_rpc") as rpc:
                with mock.patch.object(native.time, "time", side_effect=[4.0, 4.025]):
                    capture = asyncio.run(client.chat("hello"))
        rpc.assert_not_called()
        self.assertEqual(capture.errors, ["no client token available"])
        self.assertEqual(capture.final_response, {"ok": False})
        self.assertAlmostEqual(capture.latency_ms, 25.0)

    def test_when_chat_refreshes_late_token_before_constructing_rpc_request(self) -> None:
        client = self.make_client(token="")
        response = {"ok": True, "payload": {"text": "ready"}}
        with mock.patch.object(native, "resolve_client_token", return_value="rotated-token"):
            with mock.patch.object(native, "_miniapp_rpc", return_value=response) as rpc:
                with mock.patch.object(native.time, "time", side_effect=[5.0, 5.1]):
                    capture = asyncio.run(client.chat("hello"))
        self.assertEqual(client.token, "rotated-token")
        self.assertEqual(capture.reply_text, "ready")
        self.assertEqual(rpc.call_args.args[1], "rotated-token")
        self.assertEqual(rpc.call_args.args[3]["sessionKey"], client.session_key)

    def test_successful_chat_preserves_reply_usage_timing_and_raw_metadata(self) -> None:
        client = self.make_client()
        response = {
            "ok": True,
            "payload": {
                "text": "안녕하세요",
                "usage": {"inputTokens": "12", "outputTokens": 5},
                "model": "main-model",
                "fellBack": False,
            },
        }
        with mock.patch.object(native, "_miniapp_rpc", return_value=response) as rpc:
            with mock.patch.object(native.time, "time", side_effect=[10.0, 10.4]):
                capture = asyncio.run(client.chat(
                    "질문",
                    timeout=17.5,
                    session_key="client:override",
                ))

        rpc.assert_called_once_with(
            "http://gateway",
            "secret",
            "miniapp.chat.send",
            {"message": "질문", "sessionKey": "client:override"},
            17.5,
        )
        self.assertEqual(capture.reply_text, "안녕하세요")
        self.assertEqual(capture.all_text, "안녕하세요")
        self.assertEqual(capture.final_messages, [{"text": "안녕하세요"}])
        self.assertEqual(capture.token_usage, {"inputTokens": 12, "outputTokens": 5})
        self.assertEqual(capture.deltas, [{"text": "안녕하세요", "ts": 10.4}])
        self.assertEqual(capture.events, [{"event": "chat.reply", "ts": 10.4}])
        self.assertEqual(capture.raw_events, [{
            "time": 10.4,
            "type": "miniapp.chat.send",
            "data": {"model": "main-model", "fellBack": False},
        }])
        self.assertAlmostEqual(capture.first_token_ms, 400.0)
        self.assertTrue(capture.final_response["ok"])

    def test_empty_reply_and_nonmapping_usage_do_not_synthesize_stream(self) -> None:
        client = self.make_client()
        response = {
            "ok": True,
            "payload": {"text": None, "usage": ["bad"], "model": None},
        }
        with mock.patch.object(native, "_miniapp_rpc", return_value=response):
            with mock.patch.object(native.time, "time", side_effect=[1.0, 1.1]):
                capture = asyncio.run(client.chat("empty"))
        self.assertEqual(capture.reply_text, "")
        self.assertEqual(capture.final_messages, [])
        self.assertEqual(capture.token_usage, {})
        self.assertEqual(capture.deltas, [])
        self.assertEqual(capture.events, [])
        self.assertEqual(len(capture.raw_events), 1)

    def test_rpc_failure_is_recorded_and_does_not_create_success_artifacts(self) -> None:
        client = self.make_client()
        response = {
            "ok": False,
            "payload": {},
            "error": {"code": "timeout", "message": "turn deadline exceeded"},
        }
        with mock.patch.object(native, "_miniapp_rpc", return_value=response):
            with mock.patch.object(native.time, "time", side_effect=[2.0, 2.2]):
                capture = asyncio.run(client.chat("slow"))
        self.assertEqual(capture.errors, ["turn deadline exceeded"])
        self.assertEqual(capture.reply_text, "")
        self.assertEqual(capture.events, [])
        self.assertEqual(capture.final_response, {
            "ok": False,
            "error": {"code": "timeout", "message": "turn deadline exceeded"},
        })


class NativeClientRPCTests(unittest.TestCase):
    def setUp(self) -> None:
        with mock.patch.dict(
            os.environ,
            {
                "DENEB_LIVETEST_CLIENT_TOKEN": "rpc-token",
                "DENEB_LIVETEST_GW_URL": "http://gateway",
            },
            clear=False,
        ):
            self.client = native.NativeTestClient()

    def test_health_rpc_maps_get_success_and_transport_failure(self) -> None:
        with mock.patch.object(native, "_http_get_json", return_value={"status": "ok"}) as get:
            success = asyncio.run(self.client.rpc("health"))
        self.assertEqual(success, {"ok": True, "payload": {"status": "ok"}})
        get.assert_called_once_with("http://gateway/health", timeout=5.0)

        with mock.patch.object(native, "_http_get_json", side_effect=TimeoutError("slow")):
            failure = asyncio.run(self.client.rpc("health"))
        self.assertEqual(failure, {"ok": False, "error": {"message": "slow"}})

    def test_miniapp_rpc_forwards_params_timeout_and_both_result_shapes(self) -> None:
        responses = [
            {"ok": True, "payload": {"items": [1]}, "error": None},
            {"ok": False, "payload": {}, "error": {"message": "denied"}},
        ]
        with mock.patch.object(native, "_miniapp_rpc", side_effect=responses) as rpc:
            success = asyncio.run(self.client.rpc(
                "miniapp.items.list", {"limit": 1}, timeout=8
            ))
            failure = asyncio.run(self.client.rpc("miniapp.items.delete"))
        self.assertEqual(success, {"ok": True, "payload": {"items": [1]}})
        self.assertEqual(failure, {"ok": False, "error": {"message": "denied"}})
        self.assertEqual(rpc.call_args_list, [
            mock.call(
                "http://gateway",
                "rpc-token",
                "miniapp.items.list",
                {"limit": 1},
                8,
            ),
            mock.call(
                "http://gateway",
                "rpc-token",
                "miniapp.items.delete",
                {},
                30.0,
            ),
        ])

    def test_nonminiapp_rpc_fails_explicitly_and_alias_stays_compatible(self) -> None:
        result = asyncio.run(self.client.rpc("agent.run", {"message": "no"}))
        self.assertFalse(result["ok"])
        self.assertIn("RPC not supported on native surface: agent.run", result["error"]["message"])
        self.assertIs(native.TelegramTestClient, native.NativeTestClient)


if __name__ == "__main__":
    unittest.main()
