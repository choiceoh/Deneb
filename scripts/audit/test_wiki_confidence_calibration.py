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


def _corpus(tmp: Path, pages: list[tuple[str, str]], hits: list[str]) -> Path:
    (tmp / "index.md").write_text(
        "# index\n\n" + HEADER + "\n" + "\n".join(_row(p, c) for p, c in pages) + "\n",
        encoding="utf-8",
    )
    (tmp / ".recall-hits.jsonl").write_text(
        "".join(json.dumps({"path": p, "at": 1}) + "\n" for p in hits), encoding="utf-8"
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


if __name__ == "__main__":
    unittest.main()
