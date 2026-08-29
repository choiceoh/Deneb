"""Tests for the CodeGraph blast-radius brief attached to PR bodies."""

from __future__ import annotations

import unittest

from impact_brief import (
    MARK_END,
    MARK_START,
    Brief,
    SymbolImpact,
    is_test_path,
    render_markdown,
    splice_into_body,
    symbols_for_lines,
)


class SymbolAttributionTests(unittest.TestCase):
    SYMBOLS = [(10, "Alpha", "function"), (40, "Beta", "function"), (80, "Gamma", "variable")]

    def test_line_inside_a_definition(self) -> None:
        self.assertEqual(symbols_for_lines(self.SYMBOLS, [45]), [("Beta", "function", 40)])

    def test_line_before_the_first_definition_is_dropped(self) -> None:
        """Imports and package headers belong to no symbol."""
        self.assertEqual(symbols_for_lines(self.SYMBOLS, [3]), [])

    def test_multiple_lines_in_one_symbol_report_it_once(self) -> None:
        self.assertEqual(
            symbols_for_lines(self.SYMBOLS, [41, 42, 43]), [("Beta", "function", 40)]
        )

    def test_result_is_ordered_by_definition_line(self) -> None:
        got = symbols_for_lines(self.SYMBOLS, [85, 12])
        self.assertEqual([name for name, _, _ in got], ["Alpha", "Gamma"])

    def test_no_symbol_map_yields_nothing(self) -> None:
        self.assertEqual(symbols_for_lines([], [5]), [])


class TestPathTests(unittest.TestCase):
    def test_go_test_file(self) -> None:
        self.assertTrue(is_test_path("gateway-go/internal/a/b_test.go"))

    def test_kotlin_test_file(self) -> None:
        self.assertTrue(is_test_path("client-android/app/src/ChatViewModelTest.kt"))

    def test_production_file(self) -> None:
        self.assertFalse(is_test_path("gateway-go/internal/a/b.go"))


class SpliceTests(unittest.TestCase):
    BRIEF = f"{MARK_START}\nbody v1\n{MARK_END}"
    BRIEF2 = f"{MARK_START}\nbody v2\n{MARK_END}"

    def test_appends_to_an_existing_body(self) -> None:
        out = splice_into_body("## Summary\n\nchanged a thing", self.BRIEF)
        self.assertIn("## Summary", out)
        self.assertIn("body v1", out)

    def test_replaces_instead_of_duplicating(self) -> None:
        once = splice_into_body("## Summary", self.BRIEF)
        twice = splice_into_body(once, self.BRIEF2)
        self.assertEqual(twice.count(MARK_START), 1)
        self.assertIn("body v2", twice)
        self.assertNotIn("body v1", twice)
        self.assertIn("## Summary", twice)

    def test_empty_body(self) -> None:
        self.assertTrue(splice_into_body("", self.BRIEF).startswith(MARK_START))


class RenderTests(unittest.TestCase):
    def test_degradations_are_visible_not_silent(self) -> None:
        """A brief that could not see the graph must not read like one that did."""
        brief = Brief("origin/main...HEAD", ["a.go"], [], [], ["CodeGraph 인덱스 없음"])
        out = render_markdown(brief)
        self.assertIn("CodeGraph 인덱스 없음", out)
        self.assertTrue(out.startswith(MARK_START))
        self.assertTrue(out.rstrip().endswith(MARK_END))

    def test_widest_blast_radius_is_called_out(self) -> None:
        wide = SymbolImpact(
            name="Hub",
            kind="type",
            file="a.go",
            line=10,
            affected_outside=[{"name": "Caller", "file": "b.go"}],
            affected_tests=["TestHub"],
            total_affected=2,
        )
        narrow = SymbolImpact(name="leaf", kind="function", file="a.go", line=90)
        out = render_markdown(Brief("r", ["a.go"], ["a.go"], [wide, narrow], []))
        self.assertIn("가장 넓은 파급", out)
        self.assertIn("`Hub`", out)
        self.assertIn("`Caller`", out)

    def test_untested_edited_symbols_are_named(self) -> None:
        bare = SymbolImpact(name="lonely", kind="function", file="a.go", line=1)
        out = render_markdown(Brief("r", ["a.go"], ["a.go"], [bare], []))
        self.assertIn("테스트가 닿지 않는 편집 심볼", out)
        self.assertIn("`lonely`", out)

    def test_truncation_is_disclosed(self) -> None:
        brief = Brief("r", ["a.go"], ["a.go"], [SymbolImpact("x", "function", "a.go", 1)], [], 7)
        self.assertIn("7개", render_markdown(brief))


if __name__ == "__main__":
    unittest.main()
