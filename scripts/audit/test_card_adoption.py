"""Unit tests for the deneb-ui card-adoption audit."""

from __future__ import annotations

import io
import unittest

from card_adoption import main, summarize, tally

AUTHORED = "12:00:00 │ deneb-ui card authored session={s} blocks=1 runes=900"
MISS = "12:00:00 │ deneb-ui adoption miss — structured answer without a card (heuristic) session={s} runes=900"
REJECTED = '12:00:00 │ deneb-ui card rejected before delivery session={s} block=0 reason=schema_issues'


def _lines(spec: list[tuple[str, str]]) -> list[str]:
    tmpl = {"authored": AUTHORED, "miss": MISS, "rejected": REJECTED}
    return [tmpl[kind].format(s=session) for session, kind in spec]


class TallyTests(unittest.TestCase):
    def test_counts_each_signal_per_session(self) -> None:
        per = tally(
            _lines(
                [
                    ("client:main", "authored"),
                    ("client:main", "miss"),
                    ("client:main", "miss"),
                    ("system:mailpoll", "authored"),
                    ("system:mailpoll", "rejected"),
                ]
            )
        )
        self.assertEqual(per["client:main"], {"authored": 1, "miss": 2, "rejected": 0})
        self.assertEqual(per["system:mailpoll"], {"authored": 1, "miss": 0, "rejected": 1})

    def test_strips_ansi_colour_from_journal_lines(self) -> None:
        raw = "\x1b[2m12:00:00\x1b[0m │ \x1b[1mdeneb-ui card authored\x1b[0m session=client:main blocks=1"
        self.assertEqual(tally([raw])["client:main"]["authored"], 1)

    def test_unrelated_lines_are_ignored(self) -> None:
        self.assertEqual(tally(["12:00:00 │ tool complete name=wiki session=client:main"]), {})

    def test_record_without_session_is_bucketed_not_dropped(self) -> None:
        per = tally(["12:00:00 │ deneb-ui card authored blocks=1"])
        self.assertEqual(per["(unknown)"]["authored"], 1)


class SummaryTests(unittest.TestCase):
    def test_separates_operator_automated_and_log_sessions(self) -> None:
        summary = summarize(
            tally(
                _lines(
                    [
                        ("client:main", "authored"),
                        ("client:main", "miss"),
                        ("client:main", "miss"),
                        ("client:main", "miss"),
                        ("system:mailpoll", "authored"),
                        ("system:mailpoll", "authored"),
                        ("system:mailpoll", "miss"),
                        ("client:main:dream", "miss"),
                    ]
                )
            )
        )
        groups = summary["groups"]
        # The dream session is a background journal, not an answering surface —
        # folding it into the operator bucket would drag the number that matters.
        self.assertEqual(groups["operator"]["adoption_pct"], 25.0)
        self.assertEqual(groups["automated"]["adoption_pct"], 66.7)
        self.assertEqual(groups["log"]["adoption_pct"], 0.0)

    def test_adoption_is_none_when_no_turns_qualify(self) -> None:
        summary = summarize(tally(_lines([("client:main", "rejected")])))
        self.assertIsNone(summary["groups"]["operator"]["adoption_pct"])

    def test_empty_window_yields_no_groups(self) -> None:
        self.assertEqual(summarize(tally([]))["groups"], {})


class CLITests(unittest.TestCase):
    def _run(self, lines: list[str], argv: list[str]) -> str:
        import sys

        buf, stdin = io.StringIO(), sys.stdin
        sys.stdin = io.StringIO("\n".join(lines))
        try:
            self.assertEqual(main([*argv, "--stdin"], stream=buf), 0)
        finally:
            sys.stdin = stdin
        return buf.getvalue()

    def test_text_output_carries_the_machine_readable_footer(self) -> None:
        out = self._run(
            _lines([("client:main", "authored"), ("client:main", "miss")]),
            [],
        )
        self.assertIn("DENEB_CARD_ADOPTION operator=50.0", out)

    def test_json_output_is_parseable(self) -> None:
        import json

        out = self._run(_lines([("client:main", "authored")]), ["--json"])
        self.assertEqual(json.loads(out)["groups"]["operator"]["authored"], 1)

    def test_empty_journal_does_not_crash(self) -> None:
        self.assertIn("no card-health records", self._run([], []))


if __name__ == "__main__":
    unittest.main()
