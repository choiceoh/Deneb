#!/usr/bin/env python3
"""Unit tests for Health Bench 3.0 composite math and calibration transforms."""

from __future__ import annotations

import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

from health_v3.baseline import BaselineRegressionError, check, snapshot, update
from health_v3.model import (
    DOMAIN_WEIGHTS,
    Domain,
    Evidence,
    Metric,
    Report,
    geometric_composite,
)
from health_v3.runtime import score_signals, write_cache, load_cache
from health_v3.structure import (
    _AI_EXTRA_TIGHTEN,
    _CHANGE_BLAST_SCALE,
    _DELIVERY_SCALE,
    _TEST_TRUTH_SCALE,
)
from health_v3 import model as v3_model
import runtime_health as rh


def _metric(mid: str, weight: float, score: float) -> Metric:
    return Metric(mid, mid, weight, score, "test")


def _domain(did: str, metrics: list[Metric], *, ratcheted: bool = True) -> Domain:
    return Domain(
        id=did,
        title=did.title(),
        weight=DOMAIN_WEIGHTS[did],
        metrics=metrics,
        evidence=[Evidence(f"{did}-ok", "measured", "ok", required=False)],
        ratcheted=ratcheted,
    )


def _report(structure: float, runtime: float, fitness: float) -> Report:
    return Report(
        profile="fast",
        revision="deadbeef",
        domains=[
            _domain(
                "structure",
                [
                    _metric("change-blast", 20, structure),
                    _metric("boundary-contracts", 14, structure),
                    _metric("ai-maintainability", 16, structure),
                    _metric("complexity-safety", 10, structure),
                    _metric("test-truth", 16, structure),
                    _metric("test-craft", 8, structure),
                    _metric("delivery-proof", 10, structure),
                    _metric("dead-surface", 6, structure),
                ],
            ),
            _domain(
                "runtime",
                [
                    _metric("stability", 18, runtime),
                    _metric("error-rate", 16, runtime),
                    _metric("llm-serving", 16, runtime),
                    _metric("turn-reliability", 16, runtime),
                    _metric("tool-reliability", 14, runtime),
                    _metric("latency", 20, runtime),
                ],
            ),
            _domain(
                "fitness",
                [
                    _metric("live-delta", 30, fitness),
                    _metric("trend-28d", 25, fitness),
                    _metric("finding-land", 25, fitness),
                    _metric("feed-card", 20, fitness),
                ],
                ratcheted=False,
            ),
        ],
    )


class GeometricCompositeTests(unittest.TestCase):
    def test_worksheet_targets_land_near_fifty(self) -> None:
        overall = geometric_composite({"structure": 50.4, "runtime": 58.5, "fitness": 43.0})
        self.assertTrue(45.0 <= overall <= 55.0, overall)
        self.assertAlmostEqual(overall, 50.7, places=1)

    def test_weak_domain_cannot_be_hidden(self) -> None:
        high = geometric_composite({"structure": 90.0, "runtime": 90.0, "fitness": 90.0})
        dragged = geometric_composite({"structure": 90.0, "runtime": 90.0, "fitness": 30.0})
        self.assertLess(dragged, high)
        # Fitness is only 20% of the geo mean — still pulls ~90 down into the 70s.
        self.assertLess(dragged, 75.0)
        self.assertGreater(high - dragged, 10.0)

    def test_zero_domain_collapses(self) -> None:
        self.assertEqual(
            geometric_composite({"structure": 80.0, "runtime": 0.0, "fitness": 80.0}),
            0.0,
        )


class StructureCalibrationTests(unittest.TestCase):
    def test_change_blast_scale_maps_current_locality_band(self) -> None:
        raw = (55.0 + 56.6) / 2.0
        self.assertAlmostEqual(raw * _CHANGE_BLAST_SCALE, 32.0, places=1)

    def test_delivery_and_test_scales(self) -> None:
        self.assertAlmostEqual(100.0 * _DELIVERY_SCALE, 42.0, places=1)
        self.assertAlmostEqual(92.8 * _TEST_TRUTH_SCALE, 65.0, places=0)
        self.assertLess(_AI_EXTRA_TIGHTEN, 1.0)


class RuntimeBarTests(unittest.TestCase):
    def test_tight_bars_score_live_like_window_mid_band(self) -> None:
        # Approximate the 2026-07-15 live window shape.
        signals = rh.Signals(
            runs=771,
            timeout_runs=22,
            agent_ms=[22_000] * 385 + [141_000] * 386,
            tool_calls=2003,
            tool_call_reports=771,
            tool_errors=25,
            crashes=0,
            llm_hard=7,
            other_errors=97,
            days=8.0,
        )
        metrics = {m.id: m.score for m in score_signals(signals)}
        domain = sum(
            metrics[mid] * weight
            for mid, weight in (
                ("stability", 18),
                ("error-rate", 16),
                ("llm-serving", 16),
                ("turn-reliability", 16),
                ("tool-reliability", 14),
                ("latency", 20),
            )
        ) / 100.0
        self.assertTrue(45.0 <= domain <= 70.0, domain)
        self.assertLess(metrics["latency"], 45.0)
        self.assertGreater(metrics["stability"], 80.0)

    def test_cache_ttl_fail_closed(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "cache.json"
            signals = rh.Signals(runs=10, agent_ms=[1000] * 10, tool_call_reports=10, days=1.0)
            write_cache(path, signals, ttl_hours=1)
            stale_now = datetime.now(timezone.utc) + timedelta(hours=3)
            with self.assertRaises(TimeoutError):
                load_cache(path, now=stale_now)


class BaselineRatchetTests(unittest.TestCase):
    def test_check_flags_domain_drop_and_ignores_fitness(self) -> None:
        good = _report(60, 60, 40)
        base = snapshot(good)
        worse_structure = _report(50, 60, 90)  # fitness up must not hide structure drop
        result = check(worse_structure, base)
        self.assertFalse(result.ok)
        self.assertTrue(any("structure" in line for line in result.regressions))

    def test_update_rejects_overall_drop(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "baseline.json"
            update(path, _report(60, 60, 40))
            with self.assertRaises(BaselineRegressionError):
                update(path, _report(50, 60, 40))


class ReportShapeTests(unittest.TestCase):
    def test_weights_sum_and_schema(self) -> None:
        report = _report(50.4, 58.5, 43.0)
        self.assertAlmostEqual(sum(DOMAIN_WEIGHTS.values()), 1.0)
        self.assertEqual(v3_model.SCHEMA_VERSION, 3)
        self.assertEqual(v3_model.RUBRIC_VERSION, "3.0.0")
        payload = report.to_dict()
        self.assertEqual(payload["schema_version"], 3)
        self.assertTrue(45.0 <= payload["score"]["overall"] <= 55.0)


if __name__ == "__main__":
    unittest.main()
