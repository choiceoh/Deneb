#!/usr/bin/env python3
"""Contract tests for the LongMemEval reader harness.

Everything here is the deterministic half — protocol parsing, ref resolution,
window and search rendering, scoring. The model calls are not exercised; they
are the one part that cannot be pinned, which is exactly why the rest is.

The cases are not hypothetical. Each of the parsing ones is a shape that
actually cost a measured run when the original harness mishandled it.

    python3 -m unittest discover -s scripts/eval -p 'test_*.py'
"""

import unittest
import unittest.mock
import urllib.error

import reader_tool_eval as rte


class ParseDirectiveTests(unittest.TestCase):
    def test_plain_directives(self):
        self.assertEqual(rte.parse_directive("OPEN cl:lm:q1:s12"),
                         ("OPEN", "cl:lm:q1:s12"))
        self.assertEqual(rte.parse_directive("SEARCH 병원 예약"),
                         ("SEARCH", "병원 예약"))

    def test_answer_keeps_body_past_the_first_line(self):
        verb, arg = rte.parse_directive("ANSWER 세 번이다.\n근거: 5/2, 6/9, 7/1")
        self.assertEqual(verb, "ANSWER")
        self.assertIn("세 번이다.", arg)
        self.assertIn("7/1", arg)

    def test_directive_survives_markdown_and_fences(self):
        for wrapped in ("**OPEN** s3", "`OPEN s3`", "  ## OPEN s3", "OPEN: s3"):
            with self.subTest(wrapped=wrapped):
                self.assertEqual(rte.parse_directive(wrapped), ("OPEN", "s3"))

    def test_directive_found_after_leading_prose(self):
        reply = "먼저 해당 대화를 확인해야겠다.\nOPEN cl:lm:q1:s7\n"
        self.assertEqual(rte.parse_directive(reply), ("OPEN", "cl:lm:q1:s7"))

    def test_unprefixed_reply_counts_as_an_answer(self):
        # A reader that forgets the protocol still produced an answer; scoring
        # it as "no answer" would blame the reader for a parser gap.
        verb, arg = rte.parse_directive("사용자는 5월에 이사했다.")
        self.assertEqual(verb, "ANSWER")
        self.assertEqual(arg, "사용자는 5월에 이사했다.")

    def test_empty_reply_is_an_empty_answer(self):
        self.assertEqual(rte.parse_directive(""), ("ANSWER", ""))

    def test_bare_verb_without_argument_is_not_an_escalation(self):
        # "OPEN" with nothing to open must not burn a budget slot.
        verb, _ = rte.parse_directive("OPEN")
        self.assertEqual(verb, "ANSWER")


class ResolveRefTests(unittest.TestCase):
    def test_every_ref_shape_the_renderer_emits(self):
        for ref in ("client:lm:q1:s12", "cl:lm:q1:s12", "s12",
                    "cl:lm:q1:s12#3/user", "cl:lm:q1:s12 요약"):
            with self.subTest(ref=ref):
                self.assertEqual(rte.resolve_ref(ref, 50), 12)

    def test_qid_digits_do_not_shadow_the_session(self):
        # The qid itself can contain an sNN-looking token; the session is the
        # last one before the anchor.
        self.assertEqual(rte.resolve_ref("cl:lm:s99abc:s4#1/user", 50), 4)

    def test_out_of_range_and_garbage_reject(self):
        self.assertIsNone(rte.resolve_ref("s99", 50))
        self.assertIsNone(rte.resolve_ref("nonsense", 50))
        self.assertIsNone(rte.resolve_ref("", 50))


class RenderWindowTests(unittest.TestCase):
    def setUp(self):
        self.turns = [{"role": "user" if i % 2 == 0 else "assistant",
                       "content": f"메시지 {i}"} for i in range(30)]

    def test_header_carries_the_date(self):
        out = rte.render_window(self.turns, "2023-05-20", "cl:lm:q1:s3")
        self.assertIn("2023-05-20", out)
        self.assertIn("cl:lm:q1:s3", out)
        self.assertIn("of 30", out)

    def test_window_is_a_deep_read_not_a_preview(self):
        long_turns = [{"role": "assistant", "content": "가" * 3000}]
        out = rte.render_window(long_turns, "2023-05-20", "s0")
        # 3000 Korean chars is ~9000 bytes; the per-message cap is 4000, so it
        # clips — but far past the 1200B preview that defeated escalation.
        self.assertGreater(len(out.encode()), 3000)

    def test_total_budget_is_enforced(self):
        big = [{"role": "user", "content": "나" * 5000} for _ in range(10)]
        out = rte.render_window(big, "2023-05-20", "s0")
        self.assertLessEqual(len(out.encode()), rte.WINDOW_TOTAL_BYTES + 4000)

    def test_short_session_renders_completely(self):
        short = [{"role": "user", "content": "짧은 대화"}]
        out = rte.render_window(short, "2023-05-20", "s0")
        self.assertIn("짧은 대화", out)
        self.assertIn("of 1", out)


class SearchSessionsTests(unittest.TestCase):
    def setUp(self):
        self.sessions = [
            [{"role": "user", "content": "피부과 예약했어"}],
            [{"role": "user", "content": "이비인후과 다녀왔다"}],
            [{"role": "user", "content": "관계 없는 잡담"}],
        ]
        self.dates = ["2023-05-20", "2023-06-11", "2023-07-02"]
        self.labels = ["cl:lm:q1:s0", "cl:lm:q1:s1", "cl:lm:q1:s2"]

    def test_headers_carry_dates_for_period_filtering(self):
        out = rte.search_sessions(self.sessions, self.dates, self.labels, "예약")
        self.assertIn("2023-05-20", out)
        self.assertIn("cl:lm:q1:s0", out)

    def test_only_matching_sessions_are_listed(self):
        out = rte.search_sessions(self.sessions, self.dates, self.labels, "예약")
        self.assertIn("s0", out)
        self.assertNotIn("cl:lm:q1:s2", out)

    def test_no_match_says_so_rather_than_inventing(self):
        out = rte.search_sessions(self.sessions, self.dates, self.labels, "존재하지않는말")
        self.assertIn("일치하는 과거 대화가 없습니다", out)

    def test_blank_query_is_rejected(self):
        self.assertIn("비어", rte.search_sessions(self.sessions, self.dates,
                                                  self.labels, "   "))

    def test_result_cap_is_honored(self):
        many = [[{"role": "user", "content": "병원"}] for _ in range(40)]
        out = rte.search_sessions(many, ["2023-01-01"] * 40,
                                  [f"s{i}" for i in range(40)], "병원",
                                  max_results=5)
        self.assertEqual(out.count("### Session:"), 5)


class ServeDirectiveTests(unittest.TestCase):
    def setUp(self):
        self.hay = {
            "sessions": [[{"role": "user", "content": "내용 A"}],
                         [{"role": "user", "content": "내용 B"}]],
            "dates": ["2023-05-20", "2023-06-11"],
            "labels": ["cl:lm:q1:s0", "cl:lm:q1:s1"],
        }

    def test_open_spends_a_slot_and_serves_the_window(self):
        served, opens, searches = rte.serve_directive(
            "OPEN", "s1", self.hay, 0, 0, 2, 2)
        self.assertIn("내용 B", served)
        self.assertEqual((opens, searches), (1, 0))

    def test_unresolvable_ref_still_spends_the_slot_but_guides_back(self):
        # Otherwise a model that emits garbage refs loops until the turn cap.
        served, opens, _ = rte.serve_directive("OPEN", "s99", self.hay, 0, 0, 2, 2)
        self.assertIn("SEARCH", served)
        self.assertEqual(opens, 1)

    def test_exhausted_budget_refuses_without_spending(self):
        served, opens, searches = rte.serve_directive(
            "OPEN", "s1", self.hay, 2, 0, 2, 2)
        self.assertIn("소진", served)
        self.assertEqual((opens, searches), (2, 0))

    def test_missing_haystack_degrades_instead_of_raising(self):
        served, _, _ = rte.serve_directive("SEARCH", "무엇", None, 0, 0, 2, 2)
        self.assertIsInstance(served, str)


class SummarizeTests(unittest.TestCase):
    def test_overall_and_per_type_accuracy(self):
        s = rte.summarize([
            {"type": "multi-session", "correct": True, "opens": 1, "searches": 1},
            {"type": "multi-session", "correct": False, "opens": 0, "searches": 1},
            {"type": "temporal-reasoning", "correct": True, "opens": 2,
             "searches": 0, "abstained": True},
        ])
        self.assertEqual(s["n"], 3)
        self.assertEqual(s["overall"], 66.7)
        self.assertEqual(s["by_type"]["multi-session"], 50.0)
        self.assertEqual(s["by_type"]["temporal-reasoning"], 100.0)
        self.assertEqual(s["opens"], 3)
        self.assertEqual(s["searches"], 2)
        self.assertEqual(s["abstain_pct"], 33.3)

    def test_empty_run_does_not_divide_by_zero(self):
        s = rte.summarize([])
        self.assertEqual(s["overall"], 0.0)
        self.assertEqual(s["n"], 0)

    def test_format_summary_lists_every_type(self):
        text = rte.format_summary("t", rte.summarize([
            {"type": "single-session-assistant", "correct": True},
            {"type": "knowledge-update", "correct": False},
        ]))
        self.assertIn("single-session-assistant", text)
        self.assertIn("knowledge-update", text)


class AbstentionTests(unittest.TestCase):
    def test_abstention_qids_are_recognized(self):
        self.assertTrue(rte.is_abstention("abc_abs"))
        self.assertFalse(rte.is_abstention("abc"))

    def test_abstain_detection_covers_both_languages(self):
        self.assertTrue(rte.looks_abstained("잘 모르겠습니다"))
        self.assertTrue(rte.looks_abstained("I do not know"))
        self.assertFalse(rte.looks_abstained("세 번입니다"))


class VerdictTests(unittest.TestCase):
    """An ungraded question is a HOLE in the measurement, not a loss.

    Scoring an unparseable verdict as INCORRECT silently deflates every number
    the harness reports. It really happened: the judge model reasons before
    answering, a small max_tokens was spent entirely on reasoning, and the
    empty reply read as "wrong" for answers matching the reference word for
    word — 2 of 3 questions in the first real run.
    """

    def test_plain_verdicts(self):
        self.assertEqual(rte.parse_verdict("CORRECT matches reference")[0],
                         "CORRECT")
        self.assertEqual(rte.parse_verdict("INCORRECT says May, reference June")[0],
                         "INCORRECT")

    def test_incorrect_is_not_read_as_correct_by_substring(self):
        # "INCORRECT" contains "CORRECT"; position decides.
        self.assertEqual(rte.parse_verdict("INCORRECT")[0], "INCORRECT")

    def test_verdict_after_leading_prose_is_still_found(self):
        self.assertEqual(rte.parse_verdict("판정: CORRECT — 동일한 내용")[0],
                         "CORRECT")

    def test_empty_verdict_is_unscored_not_incorrect(self):
        label, reason = rte.parse_verdict("")
        self.assertEqual(label, "UNSCORED")
        self.assertIn("empty", reason)

    def test_verdict_without_a_token_is_unscored(self):
        self.assertEqual(rte.parse_verdict("음... 판단하기 어렵다")[0], "UNSCORED")

    def test_reader_failure_is_unscored_not_incorrect(self):
        label, reason = rte.judge(
            {"question": "q", "answer": "", "error": "URLError"}, "any-model")
        self.assertEqual(label, "UNSCORED")
        self.assertIn("reader failed", reason)

    def test_summary_surfaces_unscored_questions(self):
        s = rte.summarize([
            {"type": "t", "correct": True, "verdict_label": "CORRECT"},
            {"type": "t", "correct": False, "verdict_label": "UNSCORED"},
        ])
        self.assertEqual(s["unscored"], 1)
        self.assertIn("UNSCORED", rte.format_summary("t", s))


class RetryTests(unittest.TestCase):
    """A long run has to outlive its transport.

    The wormhole restarts under normal operation (deploys, config reloads). A
    236-question run met two restarts and lost 82 questions to "Connection
    refused" — the answers were never wrong, they were never obtained.
    """

    def test_transport_faults_are_retried(self):
        self.assertTrue(rte.should_retry(urllib.error.URLError("refused")))
        self.assertTrue(rte.should_retry(OSError("connection reset")))

    def test_upstream_5xx_and_429_are_retried(self):
        for code in (500, 502, 503, 429):
            with self.subTest(code=code):
                self.assertTrue(rte.should_retry(
                    urllib.error.HTTPError("u", code, "m", {}, None)))

    def test_client_errors_are_not_retried(self):
        # An unserved model 404s identically forever; retrying only delays the
        # run reporting the real cause.
        for code in (400, 401, 404):
            with self.subTest(code=code):
                self.assertFalse(rte.should_retry(
                    urllib.error.HTTPError("u", code, "m", {}, None)))

    def test_call_model_gives_up_after_the_attempt_budget(self):
        slept = []
        with unittest.mock.patch.object(
                rte.urllib.request, "urlopen",
                side_effect=urllib.error.URLError("refused")), \
             unittest.mock.patch.object(rte, "wormhole_token", return_value="t"):
            with self.assertRaises(urllib.error.URLError):
                rte.call_model("m", [{"role": "user", "content": "x"}],
                               attempts=3, sleep=slept.append)
        self.assertEqual(len(slept), 2)          # retries between 3 attempts
        self.assertLess(slept[0], slept[1])      # backoff grows

    def test_call_model_does_not_retry_a_404(self):
        slept = []
        with unittest.mock.patch.object(
                rte.urllib.request, "urlopen",
                side_effect=urllib.error.HTTPError("u", 404, "m", {}, None)), \
             unittest.mock.patch.object(rte, "wormhole_token", return_value="t"):
            with self.assertRaises(urllib.error.HTTPError):
                rte.call_model("m", [{"role": "user", "content": "x"}],
                               attempts=4, sleep=slept.append)
        self.assertEqual(slept, [])


class ReadingStrategyTests(unittest.TestCase):
    """Chain-of-Note is the ceiling lever, so its prompt has to stay intact.

    The benchmark's own paper measures reading strategy under ORACLE retrieval
    — evidence handed over whole — where it moved GPT-4o 0.870 -> 0.924. That
    is the setting this harness found itself in: 76.7 against a 77.5 oracle,
    with retrieval and escalation already spent.
    """

    def test_con_adds_notes_and_keeps_the_protocol(self):
        con = rte.READER_SYSTEM_CON
        self.assertIn("NOTE <ref>", con)
        for directive in ("OPEN <ref>", "SEARCH <query>", "ANSWER <text>"):
            self.assertIn(directive, con)

    def test_con_does_not_ask_for_notes_on_an_escalation(self):
        # Notes in front of an OPEN would break parse_directive's first-verb
        # scan and spend the turn on nothing.
        self.assertIn("alone with no notes", rte.READER_SYSTEM_CON)

    def test_direct_reading_is_unchanged(self):
        self.assertNotIn("NOTE <ref>", rte.READER_SYSTEM)

    def test_notes_before_an_answer_still_parse_as_the_answer(self):
        reply = ("NOTE s3 | 자전거 세 대를 소유\n"
                 "NOTE s7 | NONE\n"
                 "ANSWER 세 대입니다.")
        verb, arg = rte.parse_directive(reply)
        self.assertEqual(verb, "ANSWER")
        self.assertIn("세 대", arg)


class OracleTests(unittest.TestCase):
    """The ceiling has to be re-measured whenever the reader or judge changes.

    A ceiling recorded under one reader says nothing about another; comparing a
    new reading strategy against a stale oracle is how a run convinces itself
    it broke through when it only moved.
    """

    def setUp(self):
        self.hay = {
            "sessions": [[{"role": "user", "content": "관계 없는 잡담"}],
                         [{"role": "user", "content": "자전거 세 대를 소유"},
                          {"role": "assistant", "content": "세 대군요"}]],
            "dates": ["2023-05-20", "2023-06-11"],
            "labels": ["cl:lm:q1:s0", "cl:lm:q1:s1"],
            "gold_indices": [1],
        }

    def test_serves_only_the_evidence_sessions_whole(self):
        out = rte.oracle_block({}, self.hay, self.hay["gold_indices"])
        self.assertIn("자전거 세 대를 소유", out)
        self.assertIn("세 대군요", out)          # the WHOLE session, not a snippet
        self.assertNotIn("관계 없는 잡담", out)  # and nothing else

    def test_reference_date_anchors_temporal_questions(self):
        # The retrieval block carries 기준일=; an oracle without it handicaps
        # temporal questions exactly where the retrieval run is not.
        hay = dict(self.hay, question_date="2023-06-20")
        self.assertIn("기준일=2023-06-20", rte.oracle_block({}, hay, [1]))

    def test_header_carries_label_and_date(self):
        out = rte.oracle_block({}, self.hay, [1])
        self.assertIn("cl:lm:q1:s1", out)
        self.assertIn("2023-06-11", out)

    def test_missing_evidence_degrades_instead_of_raising(self):
        self.assertIn("없음", rte.oracle_block({}, None, []))
        self.assertIn("없음", rte.oracle_block({}, self.hay, []))

    def test_out_of_range_index_is_skipped(self):
        self.assertIn("없음", rte.oracle_block({}, self.hay, [99]))


if __name__ == "__main__":
    unittest.main()
