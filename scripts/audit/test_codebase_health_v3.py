#!/usr/bin/env python3
"""Unit tests for Health Bench 3.0 composite math and calibration transforms."""

from __future__ import annotations

import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from textwrap import dedent

from health_v3.baseline import BaselineRegressionError, check, snapshot, update
from health_v3.model import (
    DOMAIN_WEIGHTS,
    Domain,
    Evidence,
    Metric,
    Report,
    geometric_composite,
)
from health_v3.runtime import (
    TOOLERR_FRAC_HARD,
    TOOLERR_FRAC_SOFT,
    _grade,
    evaluate_runtime,
    load_cache,
    score_signals,
    state_cache_path,
    write_cache,
)
from health_v3.structure import (
    _AI_EXTRA_TIGHTEN,
    _AI_LANE_GUIDE_DOC,
    _CHANGE_BLAST_SCALE,
    _DELIVERY_SCALE,
    _TEST_TRUTH_SCALE,
    _ai_lane_impact_coverage,
    _collect_ai_lane_guides,
    _unpaid_ai_lane_finding,
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


class AILaneGuideEvidenceTests(unittest.TestCase):
    def test_missing_non_go_guides_keep_original_unpaid_finding_id(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)

            guides = _collect_ai_lane_guides(root)
            finding = _unpaid_ai_lane_finding(guides)

            self.assertEqual(_ai_lane_impact_coverage(guides), 0.45)
            self.assertIsNotNone(finding)
            self.assertEqual(finding.id, "ai-maintainability:207960b23a30")

    def test_grounded_non_go_guides_remove_unpaid_finding(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write_lane_fixture(root)

            guides = _collect_ai_lane_guides(root)

            self.assertEqual(sorted(guides), ["kotlin", "python", "typescript"])
            self.assertTrue(all(item.measured for item in guides.values()))
            self.assertEqual(_ai_lane_impact_coverage(guides), 1.0)
            self.assertIsNone(_unpaid_ai_lane_finding(guides))

    def test_lane_guides_require_source_defined_symbols(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write_lane_fixture(root, python_symbol="wrong_symbol")

            guides = _collect_ai_lane_guides(root)

            self.assertTrue(guides["kotlin"].measured)
            self.assertTrue(guides["typescript"].measured)
            self.assertFalse(guides["python"].measured)
            self.assertIn("no source-defined entry symbol", guides["python"].detail)
            self.assertIsNotNone(_unpaid_ai_lane_finding(guides))


def _write(root: Path, rel: str, text: str = "") -> None:
    path = root / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text, encoding="utf-8")


def _write_lane_fixture(root: Path, *, python_symbol: str = "structural_candidates") -> None:
    _write(
        root,
        "Makefile",
        dedent(
            """
            ci:
            python-test:
            python-lint:
            health-v3-test:
            """
        ).lstrip(),
    )
    _write(
        root,
        "client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/AppRoot.kt",
        "package ai.deneb\nfun App() {}\n",
    )
    _write(
        root,
        "client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/deneb/DenebGatewayClient.kt",
        "package ai.deneb.deneb\nclass DenebGatewayClient\n",
    )
    _write(
        root,
        "client-android/app/composeApp/src/commonTest/kotlin/ai/deneb/AppRouteSerializationContractTest.kt",
        "package ai.deneb\nclass AppRouteSerializationContractTest\n",
    )
    _write(
        root,
        "andromeda/src/gateway.ts",
        "export async function callRpc() {}\nexport function gatewayFetch() {}\n",
    )
    _write(root, "andromeda/src/resources.ts", "export const RESOURCE_DEFS = [];\n")
    _write(root, "andromeda/src/gateway.behavior.test.ts", "test('rpc', () => {});\n")
    _write(root, "scripts/audit/health_finding_miner.py", "def structural_candidates(report):\n    return []\n")
    _write(root, "scripts/audit/test_health_finding_miner.py", "import unittest\n")
    _write(
        root,
        _AI_LANE_GUIDE_DOC.as_posix(),
        dedent(
            f"""
            # Product Lane AI Maintainability

            ## Kotlin Native Client

            - Source anchors: `client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/AppRoot.kt`,
              `client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/deneb/DenebGatewayClient.kt`.
            - Entrypoints: `App`, `DenebGatewayClient`.
            - Tests: `client-android/app/composeApp/src/commonTest/kotlin/ai/deneb/AppRouteSerializationContractTest.kt`.
            - Verify: `make ci ARGS=--kotlin`.

            ## TypeScript Desktop Workstation

            - Source anchors: `andromeda/src/gateway.ts`, `andromeda/src/resources.ts`.
            - Entrypoints: `callRpc`, `gatewayFetch`, `RESOURCE_DEFS`.
            - Tests: `andromeda/src/gateway.behavior.test.ts`.
            - Verify: `cd andromeda && pnpm verify`.

            ## Python Audit And Ops Scripts

            - Source anchors: `scripts/audit/health_finding_miner.py`.
            - Entrypoints: `{python_symbol}`.
            - Tests: `scripts/audit/test_health_finding_miner.py`.
            - Verify: `make python-test`.
            """
        ).lstrip(),
    )


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

    def test_state_cache_lives_outside_the_repo(self) -> None:
        # The tracked seed cannot be the live cache: the daily deep refresh must
        # revert tracked writes to keep the auto-deploy tree clean, which is
        # exactly how the on-disk cache went permanently stale (07-18 → 07-27,
        # miner silently on the v2 fallback). The live cache must therefore
        # resolve under the state dir, never under the checkout.
        self.assertNotIn("scripts", state_cache_path().parts)
        self.assertEqual(state_cache_path().parts[-3:], (".deneb", "data", "health-v3-runtime-cache.json"))

    def test_evaluate_runtime_reads_state_cache_before_repo_seed(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "repo"
            seed = root / "scripts" / "audit" / "health-v3-runtime-cache.json"
            seed.parent.mkdir(parents=True)
            state = Path(tmp) / "state" / "health-v3-runtime-cache.json"
            state.parent.mkdir(parents=True)
            # Distinguishable signal shapes: state=fresh 20 runs, seed=fresh 10 runs.
            write_cache(state, rh.Signals(runs=20, agent_ms=[1000] * 20, tool_call_reports=20, days=1.0))
            write_cache(seed, rh.Signals(runs=10, agent_ms=[9000] * 10, tool_call_reports=10, days=1.0))

            real_state_path = state_cache_path
            try:
                import health_v3.runtime as runtime_mod

                runtime_mod.state_cache_path = lambda: state
                domain = evaluate_runtime(root, profile="fast")
                self.assertTrue(any("runtime-cache" == e.name and e.status == "measured" for e in domain.evidence))
                # State cache won: latency metric reflects the 1000ms shape, not 9000ms.
                latency = next(m for m in domain.metrics if m.id == "latency")
                self.assertGreater(latency.score, 90.0)

                # State cache gone → seed is the fallback (9000ms shape).
                state.unlink()
                domain = evaluate_runtime(root, profile="fast")
                latency = next(m for m in domain.metrics if m.id == "latency")
                self.assertGreater(latency.score, 90.0)  # 9s p95 still under the 40s soft bar
                self.assertTrue(any(e.name == "runtime-cache" and e.status == "measured" for e in domain.evidence))

                # Both gone → fail-closed placeholder domain, metrics at 0.
                seed.unlink()
                domain = evaluate_runtime(root, profile="fast")
                self.assertTrue(all(m.score == 0.0 for m in domain.metrics))
                self.assertTrue(any(e.name == "runtime-cache" and e.status == "unavailable" for e in domain.evidence))
            finally:
                runtime_mod.state_cache_path = real_state_path


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


class SubBarResolutionTests(unittest.TestCase):
    """The sub-hard-bar band must add resolution WITHOUT softening the verdict.

    Production sat past five of six runtime bars on 2026-07-25, so every one of
    those pillars scored exactly 0.0 for four consecutive nights and the nightly
    ratchet could not tell "recovering" from "still broken".
    """

    BARS = (TOOLERR_FRAC_SOFT, TOOLERR_FRAC_HARD)

    def test_monotonically_decreasing_across_the_whole_range(self) -> None:
        # A worse measurement must never score higher — the property that makes the
        # tail a measurement rather than decoration.
        values = [0.0, 0.005, 0.01, 0.015, 0.0229, 0.023, 0.024, 0.035, 0.046, 0.092, 0.5, 5.0]
        scores = [_grade(v, *self.BARS) for v in values]
        for (v0, s0), (v1, s1) in zip(zip(values, scores), zip(values[1:], scores[1:])):
            self.assertGreaterEqual(s0, s1, f"{v0}->{v1} scored {s0}->{s1}")

    def test_continuous_at_the_hard_bar(self) -> None:
        soft, hard = self.BARS
        just_under = _grade(hard - 1e-9, soft, hard)
        at_bar = _grade(hard, soft, hard)
        just_over = _grade(hard + 1e-9, soft, hard)
        self.assertAlmostEqual(just_under, at_bar, places=4)
        self.assertAlmostEqual(at_bar, just_over, places=4)

    def test_past_the_bar_still_reads_as_failure(self) -> None:
        # 45 is the `high` finding cut; the band must stay far below it so crossing
        # a bar is never mistaken for partial credit.
        soft, hard = self.BARS
        self.assertLess(_grade(hard, soft, hard), 10.0)
        self.assertLess(_grade(hard * 4, soft, hard), _grade(hard, soft, hard))

    def test_perfect_input_is_unchanged(self) -> None:
        # The band must not cost anything at the healthy end, or every green pillar
        # silently regresses against its baseline.
        self.assertAlmostEqual(_grade(0.0, *self.BARS), 100.0, places=6)
        self.assertAlmostEqual(_grade(self.BARS[0], *self.BARS), 100.0, places=6)

    def test_distinguishes_degrees_of_failure(self) -> None:
        # The whole point: two broken-but-different states must not collapse to one
        # number. Real 2026-07-25 values were 3.5% tool errors and 8.0% timeouts.
        soft, hard = self.BARS
        self.assertGreater(_grade(0.035, soft, hard), _grade(0.070, soft, hard))
        self.assertGreater(_grade(0.070, soft, hard) - _grade(0.140, soft, hard), 0.3)


if __name__ == "__main__":
    unittest.main()
