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

    def test_forbidden_values_unions_must_not_and_stale_values_stably(self) -> None:
        case = {
            "must_not": ["cancelled", "old owner", ""],
            "stale_values": ["old owner", "8120만원"],
        }
        self.assertEqual(
            wiki_qa.forbidden_values(case),
            ["cancelled", "old owner", "8120만원"],
        )

    def test_baseline_detects_case_level_persistent_answer_regression(self) -> None:
        baseline = {
            "summary": {},
            "cases": [{
                "id": "owner-update",
                "answer": {"runs": [{"passed": True}, {"passed": True}, {"passed": False}]},
            }],
        }
        candidate = {
            "summary": {},
            "cases": [{
                "id": "owner-update",
                "answer": {"runs": [{"passed": False}, {"passed": False}, {"passed": False}]},
            }],
        }
        self.assertIn(
            "case owner-update persistent answer regression 2/3 -> 0/3",
            wiki_qa.baseline_regressions(candidate, baseline, 1.0),
        )

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

    def test_stale_values_alone_grade_and_fail_an_answer(self) -> None:
        case = {"question": "owner?", "stale_values": ["김민준"]}
        response = {"payload": {"text": "담당자는 김민준입니다"}}
        with mock.patch.object(wiki_qa, "rpc", side_effect=[{}, response]):
            with mock.patch.object(wiki_qa.time, "sleep"):
                with mock.patch.object(wiki_qa.time, "time", side_effect=[1.0, 1.1]):
                    ok, _, _ = wiki_qa.score_answer(case, "gw", "t", "s", 5)
        self.assertFalse(ok)


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

    def test_answer_repeat_writes_private_safe_json_and_counts_stale_leaks(self) -> None:
        report_path = Path(self.tmp.name) / "report.json"
        lifecycle_case = {
            "id": "lifecycle",
            "category": "기억",
            "difficulty": "hard",
            "question": "현재 담당자는?",
            "gold_paths": [],
            "must_contain": ["박수진"],
            "stale_values": ["김민준"],
            "op_type": "update",
        }
        self.gold.write_text(json.dumps(lifecycle_case, ensure_ascii=False) + "\n", encoding="utf-8")
        runs = [
            (True, 100, "현재 담당자는 박수진입니다"),
            (False, 300, "현재 담당자는 김민준입니다"),
            (True, 200, "현재 담당자는 박수진입니다"),
        ]
        with mock.patch.object(wiki_qa, "score_answer", side_effect=runs):
            rc, stdout, stderr = invoke_main(
                wiki_qa,
                self.base_args("answer") + ["--repeat", "3", "--json-out", str(report_path)],
            )

        self.assertEqual((rc, stderr), (0, ""))
        self.assertIn("answer:✗ 2/3 p95=300ms forbidden=['김민준']", stdout)
        self.assertIn("WIKI_QA_ANSWER=2/3 (66%)", stdout)
        self.assertIn("WIKI_QA_STALE=1/3 (33%)", stdout)
        self.assertIn("CURRENT_VALUE_RATE=2/3 (66%)", stdout)
        self.assertIn("STALE_ANSWER_RATE=1/3 (33%)", stdout)
        report = json.loads(report_path.read_text(encoding="utf-8"))
        self.assertEqual(report["repeat"], 3)
        self.assertEqual(report["summary"]["stale"], {"leaks": 1, "checked": 3, "pct": 100 / 3})
        self.assertEqual(report["summary"]["current_value"], {"hits": 2, "checked": 3, "pct": 200 / 3})
        self.assertEqual(report["summary"]["stale_answer"], {"leaks": 1, "checked": 3, "pct": 100 / 3})
        self.assertEqual(report["cases"][0]["answer"]["runs"][1]["forbidden_hit_count"], 1)
        self.assertFalse(report["cases"][0]["answer"]["runs"][1]["current_value_hit"])
        self.assertTrue(report["cases"][0]["answer"]["runs"][1]["stale_answer_hit"])
        self.assertNotIn("현재 담당자는", report_path.read_text(encoding="utf-8"))
        self.assertNotIn("김민준", report_path.read_text(encoding="utf-8"))

    def test_forget_leak_rate_is_scoped_to_forget_cases(self) -> None:
        forget_case = {
            "id": "forget-owner",
            "category": "기억",
            "question": "예전 담당자는?",
            "gold_paths": [],
            "must_not": ["김민준"],
            "op_type": "forget",
        }
        self.gold.write_text(json.dumps(forget_case, ensure_ascii=False) + "\n", encoding="utf-8")
        runs = [
            (True, 100, "기억하고 있지 않습니다"),
            (False, 200, "예전 담당자는 김민준입니다"),
        ]
        with mock.patch.object(wiki_qa, "score_answer", side_effect=runs):
            rc, stdout, stderr = invoke_main(
                wiki_qa,
                self.base_args("answer") + ["--repeat", "2"],
            )
        self.assertEqual((rc, stderr), (0, ""))
        self.assertIn("FORGET_LEAK_RATE=1/2 (50%)", stdout)
        self.assertNotIn("STALE_ANSWER_RATE=", stdout)

    def test_require_zero_stale_returns_one_on_any_forbidden_answer(self) -> None:
        lifecycle_case = {
            "id": "lifecycle",
            "category": "기억",
            "question": "현재 담당자는?",
            "gold_paths": [],
            "must_contain": ["박수진"],
            "stale_values": ["김민준"],
        }
        self.gold.write_text(json.dumps(lifecycle_case, ensure_ascii=False) + "\n", encoding="utf-8")
        with mock.patch.object(
            wiki_qa,
            "score_answer",
            return_value=(False, 10, "현재 담당자는 김민준입니다"),
        ):
            rc, _, stderr = invoke_main(
                wiki_qa,
                self.base_args("answer") + ["--require-zero-stale"],
            )
        self.assertEqual(rc, 1)
        self.assertIn("CUTOVER_GATE_FAIL: stale/forbidden leak 1건", stderr)

    def test_require_zero_stale_rejects_an_empty_safety_set(self) -> None:
        with mock.patch.object(wiki_qa, "score_answer", return_value=(True, 10, "answer")):
            rc, _, stderr = invoke_main(
                wiki_qa,
                self.base_args("answer") + ["--ids", "hit", "--require-zero-stale"],
            )
        self.assertEqual(rc, 1)
        self.assertIn("CUTOVER_GATE_FAIL: stale/forbidden 채점 케이스 0건", stderr)

    def test_baseline_json_returns_one_when_answer_regresses_beyond_tolerance(self) -> None:
        baseline_path = Path(self.tmp.name) / "baseline.json"
        baseline_path.write_text(
            json.dumps({
                "summary": {
                    "recall": {"passed": 0, "total": 0, "pct": None},
                    "answer": {"passed": 1, "total": 1, "pct": 100.0},
                    "stale": {"leaks": 0, "checked": 0, "pct": None},
                }
            }),
            encoding="utf-8",
        )
        with mock.patch.object(wiki_qa, "score_answer", return_value=(False, 10, "wrong")):
            rc, _, stderr = invoke_main(
                wiki_qa,
                self.base_args("answer")
                + ["--ids", "hit", "--baseline-json", str(baseline_path), "--max-regression-pp", "0"],
            )
        self.assertEqual(rc, 1)
        self.assertIn("CUTOVER_GATE_FAIL: answer 0.00% < baseline 100.00%", stderr)

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
        for option in (
            "--mode",
            "--gold",
            "--gw",
            "--token-file",
            "--k",
            "--ids",
            "--limit",
            "--repeat",
            "--json-out",
            "--baseline-json",
            "--require-zero-stale",
        ):
            self.assertIn(option, proc.stdout)


if __name__ == "__main__":
    unittest.main()
