#!/usr/bin/env python3
"""Pathway attribution for the retention proxy (PAST-Bench, arXiv:2608.04003).

A confirm rate says a gain happened; it does not say the gain came through the
intended save → retrieve → update pathway. These tests pin that separation:
the split must reflect whether post-evolve turns actually EXERCISED the evolved
skill, and it must never move the ratcheted score.
"""

from __future__ import annotations

import json
import tempfile
import time
import unittest
from pathlib import Path

from rsi_bench.ledgers import PathwayWindow, load_pathway_window
from rsi_bench.model import Evidence  # noqa: F401  (import guards module wiring)
from rsi_bench.utility import _score_retention
from rsi_bench.ledgers import GenesisWindow, WatchWindow


def _now_ms() -> int:
    return int(time.time() * 1000)


def _write(root: Path, evolved_at: int, uses: list[dict]) -> None:
    root.mkdir(parents=True, exist_ok=True)
    (root / "skill_genesis_log.jsonl").write_text(
        json.dumps({"type": "evolved", "skillName": "kb", "createdAt": evolved_at}) + "\n",
        encoding="utf-8",
    )
    (root / "skill_usage.jsonl").write_text(
        "".join(json.dumps(u) + "\n" for u in uses), encoding="utf-8"
    )


def _use(used_at: int, exercised: str | None = None, *, source: str = "real", skill: str = "kb") -> dict:
    row = {"skillName": skill, "source": source, "usedAt": used_at, "success": True}
    if exercised is not None:
        row["exercised"] = exercised
    return row


class PathwayWindowTests(unittest.TestCase):
    def test_splits_post_evolve_uses_by_whether_the_skill_ran(self) -> None:
        now = _now_ms()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write(root, now - 3600_000, [
                _use(now - 1800_000, "yes"),
                _use(now - 1700_000, "no"),
                _use(now - 1600_000, "no"),
                _use(now - 1500_000),  # legacy row: no attribution
            ])
            w = load_pathway_window(root)
        self.assertEqual((w.post_uses, w.exercised, w.unexercised, w.unattributed), (4, 1, 2, 1))
        self.assertAlmostEqual(w.support_rate, 1 / 3)
        self.assertAlmostEqual(w.coverage, 0.75)
        self.assertEqual(w.skills, 1)

    def test_ignores_pre_evolve_and_non_real_records(self) -> None:
        now = _now_ms()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write(root, now - 3600_000, [
                _use(now - 7200_000, "yes"),                     # before the evolve
                _use(now - 1800_000, "yes", source="workout"),   # synthetic lane
                _use(now - 1700_000, "yes", skill="other"),      # skill never evolved
                _use(now - 1600_000, "yes"),
            ])
            w = load_pathway_window(root)
        self.assertEqual(w.post_uses, 1)
        self.assertEqual(w.exercised, 1)

    def test_no_evolves_yields_empty_window(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "skill_usage.jsonl").write_text(json.dumps(_use(_now_ms(), "yes")) + "\n", encoding="utf-8")
            w = load_pathway_window(root)
        self.assertEqual(w.post_uses, 0)
        self.assertIsNone(w.support_rate)


class RetentionEvidenceTests(unittest.TestCase):
    """The split explains the score; it must never change it."""

    def _health(self, rate: float) -> dict:
        return {"confirm_rate": rate, "resolved_evolves_7d": 6}

    def test_pathway_never_moves_the_score(self) -> None:
        genesis, watch = GenesisWindow(), WatchWindow()
        baseline, _, _ = _score_retention(genesis, self._health(0.9), watch)
        unsupported = PathwayWindow(post_uses=10, exercised=1, unexercised=9)
        scored, evidence, findings = _score_retention(genesis, self._health(0.9), watch, unsupported)
        self.assertEqual(scored, baseline)
        self.assertIn("pathwaySupport=0.10", evidence.detail)
        self.assertEqual(len(findings), 1, "an unexplained confirm rate must surface a finding")
        self.assertEqual(findings[0].pillar, "retention-proxy")

    def test_supported_confirm_rate_raises_no_finding(self) -> None:
        genesis, watch = GenesisWindow(), WatchWindow()
        supported = PathwayWindow(post_uses=10, exercised=9, unexercised=1)
        _, evidence, findings = _score_retention(genesis, self._health(0.9), watch, supported)
        self.assertEqual(findings, [])
        self.assertIn("pathwaySupport=0.90", evidence.detail)

    def test_thin_or_uncovered_attribution_stays_silent(self) -> None:
        genesis, watch = GenesisWindow(), WatchWindow()
        # Two attributed rows is below MIN_RESOLVED_FOR_HARD — too thin to accuse.
        thin = PathwayWindow(post_uses=2, exercised=0, unexercised=2)
        _, _, findings = _score_retention(genesis, self._health(0.9), watch, thin)
        self.assertEqual(findings, [])
        # Mostly legacy rows: coverage below half cannot support a verdict either.
        uncovered = PathwayWindow(post_uses=20, exercised=1, unexercised=3, unattributed=16)
        _, _, findings = _score_retention(genesis, self._health(0.9), watch, uncovered)
        self.assertEqual(findings, [])

    def test_bootstrap_state_still_reports_the_pathway(self) -> None:
        _, evidence, _ = _score_retention(GenesisWindow(), {}, WatchWindow(), PathwayWindow())
        self.assertEqual(evidence.status, "bootstrap")
        self.assertIn("pathway=no-post-uses", evidence.detail)


if __name__ == "__main__":
    unittest.main()
