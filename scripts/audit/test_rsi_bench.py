#!/usr/bin/env python3
"""Unit tests for RSI Bench 1.2."""

from __future__ import annotations

import json
import math
import tempfile
import time
import unittest
from pathlib import Path

from rsi_bench.baseline import check as check_baseline
from rsi_bench.baseline import snapshot as baseline_snapshot
from rsi_bench.ledgers import load_closure_window, load_genesis_window, load_judge_window, load_watch_window
from rsi_bench.model import (
    DOMAIN_WEIGHTS,
    MIN_CHECK_CONFIDENCE,
    MIN_RESOLVED_FOR_HARD,
    RUBRIC_VERSION,
    UNMEASURED_RATE_FLOOR,
    Domain,
    Evidence,
    Finding,
    Metric,
    Report,
    geometric_composite,
)
from rsi_bench.process import evaluate_process
from rsi_bench.utility import evaluate_utility, score_closure_land


def _write_jsonl(path: Path, rows: list[dict]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("".join(json.dumps(r) + "\n" for r in rows), encoding="utf-8")


class GeometricCompositeTests(unittest.TestCase):
    def test_geometric_mean_in_band_for_worksheet(self) -> None:
        overall = geometric_composite({"process": 35.0, "utility": 28.0})
        self.assertTrue(25.0 <= overall <= 40.0, overall)
        expected = round(math.exp(0.55 * math.log(35.0) + 0.45 * math.log(28.0)), 1)
        self.assertEqual(overall, expected)

    def test_weak_domain_pulls_composite(self) -> None:
        strong = geometric_composite({"process": 60.0, "utility": 60.0})
        weak_util = geometric_composite({"process": 60.0, "utility": 20.0})
        self.assertLess(weak_util, strong)


class LedgerTests(unittest.TestCase):
    def test_genesis_and_watch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            now = int(time.time() * 1000)
            _write_jsonl(
                root / "skill_genesis_log.jsonl",
                [
                    {"type": "evolved", "skillName": "a", "createdAt": now},
                    {"type": "evolved", "skillName": "a", "createdAt": now},
                    {"type": "evolved", "skillName": "a", "createdAt": now},
                ],
            )
            (root / "skill_evolve_watch.json").write_text(
                json.dumps({"a": {"postUses": 4}, "b": {"postUses": 4}, "c": {"postUses": 4}}),
                encoding="utf-8",
            )
            win = load_genesis_window(root)
            self.assertEqual(win.evolves, 3)
            self.assertTrue(win.thrash)
            watch = load_watch_window(root)
            self.assertEqual(watch.soft_confirmed, 3)
            self.assertGreaterEqual(watch.soft_confirmed, MIN_RESOLVED_FOR_HARD)

    def test_dispatch_counts_outcome_landed(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            folder = root / "coding_dispatch"
            folder.mkdir()
            (folder / "a.json").write_text(
                json.dumps({"status": "accepted", "outcome": "landed"}), encoding="utf-8"
            )
            (folder / "b.json").write_text(
                json.dumps({"status": "accepted", "outcome": "declined"}), encoding="utf-8"
            )
            (folder / "c.json").write_text(
                json.dumps({"status": "accepted", "outcome": "failed"}), encoding="utf-8"
            )
            from rsi_bench.ledgers import load_dispatch_window

            win = load_dispatch_window(root)
            self.assertEqual(win.files, 3)
            self.assertEqual(win.accepted, 3)
            self.assertEqual(win.landed, 1)
            self.assertEqual(win.failed, 1)
            # No attemptId → attempts=1 → efficiency 1.0 (flat-count parity).
            self.assertAlmostEqual(win.land_eff, 1.0)

    def test_dispatch_land_efficiency_decays_with_retries(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            folder = root / "coding_dispatch"
            folder.mkdir()
            markers = [
                ("first", "sc-1-100-200-1"),  # 1st attempt → 1.0
                ("retry", "sc-2-100-200-2"),  # 2nd attempt → 0.25
                ("third", "sc-3-100-200-3"),  # 3rd attempt → ~0.111
                ("weird", "not-an-ordinal"),  # unparseable → 1.0 (no penalty)
            ]
            for name, attempt_id in markers:
                (folder / f"{name}.json").write_text(
                    json.dumps({"status": "accepted", "outcome": "landed", "attemptId": attempt_id}),
                    encoding="utf-8",
                )
            from rsi_bench.ledgers import land_efficiency, load_dispatch_window

            win = load_dispatch_window(root)
            self.assertEqual(win.landed, 4)
            self.assertAlmostEqual(win.land_eff, 1.0 + 0.25 + 1.0 / 9.0 + 1.0, places=6)
            # RHAE cap guards a future sub-1 baseline; at baseline 1 it is inert.
            self.assertAlmostEqual(land_efficiency(1), 1.0)
            self.assertLessEqual(land_efficiency(0), 1.15)

    def test_soft_confirm_from_usage_after_evolve(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            now = int(time.time() * 1000)
            _write_jsonl(
                root / "skill_genesis_log.jsonl",
                [{"type": "evolved", "skillName": "solo", "createdAt": now - 1000}],
            )
            _write_jsonl(
                root / "skill_usage.jsonl",
                [
                    {"skillName": "solo", "source": "real", "usedAt": now},
                    {"skillName": "solo", "source": "real", "usedAt": now + 1},
                    {"skillName": "solo", "source": "real", "usedAt": now + 2},
                    {"skillName": "solo", "source": "workout", "usedAt": now + 3},
                ],
            )
            (root / "skill_evolve_watch.json").write_text("{}", encoding="utf-8")
            watch = load_watch_window(root)
            self.assertEqual(watch.soft_confirmed, 1)

    def test_judge_category_and_closure(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            now = int(time.time() * 1000)
            _write_jsonl(
                root / "judge_accuracy_log.jsonl",
                [
                    {
                        "pairs": 10,
                        "correct": 8,
                        "createdAt": now,
                        "byCategory": {"productivity": [8, 10], "devops": [3, 5]},
                        "byClass": {"imperative-drop": [2, 3], "truncation": [2, 2]},
                        "misses": [{"skill": "x"}],
                    }
                ],
            )
            _write_jsonl(
                root / "self_correction_candidates.jsonl",
                [
                    {"type": "self_correction_candidate", "status": "proposed", "createdAt": now},
                    {"type": "self_correction_candidate", "status": "applied", "createdAt": now},
                ],
            )
            judge = load_judge_window(root)
            self.assertEqual(judge.runs, 1)
            self.assertEqual(judge.misses, 1)
            self.assertIn("productivity", judge.by_category)
            closure = load_closure_window(root)
            self.assertEqual(closure.proposed, 2)
            self.assertEqual(closure.landed, 1)


class ClosureWindowTests(unittest.TestCase):
    """closure-land must measure delivery, not review acceptance."""

    def _closure(self, rows: list[dict]):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = Path(tmp.name)
        _write_jsonl(root / "self_correction_candidates.jsonl", rows)
        return load_closure_window(root)

    def test_accepted_alone_is_not_landed(self) -> None:
        """A batch-accept sweep must not raise closure without shipping."""
        closure = self._closure(
            [
                {"type": "self_correction_candidate", "id": "a", "status": "proposed", "createdAt": 1},
                {"type": "self_correction_review", "id": "a", "status": "accepted", "createdAt": 2},
            ]
        )
        self.assertEqual(closure.proposed, 1)
        self.assertEqual(closure.landed, 0)

    def test_watch_passed_dispatch_counts_as_landed(self) -> None:
        """The real delivery signal lives on dispatch rows, not review status."""
        closure = self._closure(
            [
                {"type": "self_correction_candidate", "id": "a", "status": "proposed", "createdAt": 1},
                {"type": "self_correction_review", "id": "a", "status": "accepted", "createdAt": 2},
                {"type": "self_correction_dispatch", "id": "a", "dispatchPhase": "merged", "createdAt": 3},
                {"type": "self_correction_dispatch", "id": "a", "dispatchPhase": "watch_passed", "createdAt": 4},
            ]
        )
        self.assertEqual(closure.proposed, 1)
        self.assertEqual(closure.landed, 1)

    def test_non_terminal_dispatch_is_not_landed(self) -> None:
        closure = self._closure(
            [
                {"type": "self_correction_candidate", "id": "a", "status": "proposed", "createdAt": 1},
                {"type": "self_correction_dispatch", "id": "a", "dispatchPhase": "merged", "createdAt": 2},
                {"type": "self_correction_dispatch", "id": "b", "dispatchPhase": "failed", "createdAt": 3},
            ]
        )
        self.assertEqual(closure.landed, 0)

    def test_rows_fold_per_candidate_id(self) -> None:
        """Review churn on one candidate must not inflate the denominator."""
        closure = self._closure(
            [
                {"type": "self_correction_candidate", "id": "a", "status": "proposed", "createdAt": 1},
                {"type": "self_correction_review", "id": "a", "status": "accepted", "createdAt": 2},
                {"type": "self_correction_review", "id": "a", "status": "superseded", "createdAt": 3},
                {"type": "self_correction_review", "id": "a", "status": "accepted", "createdAt": 4},
            ]
        )
        self.assertEqual(closure.proposed, 1)

    def test_rejected_is_not_a_revert(self) -> None:
        """Declining a bad candidate is healthy; it must not be double-penalised."""
        closure = self._closure(
            [
                {"type": "self_correction_candidate", "id": "a", "status": "proposed", "createdAt": 1},
                {"type": "self_correction_review", "id": "a", "status": "rejected", "createdAt": 2},
                {"type": "self_correction_candidate", "id": "b", "status": "proposed", "createdAt": 3},
                {"type": "self_correction_review", "id": "b", "status": "reverted", "createdAt": 4},
            ]
        )
        self.assertEqual(closure.proposed, 2)
        self.assertEqual(closure.reverted, 1)

    def test_latest_review_row_wins(self) -> None:
        closure = self._closure(
            [
                {"type": "self_correction_candidate", "id": "a", "status": "proposed", "createdAt": 1},
                {"type": "self_correction_review", "id": "a", "status": "applied", "createdAt": 2},
                {"type": "self_correction_review", "id": "a", "status": "reverted", "createdAt": 3},
            ]
        )
        self.assertEqual(closure.landed, 0)
        self.assertEqual(closure.reverted, 1)


class ProcessUtilityTests(unittest.TestCase):
    def test_rubric_version(self) -> None:
        self.assertEqual(RUBRIC_VERSION, "1.2.0")

    def test_unmeasured_rate_floor(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            data = Path(tmp) / "data"
            data.mkdir()
            root = Path(tmp) / "repo"
            (root / "scripts" / "audit").mkdir(parents=True)
            process = evaluate_process(root, cache=None, data=data)
            acceptor = next(m for m in process.metrics if m.id == "acceptor-trust")
            self.assertEqual(acceptor.score, UNMEASURED_RATE_FLOOR)
            self.assertEqual(len(process.metrics), 9)
            self.assertTrue(any(m.id == "swap-consistency" for m in process.metrics))
            self.assertTrue(any(m.id == "ability-transfer" for m in process.metrics))

    def test_utility_metric_count(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            data = Path(tmp) / "data"
            data.mkdir()
            root = Path(tmp) / "repo"
            (root / "scripts" / "audit").mkdir(parents=True)
            utility = evaluate_utility(root, data=data)
            self.assertEqual(len(utility.metrics), 5)
            self.assertTrue(utility.ratcheted)
            score, ev, _ = score_closure_land(data=data)
            self.assertEqual(ev.status, "bootstrap")
            self.assertGreaterEqual(score, 0.0)

    def _process_metrics(self, score: float) -> list[Metric]:
        return [
            Metric("acceptor-trust", "a", 16, score, "x"),
            Metric("confirm-honesty", "c", 10, score, "x"),
            Metric("judge-fuel", "j", 12, score, "x"),
            Metric("preference-collapse", "p", 8, score, "x"),
            Metric("swap-consistency", "s", 10, score, "x"),
            Metric("probe-coverage", "pr", 6, score, "x"),
            Metric("timescale-turn", "t", 12, score, "x"),
            Metric("ability-transfer", "tr", 12, score, "x"),
            Metric("anti-collapse", "x", 14, score, "x"),
        ]

    def _utility_metrics(self, score: float) -> list[Metric]:
        return [
            Metric("closure-land", "c", 25, score, "x"),
            Metric("operator-verdict", "o", 20, score, "x"),
            Metric("codebase-delta", "d", 20, score, "x"),
            Metric("retention-proxy", "r", 15, score, "x"),
            Metric("dispatch-land", "di", 20, score, "x"),
        ]

    def _evidence(self) -> list[Evidence]:
        return [Evidence(f"ev-{i}", "measured", "fixture") for i in range(5)]

    def test_utility_regression_is_caught(self) -> None:
        process = Domain(
            id="process",
            title="Process",
            weight=DOMAIN_WEIGHTS["process"],
            metrics=self._process_metrics(40),
            ratcheted=True,
        )
        good = Report(
            profile="fast",
            revision="r1",
            domains=[
                process,
                Domain(
                    id="utility",
                    title="Utility",
                    weight=DOMAIN_WEIGHTS["utility"],
                    metrics=self._utility_metrics(50),
                    ratcheted=True,
                ),
            ],
            evidence=self._evidence(),
        )
        bad = Report(
            profile="fast",
            revision="r2",
            domains=[
                process,
                Domain(
                    id="utility",
                    title="Utility",
                    weight=DOMAIN_WEIGHTS["utility"],
                    metrics=self._utility_metrics(10),
                    ratcheted=True,
                ),
            ],
            evidence=self._evidence(),
        )
        result = check_baseline(bad, baseline_snapshot(good))
        self.assertFalse(result.ok)
        self.assertTrue(any("utility" in r for r in result.regressions))

    def test_process_regression_is_caught(self) -> None:
        utility = Domain(
            id="utility",
            title="Utility",
            weight=DOMAIN_WEIGHTS["utility"],
            metrics=self._utility_metrics(50),
            ratcheted=True,
        )
        good = Report(
            profile="fast",
            revision="r1",
            domains=[
                Domain(
                    id="process",
                    title="Process",
                    weight=DOMAIN_WEIGHTS["process"],
                    metrics=self._process_metrics(50),
                    ratcheted=True,
                ),
                utility,
            ],
            evidence=self._evidence(),
        )
        bad = Report(
            profile="fast",
            revision="r2",
            domains=[
                Domain(
                    id="process",
                    title="Process",
                    weight=DOMAIN_WEIGHTS["process"],
                    metrics=self._process_metrics(20),
                    ratcheted=True,
                ),
                utility,
            ],
            evidence=self._evidence(),
        )
        result = check_baseline(bad, baseline_snapshot(good))
        self.assertFalse(result.ok)



    def test_confidence_gate_blocks_thin_evidence(self) -> None:
        process = Domain(
            id="process",
            title="Process",
            weight=DOMAIN_WEIGHTS["process"],
            metrics=self._process_metrics(50),
            ratcheted=True,
        )
        utility = Domain(
            id="utility",
            title="Utility",
            weight=DOMAIN_WEIGHTS["utility"],
            metrics=self._utility_metrics(50),
            ratcheted=True,
        )
        thin = Report(
            profile="fast",
            revision="r1",
            domains=[process, utility],
            evidence=[Evidence("only-bootstrap", "bootstrap", "thin")],
        )
        thick = Report(
            profile="fast",
            revision="r2",
            domains=[process, utility],
            evidence=self._evidence(),
        )
        result = check_baseline(thin, baseline_snapshot(thick))
        self.assertFalse(result.ok)
        self.assertTrue(any("confidence" in r for r in result.regressions))
        self.assertGreaterEqual(MIN_CHECK_CONFIDENCE, 60.0)


class FindingContractTests(unittest.TestCase):
    def test_finding_requires_fields(self) -> None:
        with self.assertRaises(ValueError):
            Finding(
                id="",
                domain="process",
                pillar="acceptor-trust",
                severity="high",
                path="x",
                evidence="e",
                why="w",
                remediation="r",
                verify="v",
            )


if __name__ == "__main__":
    unittest.main()
