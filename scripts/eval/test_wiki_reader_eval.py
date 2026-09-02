#!/usr/bin/env python3
"""Contract tests for the wiki recall reader harness.

The deterministic half only — evidence serving, path resolution, token
scoring, protocol parsing. Model I/O is the one part that cannot be pinned,
which is why the rest is.

    python3 -m unittest discover -s scripts/eval -p 'test_*.py'
"""

import pathlib
import tempfile
import unittest

import wiki_reader_eval as wre


class ResolvePageTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        (self.root / "프로젝트").mkdir()
        (self.root / "프로젝트" / "pl2-dsv-epc-001.md").write_text(
            "당진 솔라빌리지 Deviation 검토", encoding="utf-8")

    def test_resolves_with_and_without_suffix(self):
        for ref in ("프로젝트/pl2-dsv-epc-001", "프로젝트/pl2-dsv-epc-001.md"):
            with self.subTest(ref=ref):
                self.assertIsNotNone(wre.resolve_page(str(self.root), ref))

    def test_strips_the_knowledge_tool_prefix_and_quotes(self):
        for ref in ("w:프로젝트/pl2-dsv-epc-001", '"프로젝트/pl2-dsv-epc-001"'):
            with self.subTest(ref=ref):
                self.assertIsNotNone(wre.resolve_page(str(self.root), ref))

    def test_traversal_outside_the_wiki_is_refused(self):
        # The ref comes from model output; it must not be able to read the
        # filesystem outside the wiki root.
        self.assertIsNone(wre.resolve_page(str(self.root), "../../etc/passwd"))
        self.assertIsNone(wre.resolve_page(str(self.root), "/etc/passwd"))

    def test_unknown_and_empty_refs_return_none(self):
        self.assertIsNone(wre.resolve_page(str(self.root), "없는/경로"))
        self.assertIsNone(wre.resolve_page(str(self.root), ""))
        self.assertIsNone(wre.resolve_page("", "프로젝트/pl2-dsv-epc-001"))


class OracleBlockTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        (self.root / "프로젝트").mkdir()
        (self.root / "프로젝트" / "pl2-dsv-epc-001.md").write_text(
            "Deviation 승인 대기", encoding="utf-8")
        (self.root / "프로젝트" / "other.md").write_text("무관", encoding="utf-8")

    def test_serves_the_gold_page_whole(self):
        out = wre.oracle_block(str(self.root), ["프로젝트/pl2-dsv-epc-001"])
        self.assertIn("Deviation 승인 대기", out)
        self.assertNotIn("무관", out)

    def test_gold_paths_are_substrings_so_a_partial_ref_still_resolves(self):
        # This gold set stores path SUBSTRINGS, not exact paths.
        out = wre.oracle_block(str(self.root), ["pl2-dsv-epc-001"])
        self.assertIn("Deviation", out)

    def test_a_folder_gold_path_serves_every_page_under_it(self):
        """A third of gold_paths name a project FOLDER, not a page.

        Serving an arbitrary one of them is not an oracle, and it showed: the
        first oracle run scored BELOW the retrieval run (70.2 vs 79.8) because
        rglob order handed over 로그.md while the answer sat in 대표.md.
        """
        folder = self.root / "프로젝트" / "pl2-abc-001"
        folder.mkdir(parents=True)
        (folder / "로그.md").write_text("일지 항목", encoding="utf-8")
        (folder / "대표.md").write_text("Deviation 승인 대기", encoding="utf-8")
        (folder / "회의록").mkdir()
        (folder / "회의록" / "07-27.md").write_text("회의 메모", encoding="utf-8")
        out = wre.oracle_block(str(self.root), ["프로젝트/pl2-abc-001"])
        for expected in ("Deviation 승인 대기", "일지 항목", "회의 메모"):
            self.assertIn(expected, out)

    def test_representative_page_is_served_first(self):
        # The budget must cut the least important pages, never 대표.
        folder = self.root / "프로젝트" / "pl2-xyz-001"
        folder.mkdir(parents=True)
        (folder / "z-기타.md").write_text("주변 정보", encoding="utf-8")
        (folder / "대표.md").write_text("핵심 요약", encoding="utf-8")
        out = wre.oracle_block(str(self.root), ["프로젝트/pl2-xyz-001"])
        self.assertLess(out.index("핵심 요약"), out.index("주변 정보"))

    def test_pages_are_not_served_twice(self):
        out = wre.oracle_block(str(self.root),
                               ["프로젝트/pl2-dsv-epc-001",
                                "프로젝트/pl2-dsv-epc-001"])
        self.assertEqual(out.count("Deviation 승인 대기"), 1)

    def test_missing_gold_degrades_instead_of_serving_silence(self):
        self.assertIn("없음", wre.oracle_block(str(self.root), ["없는/페이지"]))
        self.assertIn("없음", wre.oracle_block(str(self.root), []))


class TokenVerdictTests(unittest.TestCase):
    """The gold set has carried these tokens all along; nothing read them.

    Tokens may CONFIRM but never CONDEMN. A hit is strong evidence and costs
    nothing; a miss is weak evidence of anything, because the fact can be
    phrased a dozen ways the token author did not anticipate. Measured on the
    first real run: token-scored cases ran 13.5 points below judge-scored ones,
    the signature of a harsh scorer rather than a wrong reader.
    """

    def test_all_required_tokens_present_is_correct(self):
        v, _ = wre.token_verdict("Deviation 승인 대기입니다", ["Deviation"], [])
        self.assertEqual(v, "CORRECT")

    def test_thousands_separator_does_not_fail_a_right_answer(self):
        # "KEC 기준 직류 최대 전압은 1,500V입니다" against token "1500" was
        # scored INCORRECT before normalization.
        v, _ = wre.token_verdict("최대 전압은 1,500V입니다", ["1500"], [])
        self.assertEqual(v, "CORRECT")

    def test_spacing_and_punctuation_are_folded(self):
        v, _ = wre.token_verdict("참여 종결 되었습니다", ["참여종결"], [])
        self.assertEqual(v, "CORRECT")

    def test_a_token_miss_defers_to_the_judge_instead_of_condemning(self):
        verdict, why = wre.token_verdict("전혀 다른 이야기", ["Deviation"], [])
        self.assertIsNone(verdict)
        self.assertIn("deferred", why)

    def test_pipe_means_either_alternative_satisfies(self):
        for answer in ("참여 종결되었습니다", "계약 유효 상태"):
            with self.subTest(answer=answer):
                v, _ = wre.token_verdict(answer, ["참여 종결|계약 유효"], [])
                self.assertEqual(v, "CORRECT")

    def test_forbidden_token_still_condemns(self):
        # must_not is an explicit prohibition, not a guess at phrasing.
        v, why = wre.token_verdict("취소되었습니다", [], ["취소"])
        self.assertEqual(v, "INCORRECT")
        self.assertIn("forbidden", why)

    def test_no_tokens_defers_to_the_judge(self):
        verdict, _ = wre.token_verdict("무슨 답이든", [], [])
        self.assertIsNone(verdict)


class JudgeReferenceTests(unittest.TestCase):
    """The judge must see the reference CONTENT, not a list of paths.

    It was handed `gold_paths` and said so in its own verdicts — "Reference
    missing; cannot verify specific claims like 191.78억" — which means the 81
    token-less cases were graded on a guess.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        (self.root / "프로젝트").mkdir()
        (self.root / "프로젝트" / "a.md").write_text(
            "공급가액 191.78억", encoding="utf-8")

    def test_reference_carries_the_page_text(self):
        captured = {}

        def fake_call(model, messages, **kw):
            captured["user"] = messages[-1]["content"]
            return "CORRECT 일치"

        original = wre.call_model
        wre.call_model = fake_call
        self.addCleanup(lambda: setattr(wre, "call_model", original))
        label, _ = wre.judge_case(
            {"question": "공급가액?", "answer": "191.78억",
             "gold_paths": ["프로젝트/a"]},
            "any-model", str(self.root))
        self.assertEqual(label, "CORRECT")
        self.assertIn("공급가액 191.78억", captured["user"])
        self.assertNotIn("gold_paths", captured["user"])


class ReferenceIndependenceTests(unittest.TestCase):
    """The oracle run is only judgeable against a DERIVED reference.

    There the reader is served the gold pages and the judge was served the same
    gold pages, so a verdict could only ask "is this consistent with these
    pages" — which a wrong answer drawn from those same pages passes. Deriving
    the answer first, from the pages alone, gives the judge something to
    compare against rather than something to check consistency with.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = pathlib.Path(self.tmp.name)
        (self.root / "프로젝트").mkdir()
        (self.root / "프로젝트" / "a.md").write_text(
            "공급가액 191.78억", encoding="utf-8")

    def _capture(self, reference):
        seen = {}

        def fake_call(model, messages, **kw):
            seen["user"] = messages[-1]["content"]
            seen["system"] = messages[0]["content"]
            return "CORRECT 동일"

        original = wre.call_model
        wre.call_model = fake_call
        self.addCleanup(lambda: setattr(wre, "call_model", original))
        wre.judge_case({"question": "공급가액?", "answer": "191.78억",
                        "gold_paths": ["프로젝트/a"]},
                       "m", str(self.root), reference)
        return seen

    def test_a_reference_answer_replaces_the_source_pages(self):
        seen = self._capture("공급가액은 191.78억이다.")
        self.assertIn("기준 답", seen["user"])
        self.assertNotIn("기준 페이지", seen["user"])

    def test_without_a_reference_it_falls_back_to_the_pages(self):
        seen = self._capture("")
        self.assertIn("기준 페이지", seen["user"])

    def test_derive_sees_only_the_pages_and_never_a_candidate(self):
        seen = {}

        def fake_call(model, messages, **kw):
            seen["user"] = messages[-1]["content"]
            return "공급가액은 191.78억이다."

        original = wre.call_model
        wre.call_model = fake_call
        self.addCleanup(lambda: setattr(wre, "call_model", original))
        out = wre.derive_reference({"question": "공급가액?",
                                    "gold_paths": ["프로젝트/a"]},
                                   str(self.root), "m")
        self.assertIn("191.78억", out)
        self.assertIn("공급가액 191.78억", seen["user"])
        self.assertNotIn("채점", seen["user"])


class ParseDirectiveTests(unittest.TestCase):
    def test_read_and_answer(self):
        self.assertEqual(wre.parse_directive("READ 프로젝트/a"),
                         ("READ", "프로젝트/a"))
        verb, arg = wre.parse_directive("ANSWER 승인 대기입니다")
        self.assertEqual((verb, arg), ("ANSWER", "승인 대기입니다"))

    def test_markdown_wrapping_survives(self):
        self.assertEqual(wre.parse_directive("**READ** 프로젝트/a"),
                         ("READ", "프로젝트/a"))

    def test_answer_keeps_the_body_past_the_first_line(self):
        _, arg = wre.parse_directive("ANSWER 첫 줄\n둘째 줄")
        self.assertIn("둘째 줄", arg)

    def test_unprefixed_reply_is_an_answer_not_a_failure(self):
        verb, arg = wre.parse_directive("승인 대기 상태입니다")
        self.assertEqual(verb, "ANSWER")
        self.assertEqual(arg, "승인 대기 상태입니다")

    def test_bare_read_without_a_path_does_not_spend_a_slot(self):
        verb, _ = wre.parse_directive("READ")
        self.assertEqual(verb, "ANSWER")


class RenderPageTests(unittest.TestCase):
    def test_long_page_is_clipped_but_still_substantial(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        page = pathlib.Path(tmp.name) / "big.md"
        page.write_text("가" * 20000, encoding="utf-8")
        out = wre.render_page(page)
        self.assertIn("페이지 잘림", out)
        self.assertLessEqual(len(out.encode()), wre.PAGE_BYTES + 200)


if __name__ == "__main__":
    unittest.main()


BLOCK = (
    "서버가 위키를 미리 검색했다. 아래 근거만 사용하라.\n"
    '- source=wiki ref="프로젝트/알파/메일분석/a.md" confidence=high age=1d\n'
    "  알파 계약 금액은 1,500만원이다.\n"
    '- source=wiki ref="프로젝트/알파/대표.md" confidence=medium age=2d\n'
    "  알파 프로젝트 개요.\n"
    '- source=wiki ref="프로젝트/베타/대표.md" confidence=low age=9d\n'
    "  베타는 무관하다.\n"
    '- source=wiki ref="인물/김철수.md" confidence=low age=9d\n'
    "  김철수 소개.\n"
)
GOLD = ["프로젝트/알파"]


class AblationTests(unittest.TestCase):
    def test_preamble_is_never_dropped(self):
        # The preamble is the reader's instructions, not its evidence — dropping
        # it would change what the run is measuring.
        for mode in ("cited", "random"):
            kept, _, _ = wre.ablate_block(BLOCK, GOLD, mode, seed=1)
            self.assertIn("아래 근거만 사용하라", kept, mode)

    def test_cited_drops_every_gold_row_and_only_those(self):
        kept, dropped, _ = wre.ablate_block(BLOCK, GOLD, "cited", seed=1)
        self.assertEqual(len(dropped), 2, dropped)
        self.assertNotIn("프로젝트/알파", kept)
        self.assertIn("프로젝트/베타/대표.md", kept)
        self.assertIn("인물/김철수.md", kept)
        # The body under a dropped header goes with it, not just the ref line.
        self.assertNotIn("1,500만원", kept)

    def test_random_matches_bytes_not_rows_and_never_takes_gold(self):
        # The control pays the same PRICE, which is bytes of context — matching
        # row count let a control remove a third of what the treatment did, and
        # then a small treatment effect is unreadable.
        kept, dropped, stat = wre.ablate_block(BLOCK, GOLD, "random", seed=1)
        self.assertTrue(stat["matched"], stat)
        self.assertGreaterEqual(stat["bytes"], 0.75 * stat["target_bytes"])
        self.assertIn("1,500만원", kept, "gold evidence must survive the control")
        for ref in dropped:
            self.assertFalse(ref.startswith("프로젝트/알파"), ref)

    def test_random_arm_is_seed_stable(self):
        a, da, _ = wre.ablate_block(BLOCK, GOLD, "random", seed=7)
        b, db, _ = wre.ablate_block(BLOCK, GOLD, "random", seed=7)
        self.assertEqual(da, db)
        self.assertEqual(a, b)

    def test_none_is_the_identity(self):
        kept, dropped, _ = wre.ablate_block(BLOCK, GOLD, "none", seed=1)
        self.assertEqual(kept, BLOCK)
        self.assertEqual(dropped, [])

    def test_a_block_that_cannot_supply_a_control_says_so(self):
        # A block that is nearly all gold cannot pay the same price. It must
        # report matched=False so the analysis EXCLUDES it rather than averaging
        # an under-powered control into the result — that contamination made the
        # first n=60 run read a +8.3pp control effect, i.e. pure noise.
        block = (
            "머리말\n"
            '- source=wiki ref="프로젝트/알파/a.md" confidence=high age=1d\n'
            '- source=wiki ref="프로젝트/알파/b.md" confidence=high age=1d\n'
            '- source=wiki ref="인물/김.md" confidence=low age=1d\n'
        )
        _, _, stat = wre.ablate_block(block, GOLD, "random", seed=3)
        self.assertFalse(stat["matched"], stat)

    def test_split_keeps_every_byte(self):
        rows = wre.split_evidence_rows(BLOCK)
        self.assertEqual("".join(c for _, c in rows), BLOCK)
        self.assertEqual([r for r, _ in rows][0], None, "preamble carries no ref")


class RecipeTests(unittest.TestCase):
    def _recipe(self, **over):
        base = {
            "reader": "dsv4", "judge": "dsv4", "oracle": False, "ablate": "none",
            "seed": 1, "read_budget": 4,
            "snapshot": {"path": "s.jsonl", "sha": "aaaa", "cases": 60},
            "corpus": {"pages": 916, "paths_sha": "bbbb"},
            "at": "2026-09-02T00:00:00+00:00",
        }
        for k, v in over.items():
            if "." in k:
                outer, inner = k.split(".")
                base[outer] = dict(base[outer]); base[outer][inner] = v
            else:
                base[k] = v
        return base

    def test_identical_recipes_compare(self):
        self.assertEqual(wre.recipe_mismatches(self._recipe(), self._recipe()), [])

    def test_a_different_reader_is_a_different_ruler(self):
        # The 2026-08-29 lesson: an oracle ceiling measured under one reader is
        # not a ceiling for another, and the delta reads as a breakthrough.
        d = wre.recipe_mismatches(self._recipe(reader="kimi"), self._recipe())
        self.assertEqual(len(d), 1)
        self.assertIn("reader", d[0])

    def test_judge_corpus_and_snapshot_all_count(self):
        for key in ("judge", "corpus.paths_sha", "snapshot.sha"):
            with self.subTest(key):
                d = wre.recipe_mismatches(self._recipe(**{key: "zzz"}), self._recipe())
                self.assertEqual(len(d), 1, d)

    def test_oracle_and_ablation_arms_are_not_comparable_to_the_base(self):
        for key, val in (("oracle", True), ("ablate", "cited")):
            with self.subTest(key):
                self.assertTrue(wre.recipe_mismatches(self._recipe(**{key: val}), self._recipe()))

    def test_provenance_alone_does_not_break_comparability(self):
        # A different wall clock, or a smaller --limit, is a wider error bar the
        # summary already shows — not a different measurement.
        same = wre.recipe_mismatches(
            self._recipe(at="2027-01-01T00:00:00+00:00", **{"snapshot.cases": 30}),
            self._recipe())
        self.assertEqual(same, [])

    def test_corpus_fingerprint_notices_an_emptied_copy(self):
        # 2026-08-29: a /tmp cleanup left one page and every score read 0.0.
        with tempfile.TemporaryDirectory() as d:
            root = pathlib.Path(d)
            (root / "a.md").write_text("a", encoding="utf-8")
            (root / "sub").mkdir()
            (root / "sub" / "b.md").write_text("b", encoding="utf-8")
            full = wre.corpus_fingerprint(str(root))
            self.assertEqual(full["pages"], 2)
            (root / "sub" / "b.md").unlink()
            emptied = wre.corpus_fingerprint(str(root))
            self.assertEqual(emptied["pages"], 1)
            self.assertNotEqual(full["paths_sha"], emptied["paths_sha"])
