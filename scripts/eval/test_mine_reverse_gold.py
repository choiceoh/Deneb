"""Guard, protocol, and pairing tests for mine_reverse_gold.

The miner feeds a metric, so the half that decides whether a candidate is kept
must be pinned here: a guard that silently admits a paraphrase turns the
direction split into a comparison of two direct questions.
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


class ProtocolTests(unittest.TestCase):
    def test_reply_survives_a_code_fence_and_surrounding_prose(self) -> None:
        raw = '설명입니다\n```json\n{"subject": "강진 신다산", "reverse": "어디?"}\n```\n'
        self.assertEqual(
            miner.parse_reply(raw),
            {"subject": "강진 신다산", "reverse": "어디?"},
        )

    def test_unparseable_reply_costs_its_own_case_not_the_run(self) -> None:
        for raw in ("", "   ", "no json here", "{broken", "[1, 2]"):
            self.assertEqual(miner.parse_reply(raw), {}, raw)

    def test_alternatives_in_must_contain_split_on_the_pipe(self) -> None:
        self.assertEqual(
            miner.detail_tokens(["참여 종결|계약 유효", " 하자보수 "]),
            ["참여 종결", "계약 유효", "하자보수"],
        )
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
        # Vendor/site fragments like "sunkean" or "라이젠" appear as bare gold
        # paths in the live sets; they resolve to project pages.
        self.assertEqual(miner.page_kind(["sunkean"]), "현장")
        self.assertEqual(miner.page_kind([]), "현장")
        self.assertEqual(miner.page_kind(None), "현장")

    def test_a_recognized_family_wins_over_a_leading_fragment(self) -> None:
        self.assertEqual(miner.page_kind(["라이젠", "프로젝트/pl3-ghg-mod-001"]), "현장")
        self.assertEqual(miner.page_kind(["카이엠", "업무/BEP"]), "문서")


class GuardTests(unittest.TestCase):
    def keep(self, subject: str, reverse: str, must_contain=None):
        return miner.reject_reason(
            DIRECT["question"], subject, reverse,
            DIRECT["must_contain"] if must_contain is None else must_contain)

    def test_a_well_formed_reverse_question_is_kept(self) -> None:
        self.assertIsNone(self.keep(
            "강진 신다산",
            "EPC 계약서 법무검토에서 하자보수 쟁점이 나온 현장이 어디였지?"))

    def test_a_rewrite_that_still_names_the_project_is_a_paraphrase_not_a_reverse(self) -> None:
        # The whole point of the reverse arm is that the project name is the
        # answer; leaving it in the question scores as a direct case.
        reason = self.keep(
            "강진 신다산",
            "강진 신다산 현장에서 하자보수 쟁점이 나온 건 어느 계약이었지?")
        self.assertEqual(reason, "subject leaks into the reverse question")

    def test_leak_check_is_not_defeated_by_spacing(self) -> None:
        reason = self.keep("강진 신다산", "강진  신다산 에서 하자보수 관련 현장은?")
        self.assertEqual(reason, "subject leaks into the reverse question")

    def test_subject_the_model_invented_is_refused(self) -> None:
        # A hallucinated subject makes the leak guard check a string that was
        # never in the question — the guard would pass while the name remains.
        reason = self.keep("영덕 풍력", "하자보수 쟁점이 나온 현장이 어디였지?")
        self.assertEqual(reason, "subject not quoted from the direct question")

    def test_dropping_the_detail_clue_leaves_an_unanswerable_question(self) -> None:
        reason = self.keep("강진 신다산", "법무검토를 했던 현장이 어디였지?")
        self.assertEqual(reason, "detail clue dropped from the reverse question")

    def test_any_one_alternative_token_satisfies_the_detail_guard(self) -> None:
        self.assertIsNone(miner.reject_reason(
            "감포 파인드그린 풍력 지금 뭐가 이슈야?",
            "감포 파인드그린",
            "계약 유효 판단이 쟁점이던 풍력 현장이 어디였지?",
            ["참여 종결|계약 유효"]))

    def test_declined_identical_and_empty_candidates_each_name_their_cause(self) -> None:
        self.assertEqual(self.keep("강진 신다산", ""), "model declined (clue too generic)")
        self.assertEqual(self.keep("", "하자보수 현장?"), "no subject identified")
        self.assertEqual(
            self.keep("강진 신다산", DIRECT["question"].replace("강진 신다산 ", "")),
            "detail clue dropped from the reverse question")


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
        self.assertEqual(labeled["question"], DIRECT["question"])
        self.assertNotIn("direction", DIRECT)


class LoadAndMineTests(unittest.TestCase):
    def test_gold_header_comments_and_junk_lines_are_skipped(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "gold.jsonl"
            path.write_text(
                "# header\n\n"
                + json.dumps(DIRECT, ensure_ascii=False) + "\n"
                + "{broken\n"
                + '{"id": "no-question"}\n',
                encoding="utf-8")
            cases = miner.load_gold(str(path))
        self.assertEqual([c["id"] for c in cases], ["gangjin-epc-legal"])

    def test_mine_emits_both_arms_and_attributes_every_rejection(self) -> None:
        second = dict(DIRECT, id="other", question="영덕 풍력 인허가 어디까지 갔어?",
                      must_contain=["실시계획"])
        replies = {
            DIRECT["question"]: '{"subject": "강진 신다산", '
                                '"reverse": "하자보수 쟁점이 나온 현장이 어디였지?"}',
            second["question"]: '{"subject": "영덕 풍력", "reverse": ""}',
        }

        def ask(_model, prompt):
            for question, reply in replies.items():
                if question in prompt:
                    return reply
            raise AssertionError(f"unexpected prompt: {prompt}")

        emitted, rejects = miner.mine([DIRECT, second], "m", ask=ask, log=lambda _s: None)

        self.assertEqual(
            [(c["id"], c["direction"]) for c in emitted],
            [("gangjin-epc-legal", "direct"), ("gangjin-epc-legal-rev", "reverse"),
             ("other", "direct")],
        )
        self.assertEqual(rejects, [("other", "model declined (clue too generic)")])

    def test_a_failing_model_call_drops_one_case_and_keeps_going(self) -> None:
        second = dict(DIRECT, id="other")
        calls = []

        def ask(_model, prompt):
            calls.append(prompt)
            if len(calls) == 1:
                raise OSError("wormhole down")
            return '{"subject": "강진 신다산", "reverse": "하자보수 현장이 어디였지?"}'

        emitted, rejects = miner.mine([DIRECT, second], "m", ask=ask, log=lambda _s: None)

        self.assertEqual(len(calls), 2, "a failed case must not abort the run")
        self.assertEqual([c["id"] for c in emitted],
                         ["gangjin-epc-legal", "other", "other-rev"])
        self.assertEqual(rejects[0][0], "gangjin-epc-legal")
        self.assertIn("model call failed", rejects[0][1])


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
