"""API routing, score boundary, and CLI tests for the benchmark judge."""

from __future__ import annotations

import io
import json
import subprocess
import sys
import unittest
from unittest import mock

from test_support import JSONResponse, invoke_main, load_script

judge = load_script("scripts/dev/bench-judge.py")


class JSONParsingTests(unittest.TestCase):
    def test_strict_json_is_preferred_and_preserves_nested_values(self) -> None:
        raw = '  {"winner":"A","meta":{"source":"judge"}}  '
        self.assertEqual(
            judge._parse_json(raw),
            {"winner": "A", "meta": {"source": "judge"}},
        )

    def test_fenced_or_prefixed_simple_object_is_extracted(self) -> None:
        self.assertEqual(
            judge._parse_json('```json\n{"helpfulness":8}\n```'),
            {"helpfulness": 8},
        )
        self.assertEqual(
            judge._parse_json('analysis first {"winner":"tie"} trailing text'),
            {"winner": "tie"},
        )

    def test_first_valid_object_wins_and_invalid_text_returns_empty(self) -> None:
        self.assertEqual(
            judge._parse_json('{"winner":"A"} then {"winner":"B"}'),
            {"winner": "A"},
        )
        for raw in ("", "not json", "{broken}", "prefix {\"x\": } suffix"):
            with self.subTest(raw=raw):
                self.assertEqual(judge._parse_json(raw), {})


class TransportTests(unittest.TestCase):
    def test_anthropic_request_shape_headers_and_timeout(self) -> None:
        captured = {}

        def urlopen(request, timeout):
            captured["request"] = request
            captured["timeout"] = timeout
            return JSONResponse({"content": [{"text": '{"helpfulness":9}'}]})

        with mock.patch.object(judge.urllib.request, "urlopen", side_effect=urlopen):
            result = judge._call_anthropic(
                "system prompt",
                "user prompt",
                "secret-key",
                "judge-model",
                "https://judge.example",
            )

        self.assertEqual(result, '{"helpfulness":9}')
        request = captured["request"]
        self.assertEqual(request.full_url, "https://judge.example/v1/messages")
        self.assertEqual(request.get_method(), "POST")
        self.assertEqual(request.get_header("X-api-key"), "secret-key")
        self.assertEqual(request.get_header("Anthropic-version"), "2023-06-01")
        self.assertEqual(captured["timeout"], 30)
        payload = json.loads(request.data)
        self.assertEqual(payload["model"], "judge-model")
        self.assertEqual(payload["system"], "system prompt")
        self.assertEqual(payload["messages"], [{"role": "user", "content": "user prompt"}])
        self.assertEqual(payload["temperature"], 0.0)
        self.assertEqual(payload["max_tokens"], 256)

    def test_openai_compatible_request_shape_and_response_path(self) -> None:
        captured = {}

        def urlopen(request, timeout):
            captured["request"] = request
            captured["timeout"] = timeout
            return JSONResponse({"choices": [{"message": {"content": "answer"}}]})

        with mock.patch.object(judge.urllib.request, "urlopen", side_effect=urlopen):
            result = judge._call_openai_compat(
                "system",
                "user",
                "local-token",
                "local-model",
                "http://127.0.0.1:9000/v1",
            )

        self.assertEqual(result, "answer")
        request = captured["request"]
        self.assertEqual(request.full_url, "http://127.0.0.1:9000/v1/chat/completions")
        self.assertEqual(request.get_header("Authorization"), "Bearer local-token")
        self.assertEqual(captured["timeout"], 30)
        payload = json.loads(request.data)
        self.assertEqual(payload["messages"], [
            {"role": "system", "content": "system"},
            {"role": "user", "content": "user"},
        ])

    def test_route_selects_anthropic_or_openai_compat_from_api_base(self) -> None:
        with mock.patch.dict(judge.os.environ, {
            "JUDGE_API_KEY": "key",
            "JUDGE_MODEL": "model",
            "JUDGE_API_BASE": "https://api.anthropic.com",
        }, clear=True):
            with mock.patch.object(judge, "_call_anthropic", return_value="anthropic") as anthropic:
                with mock.patch.object(judge, "_call_openai_compat") as openai:
                    self.assertEqual(judge._call_judge("s", "u"), "anthropic")
        anthropic.assert_called_once_with("s", "u", "key", "model", "https://api.anthropic.com")
        openai.assert_not_called()

        with mock.patch.dict(judge.os.environ, {
            "ANTHROPIC_API_KEY": "fallback-key",
            "JUDGE_API_BASE": "http://localhost:8000/v1",
        }, clear=True):
            with mock.patch.object(judge, "_call_openai_compat", return_value="local") as openai:
                self.assertEqual(judge._call_judge("sys", "usr"), "local")
        openai.assert_called_once_with(
            "sys",
            "usr",
            "fallback-key",
            "claude-haiku-4-5-20251001",
            "http://localhost:8000/v1",
        )

    def test_missing_key_and_network_timeout_are_not_silently_rewritten_by_transport(self) -> None:
        with mock.patch.dict(judge.os.environ, {}, clear=True):
            with self.assertRaisesRegex(RuntimeError, "No JUDGE_API_KEY"):
                judge._call_judge("system", "user")

        with mock.patch.object(judge.urllib.request, "urlopen", side_effect=TimeoutError("slow")):
            with self.assertRaisesRegex(TimeoutError, "slow"):
                judge._call_anthropic("s", "u", "key", "model", "https://api")


class AbsoluteScoringTests(unittest.TestCase):
    def test_availability_accepts_either_key_and_rejects_empty_values(self) -> None:
        for env, expected in (
            ({}, False),
            ({"JUDGE_API_KEY": ""}, False),
            ({"JUDGE_API_KEY": "judge"}, True),
            ({"ANTHROPIC_API_KEY": "anthropic"}, True),
        ):
            with self.subTest(env=env):
                with mock.patch.dict(judge.os.environ, env, clear=True):
                    self.assertEqual(judge.judge_available(), expected)

    def test_dimension_values_are_defaulted_integer_converted_and_clamped(self) -> None:
        raw = json.dumps({
            "helpfulness": -4,
            "accuracy": 12,
            "korean_quality": "7",
            "completeness": 9.9,
        })
        with mock.patch.object(judge, "_call_judge", return_value=raw) as call:
            result = judge.judge_absolute("질문", "답변", "exec succeeded")

        self.assertEqual(result, {
            "helpfulness": 0,
            "accuracy": 10,
            "korean_quality": 7,
            "tool_usage": 5,
            "completeness": 9,
        })
        system, prompt = call.call_args.args
        self.assertEqual(system, judge.JUDGE_SYSTEM_ABSOLUTE)
        self.assertIn("## User Message\n질문", prompt)
        self.assertIn("## Assistant Response\n답변", prompt)
        self.assertIn("## Tool Usage\nexec succeeded", prompt)

    def test_malformed_value_or_transport_error_returns_fresh_neutral_defaults(self) -> None:
        stderr = io.StringIO()
        with mock.patch.object(judge, "_call_judge", return_value='{"helpfulness":null}'):
            with mock.patch.object(judge.sys, "stderr", stderr):
                first = judge.judge_absolute("q", "a")
        self.assertEqual(first, judge._DEFAULT_SCORES)
        self.assertIn("judge error", stderr.getvalue())

        first["accuracy"] = 0
        with mock.patch.object(judge, "_call_judge", side_effect=TimeoutError("slow judge")):
            with mock.patch.object(judge.sys, "stderr", io.StringIO()):
                second = judge.judge_absolute("q", "a")
        self.assertEqual(second, judge._DEFAULT_SCORES)
        self.assertEqual(second["accuracy"], 5)

    def test_overall_score_maps_all_zero_to_zero_and_all_ten_to_hundred(self) -> None:
        for value, expected in ((0, 0.0), (10, 100.0)):
            with self.subTest(value=value):
                scores = {dimension: value for dimension in judge._DIMENSIONS}
                with mock.patch.object(judge, "judge_absolute", return_value=scores):
                    self.assertEqual(judge.judge_absolute_score("q", "a"), expected)


class PairwiseScoringTests(unittest.TestCase):
    def test_valid_pairwise_result_clamps_confidence_and_preserves_reason(self) -> None:
        with mock.patch.object(
            judge,
            "_call_judge",
            return_value='{"winner":"B","confidence":1.7,"reason":"B is complete"}',
        ) as call:
            result = judge.judge_pairwise("question", "answer A", "answer B")
        self.assertEqual(result, {"winner": "B", "confidence": 1.0, "reason": "B is complete"})
        self.assertEqual(call.call_args.args[0], judge.JUDGE_SYSTEM_PAIRWISE)
        prompt = call.call_args.args[1]
        self.assertIn("## Response A\nanswer A", prompt)
        self.assertIn("## Response B\nanswer B", prompt)

    def test_invalid_winner_defaults_to_tie_and_missing_fields_are_stable(self) -> None:
        with mock.patch.object(judge, "_call_judge", return_value='{"winner":"C"}'):
            self.assertEqual(
                judge.judge_pairwise("q", "a", "b"),
                {"winner": "tie", "confidence": 0.5, "reason": ""},
            )

    def test_invalid_confidence_or_api_failure_returns_explicit_error_tie(self) -> None:
        for failure in ('{"winner":"A","confidence":"high"}', TimeoutError("slow")):
            with self.subTest(failure=repr(failure)):
                kwargs = {"return_value": failure} if isinstance(failure, str) else {"side_effect": failure}
                with mock.patch.object(judge, "_call_judge", **kwargs):
                    with mock.patch.object(judge.sys, "stderr", io.StringIO()):
                        result = judge.judge_pairwise("q", "a", "b")
                self.assertEqual(result["winner"], "tie")
                self.assertEqual(result["confidence"], 0.0)
                self.assertTrue(result["reason"].startswith("error:"))


class CLIContractTests(unittest.TestCase):
    def test_check_mode_exit_and_output_follow_availability(self) -> None:
        for available, expected_rc, marker in (
            (True, 0, "yes"),
            (False, 1, "no"),
        ):
            with self.subTest(available=available):
                with mock.patch.object(judge, "judge_available", return_value=available):
                    rc, stdout, stderr = invoke_main(judge, ["check"])
                self.assertEqual((rc, stderr), (expected_rc, ""))
                self.assertEqual(stdout.strip(), f"judge_available={marker}")

    def test_missing_key_absolute_mode_has_machine_metric_and_exit_one(self) -> None:
        rc, stdout, stderr = invoke_main(
            judge,
            ["absolute", "--message", "q", "--response", "a"],
            env={},
            clear_env=True,
        )
        self.assertEqual(rc, 1)
        self.assertEqual(stdout.strip(), "metric_value=0")
        self.assertIn("No JUDGE_API_KEY", stderr)

    def test_absolute_cli_emits_deterministic_metric_and_dimension_order(self) -> None:
        scores = {
            "helpfulness": 8,
            "accuracy": 9,
            "korean_quality": 7,
            "tool_usage": 6,
            "completeness": 10,
        }
        with mock.patch.object(judge, "judge_available", return_value=True):
            with mock.patch.object(judge, "judge_absolute", return_value=scores) as absolute:
                rc, stdout, stderr = invoke_main(
                    judge,
                    ["absolute", "--message", "q", "--response", "a", "--tools", "exec"],
                )
        self.assertIsNone(rc)
        self.assertEqual(stderr, "")
        self.assertEqual(stdout.splitlines(), [
            "metric_value=80",
            "DENEB_JUDGE_DETAIL helpfulness=8 accuracy=9 korean_quality=7 "
            "tool_usage=6 completeness=10 overall=80",
        ])
        absolute.assert_called_once_with("q", "a", "exec")

    def test_pairwise_cli_maps_winner_to_fixed_metric(self) -> None:
        for winner, expected in (("A", 100), ("tie", 50), ("B", 0)):
            with self.subTest(winner=winner):
                result = {"winner": winner, "confidence": 0.75, "reason": "fixture"}
                with mock.patch.object(judge, "judge_available", return_value=True):
                    with mock.patch.object(judge, "judge_pairwise", return_value=result):
                        rc, stdout, stderr = invoke_main(
                            judge,
                            ["pairwise", "--message", "q", "--response-a", "a", "--response-b", "b"],
                        )
                self.assertIsNone(rc)
                self.assertEqual(stderr, "")
                self.assertEqual(stdout.splitlines()[0], f"metric_value={expected}")
                self.assertIn("confidence=0.75 reason=fixture", stdout)

    def test_real_help_entrypoint_lists_all_modes(self) -> None:
        proc = subprocess.run(
            [sys.executable, judge.__file__, "--help"],
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(proc.returncode, 0)
        for mode in ("absolute", "pairwise", "check"):
            self.assertIn(mode, proc.stdout)


if __name__ == "__main__":
    unittest.main()
