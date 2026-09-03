"""Guard, protocol, grounding, and pairing tests for mine_reverse_gold.

The miner feeds a metric, so the half that decides whether a candidate is kept
must be pinned here: a guard that silently admits a paraphrase turns the
direction split into a comparison of two direct questions, and a guard that
wrongly condemns a case shrinks the very set it exists to build.
"""

from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import mine_reverse_gold as miner  # noqa: E402  (path set above)


DIRECT = {
    "id": "gangjin-epc-legal",
    "category": "프로젝트",
    "question": "강진 신다산 EPC 계약서 법무검토 결과 뭐였지?",
    "gold_paths": ["프로젝트/com-sds-epc-001"],
    "must_contain": ["하자보수"],
    "must_not": [],
}

# Pages shaped like the real wiki: projects one level down AND vendor pages
# nested a level deeper under 프로젝트/거래/, which is what broke the first
# implementation.
PAGES = {
    "프로젝트/com-sds-epc-001/대표.md": "강진 신다산 하자보수 조항 4·5번",
    "프로젝트/com-sds-epc-001/로그.md": "하자보수 재확인",
    "프로젝트/pl2-dsv-epc-001/대표.md": "당진 솔라빌리지 하자보수 및 Deviation",
    "프로젝트/거래/현대에너지솔루션/대표.md": "국내 모듈 점유 2위",
    "업무/BEP.md": "블랙록 펀드",
    "index.md": "루트 문서",
}
TARGETS = ["프로젝트/com-sds-epc-001", "프로젝트/pl2-dsv-epc-001",
           "프로젝트/거래/현대에너지솔루션", "업무/BEP"]


class ProtocolTests(unittest.TestCase):
    def test_reply_survives_a_code_fence_and_surrounding_prose(self) -> None:
        raw = '설명입니다\n```json\n{"subject": "강진 신다산", "reverse": "어디?"}\n```\n'
        self.assertEqual(miner.parse_reply(raw),
                         {"subject": "강진 신다산", "reverse": "어디?"})

    def test_unparseable_reply_costs_its_own_case_not_the_run(self) -> None:
        for raw in ("", "   ", "no json here", "{broken", "[1, 2]"):
            self.assertEqual(miner.parse_reply(raw), {}, raw)

    def test_alternatives_in_must_contain_split_on_the_pipe(self) -> None:
        self.assertEqual(miner.detail_tokens(["참여 종결|계약 유효", " 하자보수 "]),
                         ["참여 종결", "계약 유효", "하자보수"])
        self.assertEqual(miner.detail_tokens([]), [])
        self.assertEqual(miner.detail_tokens(None), [])


class PageKindTests(unittest.TestCase):
    def test_the_asked_for_noun_follows_the_gold_target_family(self) -> None:
        # Asking "which 현장?" about a fund profile or a contact card yields a
        # question with no valid answer, which then scores as a permanent miss.
        self.assertEqual(miner.page_kind(["프로젝트/com-sds-epc-001"]), "현장")
        self.assertEqual(miner.page_kind(["업무/BEP"]), "문서")
        self.assertEqual(miner.page_kind(["인물/에코프로-담당자"]), "인물")

    def test_a_prefixless_fragment_is_treated_as_a_site(self) -> None:
        self.assertEqual(miner.page_kind(["sunkean"]), "현장")
        self.assertEqual(miner.page_kind([]), "현장")
        self.assertEqual(miner.page_kind(None), "현장")

    def test_a_recognized_family_wins_over_a_leading_fragment(self) -> None:
        self.assertEqual(miner.page_kind(["라이젠", "프로젝트/pl3-ghg-mod-001"]), "현장")
        self.assertEqual(miner.page_kind(["카이엠", "업무/BEP"]), "문서")


class CorpusGroundingTests(unittest.TestCase):
    def test_a_nested_target_is_not_condemned_as_dead_gold(self) -> None:
        # The regression that motivated this rewrite: folding pages into a fixed
        # two-segment "subtree" turned 프로젝트/거래/현대에너지솔루션 into
        # 프로젝트/거래, the match failed, and 37 sound cases were reported dead
        # against a measured truth of 3.
        reason, reach = miner.clue_verdict(
            PAGES, TARGETS, ["프로젝트/거래/현대에너지솔루션"], ["2위"])
        self.assertIsNone(reason)
        self.assertEqual(reach, 1)

    def test_gold_path_matches_only_at_a_segment_start(self) -> None:
        # Without the segment rule a short gold string would claim unrelated
        # pages and the ambiguity count would collapse toward 1 — hiding exactly
        # the cases the count exists to flag.
        self.assertTrue(miner.path_hit("프로젝트/com-sds-epc-001",
                                       "프로젝트/com-sds-epc-001/대표.md"))
        self.assertTrue(miner.path_hit("com-sds-epc-001",
                                       "프로젝트/com-sds-epc-001/대표.md"))
        self.assertFalse(miner.path_hit("com-sds-epc-001",
                                        "프로젝트/남com-sds-epc-001/대표.md"))
        self.assertFalse(miner.path_hit("", "프로젝트/a/대표.md"))

    def test_reach_counts_scoreable_targets_not_files(self) -> None:
        # Two files inside one project are ONE candidate answer.
        self.assertEqual(miner.clue_reach(PAGES, TARGETS, ["하자보수"]), 2)
        self.assertEqual(miner.clue_reach(PAGES, TARGETS, ["Deviation"]), 1)
        self.assertEqual(miner.clue_reach(PAGES, TARGETS, ["없는토큰"]), 0)

    def test_gold_targets_are_the_distinct_paths_the_bench_can_score(self) -> None:
        cases = [DIRECT, dict(DIRECT, id="b", gold_paths=["업무/BEP", "프로젝트/x"]),
                 dict(DIRECT, id="c", gold_paths=["업무/BEP"])]
        self.assertEqual(miner.gold_targets(cases),
                         ["프로젝트/com-sds-epc-001", "업무/BEP", "프로젝트/x"])

    def test_a_clue_missing_from_its_own_target_is_refused_as_dead_gold(self) -> None:
        reason, _ = miner.clue_verdict(
            PAGES, TARGETS, ["프로젝트/com-sds-epc-001"], ["Deviation"])
        self.assertEqual(reason, "clue absent from its own gold page")

    def test_an_unresolvable_gold_path_is_named_apart_from_a_missing_clue(self) -> None:
        # Mining a twin off a gold path that resolves to nothing would only give
        # a pre-existing defect a second id; it is not the same failure as a
        # resolvable page that lacks its clue, so it does not share the label.
        reason, _ = miner.clue_verdict(PAGES, TARGETS, ["sunkean"], ["2,240"])
        self.assertEqual(reason, "gold path resolves to no page")
        reason, _ = miner.clue_verdict(PAGES, TARGETS, [], ["포스코"])
        self.assertEqual(reason, "no gold path to target")

    def test_a_clue_on_its_own_target_is_kept_with_its_reach(self) -> None:
        reason, reach = miner.clue_verdict(
            PAGES, TARGETS, ["프로젝트/com-sds-epc-001"], ["하자보수"])
        self.assertIsNone(reason)
        self.assertEqual(reach, 2, "a two-target clue needs an added detail")

    def test_without_a_corpus_the_check_abstains_rather_than_guesses(self) -> None:
        self.assertEqual(miner.clue_verdict(None, TARGETS, ["프로젝트/x"], ["t"]),
                         (None, 0))
        self.assertEqual(miner.clue_verdict(PAGES, TARGETS, ["프로젝트/x"], []),
                         (None, 0))

    def test_excerpt_prefers_the_longest_page_under_the_target(self) -> None:
        pages = {"프로젝트/a/대표.md": "짧다", "프로젝트/a/로그.md": "훨씬 더 긴 본문 " * 5}
        self.assertIn("훨씬 더 긴", miner.excerpt_for(pages, ["프로젝트/a"], 1500))
        self.assertEqual(miner.excerpt_for(pages, ["프로젝트/없음"], 1500), "")


class PromptGroundingTests(unittest.TestCase):
    def build(self, case, pages=PAGES, targets=TARGETS, chars=1500):
        return miner.build_prompt(case, pages, targets, chars)

    def test_an_ambiguous_clue_gets_the_excerpt_and_a_one_clue_cap(self) -> None:
        prompt = self.build({"question": "강진 하자보수?",
                             "gold_paths": ["프로젝트/com-sds-epc-001"],
                             "must_contain": ["하자보수"]})
        self.assertIn("2곳에 나타난다", prompt)
        self.assertIn("딱 하나만", prompt)
        self.assertIn("둘 이상 보태지 마라", prompt)
        self.assertIn("4·5번", prompt, "the excerpt must supply the extra clue")

    def test_a_pinned_clue_gets_neither_permission_nor_material_to_pad_with(self) -> None:
        prompt = self.build({"question": "당진 진행 상황?",
                             "gold_paths": ["프로젝트/pl2-dsv-epc-001"],
                             "must_contain": ["Deviation"]})
        self.assertIn("한 곳에만", prompt)
        self.assertIn("절대 보태지 마라", prompt)
        self.assertNotIn("대상 페이지 발췌", prompt,
                         "withholding the excerpt is the cheapest way to stop padding")

    def test_no_excerpt_asks_for_a_plain_rewrite_not_an_impossible_one(self) -> None:
        # Telling the model to "pick one from the excerpt below" when there is no
        # excerpt is what drove the decline rate to 95% on the 2026-09-03 run.
        prompt = self.build({"question": "q", "gold_paths": ["프로젝트/없음"],
                             "must_contain": ["하자보수"]})
        self.assertNotIn("대상 페이지 발췌", prompt)
        self.assertNotIn("아래 발췌에서", prompt)
        self.assertIn("원 질문의 정보만으로", prompt)

    def test_excerpt_is_capped_so_one_long_page_cannot_blow_the_prompt(self) -> None:
        pages = {"프로젝트/a/대표.md": "가" * 5000, "프로젝트/b/대표.md": "가"}
        prompt = miner.build_prompt(
            {"question": "q", "gold_paths": ["프로젝트/a"], "must_contain": ["가"]},
            pages, ["프로젝트/a", "프로젝트/b"], 100)
        self.assertIn("가" * 100, prompt)
        self.assertNotIn("가" * 101, prompt)

    def test_without_a_corpus_the_prompt_carries_neither_note_nor_excerpt(self) -> None:
        prompt = self.build({"question": "q", "gold_paths": ["프로젝트/a"],
                             "must_contain": ["t"]}, pages=None, targets=[])
        self.assertNotIn("대상 페이지 발췌", prompt)
        self.assertNotIn("나타난다", prompt)


class MinimalismTests(unittest.TestCase):
    """Padding is retrieval help, so an over-specified reverse arm reports a
    smaller deficit than the real one — the opposite of the split's purpose."""

    DIRECT_Q = "당진 솔라빌리지 최근 진행 상황 어때?"

    def test_a_clue_that_already_pins_the_answer_buys_no_extra_room(self) -> None:
        padded = ("Deviation 이슈가 기록된 걸 보려는데, 감리사를 한빛디엔에스로 선정해서 "
                  "EPC 공급가액을 1,042.78억으로 조정했던 그 현장이 어디지?")
        reason = miner.growth_reject(self.DIRECT_Q, padded, reach=1)
        self.assertIsNotNone(reason)
        self.assertIn("over-specified", reason)

    def test_a_plain_rephrasing_fits_the_pinned_budget(self) -> None:
        self.assertIsNone(miner.growth_reject(
            self.DIRECT_Q, "Deviation 이슈가 기록된 현장이 어디였지?", reach=1))

    def test_an_ambiguous_clue_earns_room_for_exactly_one_addition(self) -> None:
        one = "154kV 케이블을 ZTT에서 공급받은 신안군 태양광 현장이 어디였지?"
        self.assertIsNone(miner.growth_reject(self.DIRECT_Q, one, reach=12))
        four = ("부산항만공사 소유 건물 4개소에 약 1,975kW 자가소비로 깔고 진코 640Wp "
                "모듈 쓰는 제이티에너지 건에서 추가 가배치 요청이 들어온 현장이 어디더라?")
        self.assertIsNotNone(miner.growth_reject(self.DIRECT_Q, four, reach=12))

    def test_an_over_specified_rewrite_is_rejected_by_the_run(self) -> None:
        padded = ("하자보수 조항이 쟁점이고 감리사가 한빛디엔에스이며 공급가액 1,042.78억으로 "
                  "조정되고 4·5번 조항 삭제를 요청한 그 현장이 어디였는지 알려줄래?")
        emitted, rejects = miner.mine(
            [DIRECT], "m",
            ask=lambda *_a: json.dumps({"subject": "강진 신다산", "reverse": padded},
                                       ensure_ascii=False),
            log=lambda _s: None, pages=PAGES, targets=TARGETS)
        self.assertEqual([c["id"] for c in emitted], ["gangjin-epc-legal"])
        self.assertIn("over-specified", rejects[0][1])


class GuardTests(unittest.TestCase):
    def keep(self, subject: str, reverse: str, must_contain=None):
        return miner.reject_reason(
            DIRECT["question"], subject, reverse,
            DIRECT["must_contain"] if must_contain is None else must_contain)

    def test_a_well_formed_reverse_question_is_kept(self) -> None:
        self.assertIsNone(self.keep(
            "강진 신다산", "EPC 법무검토에서 하자보수 쟁점이 나온 현장이 어디였지?"))

    def test_a_rewrite_that_still_names_the_project_is_a_paraphrase(self) -> None:
        # The whole point of the reverse arm is that the project name is the
        # answer; leaving it in the question scores as a direct case.
        self.assertEqual(
            self.keep("강진 신다산",
                      "강진 신다산에서 하자보수 쟁점이 나온 건 어느 계약이었지?"),
            "subject leaks into the reverse question")

    def test_leak_check_is_not_defeated_by_spacing(self) -> None:
        self.assertEqual(
            self.keep("강진 신다산", "강진  신다산 에서 하자보수 관련 현장은?"),
            "subject leaks into the reverse question")

    def test_subject_the_model_invented_is_refused(self) -> None:
        # A hallucinated subject makes the leak guard check a string that was
        # never in the question — it would pass while the name remains.
        self.assertEqual(self.keep("영덕 풍력", "하자보수 쟁점이 나온 현장이 어디였지?"),
                         "subject not quoted from the direct question")

    def test_dropping_the_detail_clue_leaves_an_unanswerable_question(self) -> None:
        self.assertEqual(self.keep("강진 신다산", "법무검토를 했던 현장이 어디였지?"),
                         "detail clue dropped from the reverse question")

    def test_any_one_alternative_token_satisfies_the_detail_guard(self) -> None:
        self.assertIsNone(miner.reject_reason(
            "감포 파인드그린 풍력 지금 뭐가 이슈야?", "감포 파인드그린",
            "계약 유효 판단이 쟁점이던 풍력 현장이 어디였지?", ["참여 종결|계약 유효"]))

    def test_declined_and_empty_candidates_each_name_their_cause(self) -> None:
        self.assertEqual(self.keep("강진 신다산", ""),
                         "model declined (clue too generic)")
        self.assertEqual(self.keep("", "하자보수 현장?"), "no subject identified")


class PairingTests(unittest.TestCase):
    def test_reverse_twin_keeps_the_target_so_the_two_arms_are_matched(self) -> None:
        twin = miner.reverse_case(DIRECT, "  하자보수 쟁점 현장이 어디였지?  ")
        self.assertEqual(twin["gold_paths"], DIRECT["gold_paths"])
        self.assertEqual(twin["must_contain"], DIRECT["must_contain"])
        self.assertEqual(twin["id"], "gangjin-epc-legal-rev")
        self.assertEqual(twin["question"], "하자보수 쟁점 현장이 어디였지?")
        self.assertEqual(twin["direction"], "reverse")
        self.assertNotIn("direction", DIRECT, "source case must not be mutated")

    def test_direct_case_is_labeled_without_touching_the_original(self) -> None:
        labeled = miner.direct_case(DIRECT)
        self.assertEqual(labeled["direction"], "direct")
        self.assertNotIn("direction", DIRECT)


class LoadAndMineTests(unittest.TestCase):
    def test_gold_header_comments_and_junk_lines_are_skipped(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "gold.jsonl"
            path.write_text("# header\n\n"
                            + json.dumps(DIRECT, ensure_ascii=False) + "\n"
                            + "{broken\n" + '{"id": "no-question"}\n',
                            encoding="utf-8")
            cases = miner.load_gold(str(path))
        self.assertEqual([c["id"] for c in cases], ["gangjin-epc-legal"])

    def test_load_wiki_keys_pages_by_relative_path(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            page = root / "프로젝트/a/대표.md"
            page.parent.mkdir(parents=True)
            page.write_text("본문", encoding="utf-8")
            pages = miner.load_wiki(str(root))
        self.assertEqual(pages, {"프로젝트/a/대표.md": "본문"})

    def test_mine_emits_both_arms_and_attributes_every_rejection(self) -> None:
        second = dict(DIRECT, id="other", question="당진 진행 상황 어때?",
                      gold_paths=["프로젝트/pl2-dsv-epc-001"],
                      must_contain=["Deviation"])
        replies = {
            DIRECT["question"]: '{"subject": "강진 신다산", '
                                '"reverse": "하자보수 쟁점이 나온 현장이 어디였지?"}',
            second["question"]: '{"subject": "당진", "reverse": ""}',
        }

        def ask(_model, prompt):
            for question, reply in replies.items():
                if question in prompt:
                    return reply
            raise AssertionError(f"unexpected prompt: {prompt}")

        emitted, rejects = miner.mine([DIRECT, second], "m", ask=ask,
                                      log=lambda _s: None,
                                      pages=PAGES, targets=TARGETS)

        self.assertEqual([(c["id"], c["direction"]) for c in emitted],
                         [("gangjin-epc-legal", "direct"),
                          ("gangjin-epc-legal-rev", "reverse"), ("other", "direct")])
        self.assertEqual(rejects, [("other", "model declined (clue too generic)")])

    def test_dead_gold_is_refused_before_the_model_is_paid_for_it(self) -> None:
        # The clue is not on the target, so no rewrite could ever be answerable;
        # calling the model first would buy a candidate that must be thrown away.
        case = dict(DIRECT, must_contain=["Deviation"])
        calls = []

        emitted, rejects = miner.mine(
            [case], "m",
            ask=lambda *a: calls.append(a) or '{"subject":"x","reverse":"y"}',
            log=lambda _s: None, pages=PAGES, targets=TARGETS)

        self.assertEqual(calls, [], "a dead-gold case must not reach the model")
        self.assertEqual(rejects,
                         [("gangjin-epc-legal", "clue absent from its own gold page")])
        self.assertEqual([c["id"] for c in emitted], ["gangjin-epc-legal"])

    def test_kept_twin_records_how_far_the_clue_reaches(self) -> None:
        emitted, _ = miner.mine(
            [DIRECT], "m",
            ask=lambda *_a: '{"subject": "강진 신다산", '
                            '"reverse": "하자보수 쟁점 현장이 어디였지?"}',
            log=lambda _s: None, pages=PAGES, targets=TARGETS)
        self.assertEqual(emitted[1]["clue_reach"], 2)

    def test_a_failing_model_call_drops_one_case_and_keeps_going(self) -> None:
        second = dict(DIRECT, id="other")
        calls = []

        def ask(_model, prompt):
            calls.append(prompt)
            if len(calls) == 1:
                raise OSError("wormhole down")
            return '{"subject": "강진 신다산", "reverse": "하자보수 현장이 어디였지?"}'

        emitted, rejects = miner.mine([DIRECT, second], "m", ask=ask,
                                      log=lambda _s: None,
                                      pages=PAGES, targets=TARGETS)

        self.assertEqual(len(calls), 2, "a failed case must not abort the run")
        self.assertEqual([c["id"] for c in emitted],
                         ["gangjin-epc-legal", "other", "other-rev"])
        self.assertIn("model call failed", rejects[0][1])


class CliWiringTests(unittest.TestCase):
    """The corpus grounding is only real if the CLI actually reaches it.

    Every guard above is exercised through mine()/build_prompt() directly, which
    stays green even when --wiki is not wired to them — that is exactly how the
    flag went missing once.
    """

    def run_cli(self, extra_argv, pages=None):
        seen = {}

        def fake_mine(cases, model, pages=None, targets=None,
                      excerpt_chars=1500, **_kw):
            seen.update(pages=pages, targets=targets, excerpt_chars=excerpt_chars)
            return [miner.direct_case(cases[0])], []

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            gold = root / "gold.jsonl"
            gold.write_text(json.dumps(DIRECT, ensure_ascii=False) + "\n",
                            encoding="utf-8")
            wiki = root / "wiki"
            for rel, text in (pages or {}).items():
                page = wiki / rel
                page.parent.mkdir(parents=True, exist_ok=True)
                page.write_text(text, encoding="utf-8")
            argv = ["--gold", str(gold), "--out", str(root / "out.jsonl")] + [
                str(wiki) if token == "<WIKI>" else token for token in extra_argv]
            real_mine, miner.mine = miner.mine, fake_mine
            try:
                rc = miner.main(argv)
            finally:
                miner.mine = real_mine
        return rc, seen

    def test_wiki_flag_reaches_the_miner_as_pages_and_targets(self) -> None:
        rc, seen = self.run_cli(["--wiki", "<WIKI>", "--excerpt-chars", "40"],
                                pages={"프로젝트/com-sds-epc-001/대표.md": "하자보수"})
        self.assertEqual(rc, 0)
        self.assertEqual(seen["excerpt_chars"], 40)
        self.assertEqual(list(seen["pages"]), ["프로젝트/com-sds-epc-001/대표.md"])
        self.assertEqual(seen["targets"], ["프로젝트/com-sds-epc-001"])

    def test_without_the_flag_the_miner_is_told_there_is_no_corpus(self) -> None:
        rc, seen = self.run_cli([])
        self.assertEqual(rc, 0)
        self.assertIsNone(seen["pages"])


class RetryTests(unittest.TestCase):
    def test_only_transient_failures_are_worth_waiting_out(self) -> None:
        import urllib.error

        def http(code):
            return urllib.error.HTTPError("u", code, "m", None, None)

        self.assertTrue(miner.should_retry(http(500)))
        self.assertTrue(miner.should_retry(http(429)))
        self.assertFalse(miner.should_retry(http(404)),
                         "an unserved model fails identically forever")
        self.assertTrue(miner.should_retry(OSError("reset")))


if __name__ == "__main__":
    unittest.main()
