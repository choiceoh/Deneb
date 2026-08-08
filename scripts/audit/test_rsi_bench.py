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

    def test_starved_pillar_reports_as_unmeasured_not_regression(self) -> None:
        """A floor missed because evidence never resolved is not a code regression.

        It still fails the check — a starved lane is a real fault — but it is
        reported separately so the reader is not sent hunting for a code change
        that never happened (2026-08-07: acceptor-trust, confirm-honesty and
        retention-proxy all sat on bootstrap floors with resolved=0).
        """
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
        starved_metrics = self._utility_metrics(50)
        starved_metrics[3] = Metric("retention-proxy", "r", 15, 10.0, "x")
        bad = Report(
            profile="fast",
            revision="r2",
            domains=[
                process,
                Domain(
                    id="utility",
                    title="Utility",
                    weight=DOMAIN_WEIGHTS["utility"],
                    metrics=starved_metrics,
                    ratcheted=True,
                ),
            ],
            evidence=[
                *self._evidence(),
                Evidence("utility-retention-proxy", "bootstrap", "resolved=0; watches=0"),
            ],
        )
        result = check_baseline(bad, baseline_snapshot(good))
        self.assertFalse(result.ok)
        self.assertTrue(
            any("utility.retention-proxy" in m for m in result.unmeasured),
            f"expected the starved pillar under unmeasured, got {result.unmeasured}",
        )
        self.assertFalse(
            any("utility.retention-proxy" in m for m in result.regressions),
            f"starved pillar must not be reported as a regression, got {result.regressions}",
        )
        self.assertTrue(
            any(line.startswith("UNMEASURED:") for line in result.format_lines()),
            result.format_lines(),
        )

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


class JudgeMissFoldingTests(unittest.TestCase):
    """Distinct defects are the fuel; probe frequency must not be the penalty."""

    @staticmethod
    def _log(root: Path, runs: int, misses_per_run: list[dict[str, str]]) -> None:
        now = int(time.time() * 1000)
        _write_jsonl(
            root / "judge_accuracy_log.jsonl",
            [
                {"pairs": 10, "correct": 10, "createdAt": now - i * 1000, "misses": misses_per_run}
                for i in range(runs)
            ],
        )

    def test_same_defect_across_runs_folds_to_one(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._log(root, 28, [{"skill": "Playwright", "degradation": "imperative-drop"}])
            judge = load_judge_window(root)
            self.assertEqual(judge.misses, 1)  # one standing weakness
            self.assertEqual(judge.miss_events, 28)  # ...seen 28 times
            self.assertEqual(judge.chronic_miss, ("Playwright|imperative-drop", 28))

    def test_distinct_degradations_of_one_skill_stay_separate(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._log(
                root,
                2,
                [
                    {"skill": "Playwright", "degradation": "imperative-drop"},
                    {"skill": "Playwright", "degradation": "truncation"},
                ],
            )
            judge = load_judge_window(root)
            self.assertEqual(judge.misses, 2)
            self.assertEqual(judge.miss_events, 4)

    def test_probing_more_does_not_lower_the_score(self) -> None:
        """The regression: penalty scaled with run count, so measuring hurt."""
        from rsi_bench.process import _score_judge

        def score_for(runs: int) -> float:
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                self._log(root, runs, [{"skill": "Playwright", "degradation": "imperative-drop"}])
                score, _ev, _f = _score_judge(load_judge_window(root))
                return score

        few, many = score_for(4), score_for(40)
        self.assertGreaterEqual(many, few)

    def test_finding_names_the_chronic_defect(self) -> None:
        from rsi_bench.process import _score_judge

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            now = int(time.time() * 1000)
            _write_jsonl(
                root / "judge_accuracy_log.jsonl",
                [
                    {
                        "pairs": 10,
                        "correct": 9,
                        "createdAt": now - i * 1000,
                        "misses": [
                            {"skill": "Playwright", "degradation": "imperative-drop"},
                            *(
                                [{"skill": "serper", "degradation": "truncation"}]
                                if i == 0
                                else []
                            ),
                            *([{"skill": "contract-review", "degradation": "swap"}] if i == 0 else []),
                        ],
                    }
                    for i in range(5)
                ],
            )
            judge = load_judge_window(root)
            self.assertEqual(judge.misses, 3)
            _score, ev, findings = _score_judge(judge)
            self.assertEqual(len(findings), 1)
            self.assertIn("Playwright", findings[0].evidence)
            self.assertIn("events=7", findings[0].evidence)
            self.assertIn("chronic=", ev.detail)


class CodebaseDeltaDecouplingTests(unittest.TestCase):
    """codebase-delta must read health STRUCTURE, never health overall.

    Overall folds in Fitness, which re-exports RSI closure-land/operator-verdict
    and its own live−baseline delta — reading it made the pillar score itself.
    """

    @staticmethod
    def _write(root: Path, baseline: dict[str, object], snapshot: dict[str, object] | None) -> None:
        audit = root / "scripts" / "audit"
        audit.mkdir(parents=True)
        (audit / "health-v3-baseline.json").write_text(json.dumps(baseline), encoding="utf-8")
        if snapshot is not None:
            (audit / "health-v3-snapshot.json").write_text(json.dumps(snapshot), encoding="utf-8")

    def test_reads_structure_and_ignores_overall_collapse(self) -> None:
        from rsi_bench.utility import score_codebase_delta

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            # Overall craters (49.1 → 32.3) purely from runtime/fitness while
            # structure holds flat. The pillar must see the flat structure.
            self._write(
                root,
                {"overall": 49.1, "domains": {"structure": 50.5, "runtime": 58.3, "fitness": 36.7}},
                {"score": {"overall": 32.3, "domains": {"structure": 50.5, "runtime": 23.2, "fitness": 16.2}}},
            )
            score, ev, findings = score_codebase_delta(root)
            self.assertEqual(ev.status, "measured")
            self.assertIn("structure", ev.detail)
            self.assertAlmostEqual(score, 25.0)  # delta 0 → neutral, not 0.0
            self.assertEqual(findings, [])

    def test_structure_regression_still_scores_and_files_a_finding(self) -> None:
        from rsi_bench.utility import score_codebase_delta

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._write(
                root,
                {"overall": 49.1, "domains": {"structure": 50.5}},
                {"score": {"overall": 49.0, "domains": {"structure": 48.5}}},
            )
            score, ev, findings = score_codebase_delta(root)
            self.assertEqual(ev.status, "measured")
            self.assertAlmostEqual(score, 13.0)  # 25 + (-2.0 * 6)
            self.assertEqual(len(findings), 1)
            self.assertEqual(findings[0].pillar, "codebase-delta")

    def test_missing_structure_is_unmeasured_not_overall_fallback(self) -> None:
        from rsi_bench.utility import score_codebase_delta

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            # Overall present on both sides, structure absent → must NOT quietly
            # score off overall (that is the coupled signal).
            self._write(root, {"overall": 49.1}, {"score": {"overall": 10.0}})
            score, ev, findings = score_codebase_delta(root)
            self.assertEqual(ev.status, "bootstrap")
            self.assertAlmostEqual(score, 25.0)
            self.assertEqual(findings, [])

    def test_cache_domains_serve_snapshotless_hosts(self) -> None:
        from rsi_bench.utility import score_codebase_delta

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._write(root, {"overall": 49.1, "domains": {"structure": 50.5}}, None)
            score, ev, _f = score_codebase_delta(
                root, cache={"health_v3": {"overall": 32.3, "domains": {"structure": 53.5}}}
            )
            self.assertEqual(ev.status, "measured")
            self.assertIn("cache", ev.detail)
            self.assertAlmostEqual(score, 43.0)  # 25 + (+3.0 * 6)


class ImpactWindowTests(unittest.TestCase):
    def test_folds_by_candidate_id_and_maps_terminal_statuses(self) -> None:
        from rsi_bench.ledgers import load_impact_window

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            now = int(time.time() * 1000)
            rows = [
                # Same candidate re-checked: the later verdict supersedes.
                {"id": "sc-1", "createdAt": now - 5000, "impactResult": {"status": "no_effect"}},
                {"id": "sc-1", "createdAt": now - 1000, "impactResult": {"status": "verified"}},
                {"id": "sc-2", "createdAt": now - 2000, "impactResult": {"status": "no_effect"}},
                {"id": "sc-3", "createdAt": now - 2000, "impactResult": {"status": "regressed"}},
                # Non-terminal and out-of-window rows never vote.
                {"id": "sc-4", "createdAt": now - 2000, "impactResult": {"status": "pending"}},
                {"id": "sc-5", "createdAt": now - 40 * 86400 * 1000, "impactResult": {"status": "verified"}},
            ]
            (root / "self_correction_candidates.jsonl").write_text(
                "\n".join(json.dumps(r) for r in rows) + "\n", encoding="utf-8"
            )
            win = load_impact_window(root)
            self.assertEqual((win.verified, win.no_effect, win.regressed), (1, 1, 1))
            self.assertEqual(win.total, 3)

    def test_no_effect_lowers_the_verdict_score(self) -> None:
        """Symmetry guard: folding impact in must not be a one-way score lift."""
        from rsi_bench.utility import score_operator_verdict

        def verdict(statuses: list[str]) -> float:
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                now = int(time.time() * 1000)
                rows = [
                    {"id": f"sc-{i}", "createdAt": now - 1000, "impactResult": {"status": s}}
                    for i, s in enumerate(statuses)
                ]
                (root / "self_correction_candidates.jsonl").write_text(
                    "\n".join(json.dumps(r) for r in rows) + "\n", encoding="utf-8"
                )
                (root / "meta_evolution_log.jsonl").write_text("", encoding="utf-8")
                score, _ev, _f = score_operator_verdict(data=root)
                return score

        all_good = verdict(["verified"] * 6)
        mixed = verdict(["verified"] * 3 + ["no_effect"] * 3)
        all_bad = verdict(["no_effect"] * 6)
        self.assertGreater(all_good, mixed)
        self.assertGreater(mixed, all_bad)
        self.assertAlmostEqual(all_bad, 0.0)


if __name__ == "__main__":
    unittest.main()
