#!/usr/bin/env python3
"""Confidence → recall calibration (Personalization Mirage, arXiv:2608.04570).

The verdict is the whole product: it decides whether the dream-quality
Confidence axis is earned, noise, or actively inverted. These pin each verdict
and the two ways the join can silently lie (a missing band, a malformed index).
"""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from wiki_confidence_calibration import calibrate, render

HEADER = "id\tpath\ttitle\tsummary\ttags\timportance\tupdated\ttype\tconfidence\tbacklinks\tcreated\trelated"


def _row(path: str, confidence: str) -> str:
    return "\t".join(["id", path, "t", "s", "", "0.5", "", "note", confidence, "", "", ""])


def _corpus(
    tmp: Path,
    pages: list[tuple[str, str]],
    hits: list[str],
    ledger_lines: list[str] | None = None,
    write_ledger: bool = True,
) -> Path:
    (tmp / "index.md").write_text(
        "# index\n\n" + HEADER + "\n" + "\n".join(_row(p, c) for p, c in pages) + "\n",
        encoding="utf-8",
    )
    if write_ledger:
        if ledger_lines is None:
            ledger_lines = [json.dumps({"path": p, "at": 1}) for p in hits]
        (tmp / ".recall-hits.jsonl").write_text(
            "".join(line + "\n" for line in ledger_lines), encoding="utf-8"
        )
    return tmp


def _pages(band: str, n: int) -> list[tuple[str, str]]:
    return [(f"{band}/{i}.md", band) for i in range(n)]


class CalibrationTests(unittest.TestCase):
    def test_monotone_when_confidence_predicts_recall(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = _corpus(
                Path(tmp),
                _pages("high", 4) + _pages("medium", 4) + _pages("low", 4),
                ["high/0.md", "high/1.md", "high/2.md", "medium/0.md", "medium/1.md"],
            )
            cal = calibrate(root)
        self.assertEqual(cal.verdict, "monotone")
        self.assertAlmostEqual(cal.rate("high"), 0.75)
        self.assertAlmostEqual(cal.rate("low"), 0.0)

    def test_inverted_is_named_and_explained(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = _corpus(
                Path(tmp),
                _pages("high", 4) + _pages("medium", 4) + _pages("low", 4),
                ["low/0.md", "low/1.md", "low/2.md", "medium/0.md"],
            )
            cal = calibrate(root)
        self.assertEqual(cal.verdict, "inverted")
        self.assertIn("INVERTED", render(cal))

    def test_flat_when_bands_are_indistinguishable(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = _corpus(
                Path(tmp),
                _pages("high", 4) + _pages("medium", 4) + _pages("low", 4),
                ["high/0.md", "medium/0.md", "low/0.md"],
            )
            cal = calibrate(root)
        self.assertEqual(cal.verdict, "flat")
        self.assertIn("FLAT", render(cal))

    def test_single_band_is_unmeasured_not_a_verdict(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = _corpus(Path(tmp), _pages("high", 4), ["high/0.md"])
            cal = calibrate(root)
        self.assertEqual(cal.verdict, "unmeasured")

    def test_unlabeled_pages_are_reported_but_never_decide_the_verdict(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = _corpus(
                Path(tmp),
                _pages("high", 4) + _pages("low", 4) + [("unset/0.md", ""), ("unset/1.md", "")],
                ["high/0.md", "high/1.md", "high/2.md", "unset/0.md", "unset/1.md"],
            )
            cal = calibrate(root)
        self.assertEqual(cal.verdict, "monotone")
        self.assertEqual(cal.bands["unset"].pages, 2)
        self.assertAlmostEqual(cal.rate("unset"), 1.0)

    def test_missing_or_malformed_corpus_degrades_to_unmeasured(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            cal = calibrate(Path(tmp))  # no index, no ledger
        self.assertEqual((cal.pages, cal.verdict), (0, "unmeasured"))

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "index.md").write_text("no header here\njust\tprose\n", encoding="utf-8")
            (root / ".recall-hits.jsonl").write_text("{not json\n", encoding="utf-8")
            cal = calibrate(root)
        self.assertEqual((cal.pages, cal.hit_paths), (0, 0))

    def test_missing_ledger_is_unmeasured_not_flat(self) -> None:
        # A fresh install has an index but no recall ledger yet. Every band
        # reads rate 0.0, which the verdict logic would call "flat" — claiming
        # the confidence label measures nothing from data that measures
        # nothing at all.
        with tempfile.TemporaryDirectory() as tmp:
            root = _corpus(
                Path(tmp), _pages("high", 4) + _pages("low", 4), [], write_ledger=False
            )
            cal = calibrate(root)
        self.assertFalse(cal.ledger_present)
        self.assertEqual(cal.verdict, "unmeasured")
        self.assertIn("ledger missing", render(cal))

    def test_zero_injection_ledger_is_unmeasured(self) -> None:
        # A ledger that exists but records zero injections is also no
        # observation, for the same reason.
        with tempfile.TemporaryDirectory() as tmp:
            root = _corpus(Path(tmp), _pages("high", 4) + _pages("low", 4), [])
            cal = calibrate(root)
        self.assertTrue(cal.ledger_present)
        self.assertEqual(cal.verdict, "unmeasured")

    def test_read_and_cite_events_do_not_count_as_recall(self) -> None:
        # read/cite are the model USING a page (it opened it, it cited it) —
        # not the preflight serving it. Counting them would inflate bands
        # recall never surfaced.
        with tempfile.TemporaryDirectory() as tmp:
            root = _corpus(
                Path(tmp),
                _pages("high", 4) + _pages("low", 4),
                [],
                ledger_lines=[
                    json.dumps({"path": "high/0.md", "at": 1, "event": "read"}),
                    json.dumps({"path": "high/1.md", "at": 1, "event": "cite"}),
                    json.dumps({"path": "low/0.md", "at": 1, "event": "inject"}),
                ],
            )
            cal = calibrate(root)
        self.assertEqual(cal.hit_paths, 1)
        self.assertEqual(cal.bands["high"].recalled, 0)
        self.assertEqual(cal.bands["high"].hits, 0)
        self.assertEqual(cal.bands["low"].recalled, 1)

    def test_non_dict_ledger_lines_are_skipped_not_fatal(self) -> None:
        # Valid JSON that is not an object (null, a string, an array) must
        # degrade like a malformed line, not abort the whole advisory audit.
        with tempfile.TemporaryDirectory() as tmp:
            root = _corpus(
                Path(tmp),
                _pages("high", 4) + _pages("low", 4),
                [],
                ledger_lines=["null", '"a-string"', "[1,2]", "{not json", json.dumps({"path": "high/0.md", "at": 1})],
            )
            cal = calibrate(root)
        self.assertEqual(cal.hit_paths, 1)
        self.assertEqual(cal.bands["high"].recalled, 1)

    def test_nonmonotonic_is_adverse_evidence_not_unmeasured(self) -> None:
        # high 0.5, medium 0.0, low 0.5: a spread wide enough to matter that
        # fits neither direction. Folding it into "unmeasured" would silently
        # keep a label the data argues against.
        with tempfile.TemporaryDirectory() as tmp:
            root = _corpus(
                Path(tmp),
                _pages("high", 4) + _pages("medium", 4) + _pages("low", 4),
                ["high/0.md", "high/1.md", "low/0.md", "low/1.md"],
            )
            cal = calibrate(root)
        self.assertEqual(cal.verdict, "nonmonotonic")
        self.assertIn("NON-MONOTONIC", render(cal))


if __name__ == "__main__":
    unittest.main()
