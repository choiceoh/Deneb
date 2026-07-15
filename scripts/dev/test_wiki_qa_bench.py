"""Wire, matching, retry, and score-output tests for the Wiki QA bench."""

from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from test_support import JSONResponse, invoke_main, load_script

wiki_qa = load_script("scripts/dev/wiki-qa-bench.py")


class RPCWireTests(unittest.TestCase):
    def test_rpc_posts_stable_frame_headers_url_and_timeout(self) -> None:
        captured = {}

        def urlopen(request, timeout):
            captured["request"] = request
            captured["timeout"] = timeout
            return JSONResponse({"ok": True, "payload": {"value": 3}})

        with mock.patch.object(wiki_qa.urllib.request, "urlopen", side_effect=urlopen):
            result = wiki_qa.rpc(
                "http://gateway.example/",
                "client-token",
                "miniapp.memory.search",
                {"query": "hello", "limit": 8},
                17,
            )

        self.assertEqual(result, {"ok": True, "payload": {"value": 3}})
        request = captured["request"]
        self.assertEqual(request.full_url, "http://gateway.example/api/v1/miniapp/rpc")
        self.assertEqual(request.get_method(), "POST")
        self.assertEqual(request.get_header("Content-type"), "application/json")
        self.assertEqual(request.get_header("X-deneb-client-token"), "client-token")
        self.assertEqual(captured["timeout"], 17)
        self.assertEqual(json.loads(request.data), {
            "id": "qa",
            "method": "miniapp.memory.search",
            "params": {"query": "hello", "limit": 8},
        })

    def test_http_200_rpc_error_shapes_raise_instead_of_scoring_as_miss(self) -> None:
        failures = [
            {"ok": False, "error": "permission denied"},
            {"error": {"code": "BROKEN", "message": "failed"}},
        ]
        for payload in failures:
            with self.subTest(payload=payload):
                with mock.patch.object(
                    wiki_qa.urllib.request,
                    "urlopen",
                    return_value=JSONResponse(payload),
                ):
                    with self.assertRaisesRegex(RuntimeError, "rpc miniapp.test"):
                        wiki_qa.rpc("http://gw", "token", "miniapp.test", {}, 1)

    def test_payload_bearing_frame_is_returned_even_when_it_has_error_metadata(self) -> None:
        payload = {"ok": True, "error": "warning", "payload": {"results": []}}
        with mock.patch.object(
            wiki_qa.urllib.request,
            "urlopen",
            return_value=JSONResponse(payload),
        ):
            self.assertEqual(wiki_qa.rpc("http://gw", "t", "method", {}, 1), payload)


class MatchingBoundaryTests(unittest.TestCase):
    def test_when_digit_relaxation_removes_only_commas_and_whitespace(self) -> None:
        self.assertEqual(wiki_qa.digits_relaxed(" 1, 068\tMW "), "1068MW")
        self.assertEqual(wiki_qa.digits_relaxed("1.068-MW"), "1.068-MW")

    def test_when_digit_match_requires_both_boundaries_and_checks_later_occurrences(self) -> None:
        self.assertTrue(wiki_qa.digits_bounded("x1068y", "1068"))
        self.assertFalse(wiki_qa.digits_bounded("11068", "1068"))
        self.assertFalse(wiki_qa.digits_bounded("10680", "1068"))
        self.assertTrue(wiki_qa.digits_bounded("11068and1068MW", "1068"))

    def test_when_contains_supports_casefold_alternatives_and_relaxed_numbers(self) -> None:
        self.assertTrue(wiki_qa.contains("완료일은 6월 2일입니다", "6/2|6월 2일"))
        self.assertTrue(wiki_qa.contains("Project ALPHA finished", "alpha"))
        self.assertTrue(wiki_qa.contains("용량 1 068 MW", "1,068"))
        self.assertFalse(wiki_qa.contains("용량 11,068 MW", "1,068"))
        self.assertFalse(wiki_qa.contains("unrelated", " |  | "))

    def test_path_match_starts_only_at_segment_boundary_and_ignores_md_suffix(self) -> None:
        self.assertTrue(wiki_qa.path_hit("업무/영덕.md", "업무/영덕-구/현황.md"))
        self.assertTrue(wiki_qa.path_hit("영덕", "업무/영덕/현황"))
        self.assertFalse(wiki_qa.path_hit("영덕", "업무/남영덕/현황.md"))
        self.assertFalse(wiki_qa.path_hit("", "업무/영덕.md"))


class ScoringFunctionTests(unittest.TestCase):
    def test_when_recall_uses_question_limit_top_k_and_segment_matching(self) -> None:
        response = {
            "payload": {
                "results": [
                    {"path": "업무/남영덕/현황.md"},
                    {"path": "업무/영덕/현황.md"},
                    {"path": "업무/ignored-after-k.md"},
                ]
            }
        }
        case = {"question": "영덕 현황?", "gold_paths": ["영덕"]}
        with mock.patch.object(wiki_qa, "rpc", return_value=response) as rpc:
            hit, paths = wiki_qa.score_recall(case, "http://gw", "token", 2, 9)
        self.assertTrue(hit)
        self.assertEqual(paths, ["업무/남영덕/현황.md", "업무/영덕/현황.md"])
        rpc.assert_called_once_with(
            "http://gw",
            "token",
            "miniapp.memory.search",
            {"query": "영덕 현황?", "limit": 2},
            9,
        )

    def test_recall_missing_payload_is_a_clean_miss(self) -> None:
        with mock.patch.object(wiki_qa, "rpc", return_value={}):
            self.assertEqual(
                wiki_qa.score_recall({"question": "q", "gold_paths": ["x"]}, "gw", "t", 8, 1),
                (False, []),
            )

    def test_answer_without_positive_or_negative_needles_is_ungraded(self) -> None:
        with mock.patch.object(wiki_qa, "rpc") as rpc:
            result = wiki_qa.score_answer({"question": "q"}, "gw", "t", "session", 1)
        self.assertEqual(result, (None, 0, ""))
        rpc.assert_not_called()

    def test_valid_answer_runs_one_reset_turn_and_returns_latency(self) -> None:
        case = {"question": "capacity?", "must_contain": ["1,068"], "must_not": ["unknown"]}
        response = {"payload": {"text": "총 1 068 MW입니다"}}
        with mock.patch.object(wiki_qa, "rpc", side_effect=[{}, response]) as rpc:
            with mock.patch.object(wiki_qa.time, "sleep") as sleep:
                with mock.patch.object(wiki_qa.time, "time", side_effect=[10.0, 10.321]):
                    ok, elapsed, text = wiki_qa.score_answer(case, "gw", "token", "session", 5)
        self.assertTrue(ok)
        self.assertEqual(elapsed, 320)
        self.assertEqual(text, "총 1 068 MW입니다")
        self.assertEqual(rpc.call_count, 2)
        sleep.assert_called_once_with(1.5)

    def test_reset_error_retries_once_then_accepts_second_answer(self) -> None:
        case = {"question": "date?", "must_contain": ["6/2|6월 2일"]}
        response = {"payload": {"text": "일정은 6월 2일입니다"}}
        with mock.patch.object(
            wiki_qa,
            "rpc",
            side_effect=[RuntimeError("busy"), {}, response],
        ) as rpc:
            with mock.patch.object(wiki_qa.time, "sleep") as sleep:
                with mock.patch.object(wiki_qa.time, "time", side_effect=[20.0, 20.2]):
                    result = wiki_qa.score_answer(case, "gw", "token", "session", 5)
        self.assertEqual(result, (True, 199, "일정은 6월 2일입니다"))
        self.assertEqual(rpc.call_count, 3)
        self.assertEqual([call.args[0] for call in sleep.call_args_list], [4, 1.5])

    def test_empty_first_answer_retries_but_prohibited_needle_still_fails(self) -> None:
        case = {"question": "status?", "must_contain": ["done"], "must_not": ["cancelled"]}
        empty = {"payload": {"text": ""}}
        prohibited = {"payload": {"text": "done but cancelled"}}
        with mock.patch.object(wiki_qa, "rpc", side_effect=[{}, empty, {}, prohibited]):
            with mock.patch.object(wiki_qa.time, "sleep") as sleep:
                with mock.patch.object(wiki_qa.time, "time", side_effect=[1.0, 1.1, 2.0, 2.3]):
                    ok, elapsed, text = wiki_qa.score_answer(case, "gw", "t", "s", 5)
        self.assertFalse(ok)
        self.assertEqual(elapsed, 299)
        self.assertEqual(text, "done but cancelled")
        self.assertEqual([call.args[0] for call in sleep.call_args_list], [1.5, 4, 1.5])


class MainOutputTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        root = Path(self.tmp.name)
        self.token = root / "client_token"
        self.gold = root / "gold.jsonl"
        self.token.write_text(" token-value\n", encoding="utf-8")
        cases = [
            {
                "id": "hit",
                "category": "업무",
                "difficulty": "hard",
                "question": "hit question",
                "gold_paths": ["업무/hit"],
                "must_contain": ["answer"],
            },
            {
                "id": "miss",
                "category": "지식",
                "question": "miss question",
                "gold_paths": ["지식/miss"],
                "must_contain": ["required"],
            },
            {
                "id": "skip",
                "category": "부재",
                "difficulty": "negative",
                "question": "skip question",
                "gold_paths": [],
            },
        ]
        self.gold.write_text(
            "# private gold fixture\n\n" + "\n".join(json.dumps(case, ensure_ascii=False) for case in cases),
            encoding="utf-8",
        )

    def base_args(self, mode):
        return [
            "--mode", mode,
            "--gold", str(self.gold),
            "--token-file", str(self.token),
            "--gw", "http://fixture-gw",
            "--timeout", "7",
        ]

    def test_recall_filters_ids_skips_negative_case_and_emits_machine_score(self) -> None:
        def score(case, gw, token, k, timeout):
            self.assertEqual((gw, token, k, timeout), ("http://fixture-gw", "token-value", 8, 7))
            return case["id"] == "hit", [f"top/{case['id']}"]

        with mock.patch.object(wiki_qa, "score_recall", side_effect=score) as recall:
            rc, stdout, stderr = invoke_main(
                wiki_qa,
                self.base_args("recall") + ["--ids", "hit,skip"],
            )
        self.assertEqual((rc, stderr), (0, ""))
        self.assertEqual(recall.call_count, 1)
        self.assertIn("hit", stdout)
        self.assertIn("skip", stdout)
        self.assertIn("recall:~ (gold_paths 없음)", stdout)
        self.assertIn("WIKI_QA_RECALL=1/1 (100%)", stdout)
        self.assertIn("recall/hard: 1/1 (100%)", stdout)
        self.assertTrue(stdout.rstrip().endswith("metric_value=100"))

    def test_recall_error_is_a_counted_miss_with_bounded_diagnostic(self) -> None:
        with mock.patch.object(wiki_qa, "score_recall", side_effect=RuntimeError("gateway down")):
            rc, stdout, _ = invoke_main(
                wiki_qa,
                self.base_args("recall") + ["--ids", "hit"],
            )
        self.assertEqual(rc, 0)
        self.assertIn("top=(error: gateway down)", stdout)
        self.assertIn("WIKI_QA_RECALL=0/1 (0%)", stdout)
        self.assertTrue(stdout.rstrip().endswith("metric_value=0"))

    def test_answer_mode_counts_pass_fail_skip_and_sorts_difficulty_summary(self) -> None:
        results = {
            "hit": (True, 123, "answer present"),
            "miss": (False, 456, "wrong response"),
            "skip": (None, 0, ""),
        }

        def score(case, *_args):
            return results[case["id"]]

        with mock.patch.object(wiki_qa, "score_answer", side_effect=score):
            rc, stdout, stderr = invoke_main(
                wiki_qa,
                self.base_args("answer") + ["--verbose"],
            )
        self.assertEqual((rc, stderr), (0, ""))
        self.assertIn("answer:✓ 123ms | answer present", stdout)
        self.assertIn("answer:✗ 456ms missing=['required'] | wrong response", stdout)
        self.assertIn("answer:~ (must_contain 없음 — 채점 제외)", stdout)
        self.assertIn("WIKI_QA_ANSWER=1/2 (50%) skipped=1", stdout)
        self.assertLess(stdout.index("answer/easy:"), stdout.index("answer/hard:"))
        self.assertTrue(stdout.rstrip().endswith("metric_value=50"))

    def test_unmatched_ids_return_exit_two_without_running_scores(self) -> None:
        with mock.patch.object(wiki_qa, "score_recall") as recall:
            rc, stdout, stderr = invoke_main(
                wiki_qa,
                self.base_args("recall") + ["--ids", "does-not-exist"],
            )
        self.assertEqual((rc, stdout), (2, ""))
        self.assertEqual(stderr.strip(), "no cases selected")
        recall.assert_not_called()

    def test_when_gateway_default_prefers_explicit_qa_environment(self) -> None:
        with mock.patch.object(wiki_qa, "score_recall", return_value=(True, ["업무/hit"])) as recall:
            rc, _, _ = invoke_main(
                wiki_qa,
                [
                    "--gold", str(self.gold),
                    "--token-file", str(self.token),
                    "--ids", "hit",
                ],
                env={
                    "DENEB_QA_GW": "http://qa",
                    "DENEB_LIVETEST_GW_URL": "http://livetest",
                },
            )
        self.assertEqual(rc, 0)
        self.assertEqual(recall.call_args.args[1], "http://qa")

    def test_real_help_entrypoint_preserves_benchmark_options(self) -> None:
        proc = subprocess.run(
            [sys.executable, wiki_qa.__file__, "--help"],
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(proc.returncode, 0)
        for option in ("--mode", "--gold", "--gw", "--token-file", "--k", "--ids", "--limit"):
            self.assertIn(option, proc.stdout)


if __name__ == "__main__":
    unittest.main()
