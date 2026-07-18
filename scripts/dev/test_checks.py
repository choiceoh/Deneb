"""Boundary and external-validator tests for shared response checks."""

from __future__ import annotations

import subprocess
import unittest
from unittest import mock

from test_support import load_script

checks = load_script("scripts/dev/checks.py")


class FenceAndLanguageTests(unittest.TestCase):
    def test_strip_deneb_ui_fences_handles_case_and_unclosed_final_block(self) -> None:
        text = (
            "before\n"
            "```DENEB-UI\n<card><text>hidden</text></card>\n```\n"
            "middle\n"
            "``` deneb-ui\n<card>unfinished"
        )
        self.assertEqual(checks.strip_deneb_ui_fences(text), "before\n\nmiddle\n")

    def test_strip_deneb_ui_fences_preserves_other_code_blocks(self) -> None:
        text = "```python\nprint('visible')\n```"
        self.assertEqual(checks.strip_deneb_ui_fences(text), text)

    def test_language_check_ignores_code_and_accepts_no_alphabetic_content(self) -> None:
        passed, detail = checks.check_korean_response("1234 🎉\n```js\nРусский\n```")
        self.assertTrue(passed)
        self.assertEqual(detail, "no alphabetic content (ok)")

    def test_language_ratio_has_a_strict_seventy_percent_boundary(self) -> None:
        passed, detail = checks.check_korean_response("aaaaaaaяяя")
        self.assertFalse(passed)
        self.assertIn("70%", detail)

        passed, detail = checks.check_korean_response("aaaaaaaaяя")
        self.assertTrue(passed)
        self.assertIn("80%", detail)

    def test_when_language_check_counts_korean_and_english_together(self) -> None:
        passed, detail = checks.check_korean_response("진행 done 완료")
        self.assertTrue(passed)
        self.assertIn("ko=4", detail)
        self.assertIn("en=4", detail)


class MarkupAndDeliveryTests(unittest.TestCase):
    def test_every_internal_markup_signature_is_rejected(self) -> None:
        cases = {
            "<function=exec>": "function",
            "<thinking>secret</thinking>": "thinking",
            "<artifact id='x'>": "artifact",
            "[[reply_to:123]]": "reply",
            "MEDIA:/tmp/a.png": "MEDIA",
            "NO_REPLY": "NO_REPLY",
            "SILENT_REPLY": "SILENT_REPLY",
        }
        for text, expected in cases.items():
            with self.subTest(text=text):
                passed, detail = checks.check_no_leaked_markup(text)
                self.assertFalse(passed)
                self.assertIn(expected, detail)

        self.assertEqual(checks.check_no_leaked_markup("ordinary response"), (True, "clean"))

    def test_delivery_length_accepts_exact_limit_and_rejects_one_over(self) -> None:
        passed, detail = checks.check_telegram_safe("x" * 4096)
        self.assertTrue(passed)
        self.assertEqual(detail, "length=4096 chars")

        passed, detail = checks.check_telegram_safe("x" * 4097)
        self.assertFalse(passed)
        self.assertIn("exceeds 4096", detail)

    def test_when_delivery_html_balance_is_checked_only_in_prose(self) -> None:
        self.assertTrue(checks.check_telegram_safe("<b>safe</b>")[0])
        passed, detail = checks.check_telegram_safe("<b>broken")
        self.assertFalse(passed)
        self.assertIn("open=1, close=0", detail)

        card = "```deneb-ui\n<card><code>lenient card html</card>\n```"
        self.assertTrue(checks.check_telegram_safe(card)[0])

    def test_deneb_html_document_is_exempt_from_length_and_tag_balance(self) -> None:
        # A webpage-style answer legitimately exceeds the prose limit (the
        # gateway caps it separately at 96KB) and is a full HTML document.
        page = "요약\n```deneb-html\n<!doctype html><b>" + "가" * 5000 + "\n```"
        self.assertTrue(checks.check_telegram_safe(page)[0])

        # Prose around the document still counts against the limit.
        long_prose = "x" * 4097 + "\n```deneb-html\n<div>페이지</div>\n```"
        passed, detail = checks.check_telegram_safe(long_prose)
        self.assertFalse(passed)
        self.assertIn("exceeds 4096", detail)


class DenebUIValidatorTests(unittest.TestCase):
    def test_reply_without_card_does_not_spawn_validator(self) -> None:
        with mock.patch.object(checks.subprocess, "run") as run:
            self.assertEqual(checks.check_deneb_ui_valid("plain prose"), (True, "no deneb-ui card"))
        run.assert_not_called()

    def test_validator_success_uses_gateway_cli_contract(self) -> None:
        reply = "```deneb-ui\n<card/>\n```"
        completed = subprocess.CompletedProcess([], 0, stdout="ok", stderr="")
        with mock.patch.object(checks.subprocess, "run", return_value=completed) as run:
            self.assertEqual(checks.check_deneb_ui_valid(reply), (True, "card valid"))

        args, kwargs = run.call_args
        self.assertEqual(args[0], ["go", "run", "./cmd/denebui-check"])
        self.assertEqual(kwargs["input"], reply)
        self.assertEqual(kwargs["timeout"], 120)
        self.assertEqual(kwargs["cwd"].name, "gateway-go")
        self.assertFalse(kwargs["check"])

    def test_validator_failure_is_bounded_to_first_three_output_lines(self) -> None:
        completed = subprocess.CompletedProcess(
            [], 1, stdout="line one\nline two\nline three\nline four\n", stderr="ignored"
        )
        with mock.patch.object(checks.subprocess, "run", return_value=completed):
            passed, detail = checks.check_deneb_ui_valid("```deneb-ui\ninvalid\n```")
        self.assertFalse(passed)
        self.assertEqual(detail, "line one; line two; line three")

    def test_validator_uses_stderr_or_exit_code_when_stdout_is_empty(self) -> None:
        completed = subprocess.CompletedProcess([], 2, stdout="", stderr="bad card\nsecond")
        with mock.patch.object(checks.subprocess, "run", return_value=completed):
            self.assertEqual(
                checks.check_deneb_ui_valid("```deneb-ui\nbad\n```"),
                (False, "bad card; second"),
            )

        completed = subprocess.CompletedProcess([], 7, stdout="", stderr="")
        with mock.patch.object(checks.subprocess, "run", return_value=completed):
            self.assertEqual(
                checks.check_deneb_ui_valid("```deneb-ui\nbad\n```"),
                (False, "exit 7"),
            )

    def test_missing_go_skips_but_timeout_fails(self) -> None:
        card = "```deneb-ui\n<card/>\n```"
        with mock.patch.object(checks.subprocess, "run", side_effect=FileNotFoundError):
            self.assertEqual(checks.check_deneb_ui_valid(card), (True, "~ skipped (go unavailable)"))
        with mock.patch.object(
            checks.subprocess,
            "run",
            side_effect=subprocess.TimeoutExpired(["go"], 120),
        ):
            self.assertEqual(checks.check_deneb_ui_valid(card), (False, "denebui-check timed out"))


class SubstanceFillerAndLatencyTests(unittest.TestCase):
    def test_substance_failures_are_reported_in_stable_priority_order(self) -> None:
        self.assertEqual(checks.check_response_substance("   "), (False, "empty response"))
        self.assertEqual(checks.check_response_substance("short"), (False, "too short (5 chars)"))
        self.assertEqual(
            checks.check_response_substance("----------"),
            (False, "no meaningful content"),
        )
        self.assertEqual(checks.check_response_substance("abcde12345"), (True, "10 chars"))

    def test_when_substance_thresholds_are_configurable_and_inclusive(self) -> None:
        self.assertTrue(checks.check_response_substance("abc", min_chars=3, min_alpha=3)[0])
        self.assertFalse(checks.check_response_substance("abc", min_chars=4, min_alpha=3)[0])
        self.assertFalse(checks.check_response_substance("abc", min_chars=3, min_alpha=4)[0])

    def test_filler_detection_is_case_insensitive_and_start_anchored(self) -> None:
        for text in ("Great question — here is the answer", "certainly, done", "물론이죠. 진행할게요"):
            with self.subTest(text=text):
                passed, detail = checks.check_no_filler(text)
                self.assertFalse(passed)
                self.assertIn("starts with filler", detail)
        self.assertEqual(
            checks.check_no_filler("결론입니다. Great question이라는 표현은 피했습니다."),
            (True, "no filler detected"),
        )

    def test_latency_limit_is_inclusive_and_formats_consistently(self) -> None:
        self.assertEqual(checks.check_latency(250.4, 250.4), (True, "250ms (limit: 250ms)"))
        self.assertEqual(
            checks.check_latency(250.6, 250.4),
            (False, "251ms exceeds 250ms limit"),
        )


if __name__ == "__main__":
    unittest.main()
