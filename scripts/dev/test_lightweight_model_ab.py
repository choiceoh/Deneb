"""Behavior tests for the split lightweight-model A/B harness."""

from __future__ import annotations

import contextlib
import io
import sys
import unittest
import urllib.error
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))

import lightweight_model_ab_battery as battery
import lightweight_model_ab_runner as runner
import lightweight_model_ab_transport as transport


class ScoringContractTest(unittest.TestCase):
    def test_good_vectors_preserves_full_scores(self) -> None:
        self.assertEqual(
            battery.score_compaction(
                battery.COMPACTION_CASES[0],
                runner.MOCK_GOOD["compaction-deal"],
            ),
            100.0,
        )
        self.assertEqual(
            battery.score_extract(
                battery.EXTRACT_CASES[0],
                runner.MOCK_GOOD["extract-quote"],
            ),
            100.0,
        )
        self.assertEqual(
            battery.score_verdict(
                next(c for c in battery.VERDICT_CASES if c["name"] == "judge-done"),
                runner.MOCK_GOOD["judge-done"],
            ),
            100.0,
        )

    def test_when_cross_case_contamination_is_penalized(self) -> None:
        case = battery.COMPACTION_CASES[0]
        clean = runner.MOCK_GOOD["compaction-deal"]
        contaminated = clean + "\n서버 포트 18789와 srv2도 확인"
        self.assertLess(
            battery.score_compaction(case, contaminated),
            battery.score_compaction(case, clean),
        )

    def test_prompt_builders_preserve_production_shapes(self) -> None:
        system, user = battery.verdict_prompt(
            next(c for c in battery.VERDICT_CASES if c["name"] == "judge-done")
        )
        self.assertIn("strict judge", system)
        self.assertIn("Goal:\n", user)
        self.assertIn("Agent's most recent response", user)
        triage_system, triage_user = battery.triage_prompt(battery.TRIAGE_CASES[0])
        self.assertIn("YES 또는 NO", triage_system)
        self.assertEqual(triage_user.splitlines()[0], "앱: Gmail")


class TransportTest(unittest.TestCase):
    def test_thinking_off_model_family_contract(self) -> None:
        self.assertEqual(
            transport.thinking_off_extra_body("deepseek-v4"),
            {"chat_template_kwargs": {"thinking": False}},
        )
        self.assertIsNone(transport.thinking_off_extra_body("deepseek-r1"))
        self.assertEqual(
            transport.thinking_off_extra_body("qwen-instruct"),
            {"chat_template_kwargs": {"enable_thinking": False}},
        )

    def test_response_format_rejection_falls_back_and_surfaces_flag(self) -> None:
        calls: list[object] = []

        def fake_chat_once(*args, **kwargs):
            response_format = args[7]
            calls.append(response_format)
            if response_format is not None:
                raise urllib.error.HTTPError("http://local", 400, "bad format", {}, None)
            return '{"ok":true}', 4

        with mock.patch.object(transport, "chat_once", side_effect=fake_chat_once):
            with contextlib.redirect_stderr(io.StringIO()):
                content, _latency, tokens, rejected = transport.chat_with_retry(
                    "http://local/v1",
                    "",
                    "candidate",
                    "system",
                    "user",
                    20,
                    1,
                    {"type": "json_object"},
                )
        self.assertEqual(content, '{"ok":true}')
        self.assertEqual(tokens, 4)
        self.assertTrue(rejected)
        self.assertEqual(calls, [{"type": "json_object"}, None])

    def test_timeout_retries_once_without_hiding_failure_time(self) -> None:
        with mock.patch.object(
            transport,
            "chat_once",
            side_effect=[TimeoutError("slow"), ("recovered", 3)],
        ) as chat_once:
            with mock.patch.object(transport.time, "sleep") as sleep:
                with contextlib.redirect_stderr(io.StringIO()):
                    content, latency, tokens, rejected = transport.chat_with_retry(
                        "http://local/v1", "", "candidate", "s", "u", 20, 1
                    )
        self.assertEqual((content, tokens, rejected), ("recovered", 3, False))
        self.assertGreaterEqual(latency, 0)
        self.assertEqual(chat_once.call_count, 2)
        sleep.assert_called_once_with(2)


class MockEndToEndTest(unittest.TestCase):
    def test_when_builtin_mock_battery_still_passes(self) -> None:
        stdout = io.StringIO()
        stderr = io.StringIO()
        with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            rc = runner.run_mock()
        self.assertEqual(rc, 0, stdout.getvalue() + stderr.getvalue())
        self.assertIn("MOCK_SELFTEST PASS", stdout.getvalue())
        self.assertIn("json_mode=rejected", stdout.getvalue())


if __name__ == "__main__":
    unittest.main()
