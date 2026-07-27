"""Deterministic tests for the proactive L4 supply miner (P5 ws3).

The load-bearing assertions: only high-severity findings become candidates,
every candidate carries the bench finding ID inside its evidence (deterministic
review), the reopen semantics mirror genesis selfCorrectionReopenBlocked
(rejected never re-files; applied re-files only after cooldown), and blocked
findings never consume the per-run cap.
"""

from __future__ import annotations

import io
import json
import os
import tempfile
import unittest

from health_finding_miner import (
    MAX_RUNTIME_PER_RUN,
    REOPEN_COOLDOWN_MS,
    RUNTIME_WEAK_SCORE,
    forbidden_target_reason,
    main,
    parse_leading_json,
    pending_impact_observations,
    reopen_blocked,
    runtime_candidates,
    select_candidates,
    structural_candidates,
)

NOW = 1_800_000_000_000


def structural_report(**overrides):
    report = {
        "revision": "revision-test-3258",
        "profile": "fast",
        "findings": [
            {
                "id": "volatile-hub:46a381ef4981",
                "pillar": "change-locality",
                "severity": "high",
                "path": "gateway-go/internal/domain/wiki",
                "evidence": "Volatile-hub index is 5.13: 63/295 commits times 24 dependents.",
                "why": "Many dependents consume a contract that changes frequently.",
                "remediation": "Stabilize and narrow the public contract.",
                "verify": "Re-run the bench and confirm this finding disappears.",
                "priority": 95.0,
                "related_paths": [],
            },
            {
                "id": "fanout-hotspot:e3983434e99a",
                "pillar": "boundary-integrity",
                "severity": "high",
                "path": "gateway-go/internal/runtime/server",
                "evidence": "Direct internal fan-out is 116; bars are 20/50.",
                "why": "A broad dependency surface.",
                "remediation": "Depend on narrow capability ports.",
                "verify": "Re-run the bench.",
                "priority": 95.3,
                "related_paths": ["gateway-go/internal/ai/agent"],
            },
            {
                "id": "doc-drift:aaaa",
                "pillar": "delivery",
                "severity": "medium",
                "path": "docs",
                "evidence": "medium stuff",
                "why": "not high severity",
                "remediation": "n/a",
                "verify": "n/a",
                "priority": 99.0,
                "related_paths": [],
            },
        ],
    }
    report.update(overrides)
    return report


def runtime_report():
    return {
        "composite": 82.3,
        "meta": {"runs": 523, "days": 7.0, "since": "7 days ago"},
        "dims": {"stability": 100.0, "latency": 51.1, "turn-reliability": 75.0},
        "weights": {"stability": 18, "latency": 20, "turn-reliability": 16},
        "detail": {"latency": ["p95 148.0s over 523 runs"]},
        "extra": {"latency": {"p95_s": 148.0, "sampled_runs": 523}},
    }


def rsi_report():
    return {
        "score": {
            "overall": 25.8,
            "domains": {"process": 31.3, "utility": 20.4},
        },
        "domains": [
            {
                "id": "process",
                "score": 31.3,
                "metrics": [{"id": "acceptor-trust", "score": 28.0}],
            },
            {
                "id": "utility",
                "score": 20.4,
                "metrics": [{"id": "dispatch-land", "score": 22.0}],
            },
        ],
    }


class StructuralCandidatesTest(unittest.TestCase):
    def test_when_only_high_severity_ranked_by_priority(self):
        cands = structural_candidates(structural_report())
        self.assertEqual(len(cands), 2)  # medium finding dropped
        self.assertEqual(cands[0]["source"], "health-finding:fanout-hotspot:e3983434e99a")
        self.assertEqual(cands[1]["source"], "health-finding:volatile-hub:46a381ef4981")

    def test_when_candidate_carries_finding_id_and_evidence(self):
        cand = structural_candidates(structural_report())[1]
        self.assertIn("volatile-hub:46a381ef4981", cand["evidence"])
        self.assertIn("Volatile-hub index is 5.13", cand["evidence"])
        self.assertIn("revision-tes", cand["evidence"])  # bench revision pinned
        self.assertEqual(cand["scope"], "code")
        self.assertEqual(cand["targetFiles"], ["gateway-go/internal/domain/wiki"])
        self.assertIn("Verify:", cand["proposedChange"])
        self.assertEqual(
            cand["impactContract"],
            {
                "metric": "health.finding_present:volatile-hub:46a381ef4981",
                "direction": "decrease",
                "baseline": 1,
                "target": 0,
                "minSamples": 1,
            },
        )

    def test_empty_report_yields_nothing(self):
        self.assertEqual(structural_candidates({"findings": []}), [])


class RuntimeCandidatesTest(unittest.TestCase):
    def test_when_weakest_dim_below_bar_selected(self):
        cands = runtime_candidates(runtime_report())
        self.assertEqual(len(cands), MAX_RUNTIME_PER_RUN)
        cand = cands[0]
        self.assertEqual(cand["source"], "health-finding:runtime-latency")
        self.assertIn("runtime-latency", cand["evidence"])
        self.assertIn("p95_s=148.0", cand["evidence"])
        self.assertIn("51.1", cand["title"])
        self.assertEqual(cand["impactContract"]["baseline"], 51.1)
        self.assertEqual(cand["impactContract"]["target"], RUNTIME_WEAK_SCORE)

    def test_when_healthy_dims_do_not_file(self):
        report = runtime_report()
        report["dims"] = {"stability": 100.0, "latency": RUNTIME_WEAK_SCORE}
        self.assertEqual(runtime_candidates(report), [])

    def test_missing_report_tolerated(self):
        self.assertEqual(runtime_candidates(None), [])


class ReopenBlockedTest(unittest.TestCase):
    SRC = "health-finding:volatile-hub:46a381ef4981"

    def _existing(self, status, created=NOW - 1000):
        return [{"id": "sc-1", "source": self.SRC, "status": status, "createdAt": created}]

    def test_never_filed_allows(self):
        self.assertIsNone(reopen_blocked([], self.SRC, NOW))

    def test_when_open_twin_blocks(self):
        for status in ("proposed", "accepted"):
            self.assertIsNotNone(reopen_blocked(self._existing(status), self.SRC, NOW))

    def test_when_operator_veto_never_refiles(self):
        old = NOW - 10 * REOPEN_COOLDOWN_MS
        for status in ("rejected", "superseded"):
            self.assertIsNotNone(reopen_blocked(self._existing(status, old), self.SRC, NOW))

    def test_when_applied_blocks_inside_cooldown_then_reopens(self):
        fresh = self._existing("applied", NOW - REOPEN_COOLDOWN_MS + 60_000)
        self.assertIsNotNone(reopen_blocked(fresh, self.SRC, NOW))
        cooled = self._existing("applied", NOW - REOPEN_COOLDOWN_MS - 60_000)
        self.assertIsNone(reopen_blocked(cooled, self.SRC, NOW))

    def test_when_reopen_cap_blocks_after_six_twins(self):
        # Cap is 5; the 6th twin permanently blocks (Go selfCorrectionReopenCap).
        cooled = NOW - REOPEN_COOLDOWN_MS - 60_000
        twins = [
            {"id": f"sc-{i}", "source": self.SRC, "status": "applied", "createdAt": cooled - i}
            for i in range(6)
        ]
        reason = reopen_blocked(twins, self.SRC, NOW)
        self.assertIsNotNone(reason)
        self.assertIn("reopen cap", reason)

    def test_when_newest_twin_wins(self):

        rows = [
            {"id": "old", "source": self.SRC, "status": "applied",
             "createdAt": NOW - 10 * REOPEN_COOLDOWN_MS},
            {"id": "new", "source": self.SRC, "status": "proposed", "createdAt": NOW - 1000},
        ]
        self.assertIn("new", reopen_blocked(rows, self.SRC, NOW) or "")


class SelectCandidatesTest(unittest.TestCase):
    def test_when_blocked_rows_do_not_consume_cap(self):
        cands = structural_candidates(structural_report())
        existing = [{
            "id": "sc-1", "source": cands[0]["source"], "status": "proposed", "createdAt": NOW,
        }]
        selected, skipped = select_candidates(cands, existing, NOW, cap=1)
        self.assertEqual([c["source"] for c in selected], [cands[1]["source"]])
        self.assertEqual(len(skipped), 1)
        self.assertIn("proposed twin", skipped[0][1])

    def test_when_cap_enforced(self):
        cands = structural_candidates(structural_report())
        selected, skipped = select_candidates(cands, [], NOW, cap=1)
        self.assertEqual(len(selected), 1)
        self.assertEqual(skipped[0][1], "per-run cap reached")


class ForbiddenTargetPrefilterTest(unittest.TestCase):
    def test_genesis_directory_is_forbidden(self):
        self.assertIsNotNone(
            forbidden_target_reason(
                ["gateway-go/internal/domain/skills/genesis"]
            )
        )

    def test_ordinary_package_is_allowed(self):
        self.assertIsNone(
            forbidden_target_reason(
                ["gateway-go/internal/runtime/rpc/handler/handlerminiapp"]
            )
        )

    def test_structural_candidates_drop_forbidden_paths(self):
        report = structural_report(
            findings=[
                {
                    "id": "volatile-hub:deadbeef",
                    "pillar": "change-blast",
                    "severity": "high",
                    "path": "gateway-go/internal/domain/skills/genesis",
                    "evidence": "volatile hub",
                    "why": "forbidden acceptor package",
                    "remediation": "do not file",
                    "verify": "n/a",
                    "priority": 99.0,
                    "related_paths": [],
                },
                {
                    "id": "fanout-hotspot:e3983434e99a",
                    "pillar": "boundary-integrity",
                    "severity": "high",
                    "path": "gateway-go/internal/runtime/server",
                    "evidence": "fanout",
                    "why": "broad surface",
                    "remediation": "ports",
                    "verify": "bench",
                    "priority": 95.0,
                    "related_paths": [],
                },
            ]
        )
        cands = structural_candidates(report)
        sources = [c["source"] for c in cands]
        self.assertNotIn("health-finding:volatile-hub:deadbeef", sources)
        self.assertIn("health-finding:fanout-hotspot:e3983434e99a", sources)


class ParseLeadingJsonTest(unittest.TestCase):
    def test_when_trailing_metric_lines_tolerated(self):
        text = '{"composite": 82.3}\nmetric_value=82.3\nDENEB_RUNTIME_DETAIL {...}\n'
        self.assertEqual(parse_leading_json(text), {"composite": 82.3})


class PendingImpactObservationsTest(unittest.TestCase):
    def _candidate(self, metric, *, updated=NOW - 1000, window=0):
        return {
            "id": "sc-impact",
            "attemptId": "attempt-1",
            "updatedAt": updated,
            "impactResult": {"status": "pending"},
            "impactContract": {
                "metric": metric,
                "observationWindowMs": window,
            },
        }

    def test_structural_finding_absence_becomes_zero_observation(self):
        candidate = self._candidate(
            "health.finding_present:volatile-hub:46a381ef4981"
        )
        observations, skipped = pending_impact_observations(
            [candidate], structural_report(findings=[]), runtime_report(), NOW
        )
        self.assertEqual(skipped, [])
        self.assertEqual(observations[0]["observed"], 0)
        self.assertEqual(observations[0]["samples"], 1)
        self.assertIn("absent", observations[0]["note"])

    def test_structural_finding_still_present_becomes_one_observation(self):
        candidate = self._candidate(
            "health.finding_present:volatile-hub:46a381ef4981"
        )
        observations, _ = pending_impact_observations(
            [candidate], structural_report(), runtime_report(), NOW
        )
        self.assertEqual(observations[0]["observed"], 1)

    def test_runtime_score_uses_fresh_dimension_and_sample_count(self):
        candidate = self._candidate("runtime.health.score:latency")
        observations, skipped = pending_impact_observations(
            [candidate], structural_report(), runtime_report(), NOW
        )
        self.assertEqual(skipped, [])
        self.assertEqual(observations[0]["observed"], 51.1)
        self.assertEqual(observations[0]["samples"], 523)

    def test_health_score_namespaces_resolve_overall_domain_and_metric(self):
        candidates = [
            self._candidate("health.score:overall"),
            self._candidate("health.domain.score:structure"),
            self._candidate("health.metric.score:structure/change-blast"),
        ]
        report = structural_report(
            score={"overall": 47.4, "domains": {"structure": 50.4}},
            domains=[{
                "id": "structure", "score": 50.4,
                "metrics": [{"id": "change-blast", "score": 32.2}],
            }],
        )
        for index, candidate in enumerate(candidates):
            candidate["id"] = f"health-{index}"
        observations, skipped = pending_impact_observations(
            candidates, report, runtime_report(), NOW
        )
        self.assertEqual(skipped, [])
        self.assertEqual([row["observed"] for row in observations], [47.4, 50.4, 32.2])

    def test_rsi_bench_namespaces_resolve_overall_domain_and_metric(self):
        candidates = [
            self._candidate("rsi.bench.score:overall"),
            self._candidate("rsi.bench.domain.score:utility"),
            self._candidate("rsi.bench.metric.score:dispatch-land"),
        ]
        for index, candidate in enumerate(candidates):
            candidate["id"] = f"rsi-{index}"
        observations, skipped = pending_impact_observations(
            candidates, structural_report(), runtime_report(), NOW, rsi_report()
        )
        self.assertEqual(skipped, [])
        self.assertEqual([row["observed"] for row in observations], [25.8, 20.4, 22.0])
        self.assertTrue(all(row["samples"] == 1 for row in observations))

    def test_known_but_unavailable_metric_stays_pending(self):
        candidates = [
            self._candidate("health.domain.score:missing"),
            self._candidate("rsi.bench.score:overall"),
        ]
        candidates[1]["id"] = "rsi-missing"
        observations, skipped = pending_impact_observations(
            candidates, structural_report(), runtime_report(), NOW
        )
        self.assertEqual(observations, [])
        self.assertIn("health domain unavailable", skipped[0][1])
        self.assertIn("RSI Bench report unavailable", skipped[1][1])

    def test_observation_window_and_foreign_metric_stay_pending(self):
        candidates = [
            self._candidate(
                "runtime.health.score:latency", updated=NOW, window=60_000
            ),
            self._candidate("external.metric"),
        ]
        observations, skipped = pending_impact_observations(
            candidates, structural_report(), runtime_report(), NOW
        )
        self.assertEqual(observations, [])
        self.assertEqual(len(skipped), 2)
        self.assertIn("window pending", skipped[0][1])
        self.assertIn("another evaluator", skipped[1][1])


class CliDryRunTest(unittest.TestCase):
    def test_dry_run_with_fixture_reports_needs_no_gateway(self):
        with tempfile.TemporaryDirectory() as tmp:
            report_path = os.path.join(tmp, "health.json")
            runtime_path = os.path.join(tmp, "runtime.json")
            with open(report_path, "w", encoding="utf-8") as handle:
                json.dump(structural_report(), handle)
            with open(runtime_path, "w", encoding="utf-8") as handle:
                json.dump(runtime_report(), handle)
            out, err = io.StringIO(), io.StringIO()
            rc = main(
                ["--report", report_path, "--runtime-report", runtime_path,
                 "--dry-run", "--json", "--url", "http://127.0.0.1:1",
                 "--token", "t"],
                stdout=out, stderr=err,
            )
            self.assertEqual(rc, 0)
            self.assertIn("DRY-RUN continues WITHOUT dedup", err.getvalue())
            lines = out.getvalue().strip().splitlines()
            summary = json.loads(lines[-1])
            self.assertEqual(summary["planned"], 3)  # 2 structural + 1 runtime
            self.assertEqual(summary["filed"], 0)
            self.assertTrue(summary["dry_run"])
            self.assertIn("health-finding:runtime-latency", out.getvalue())

    def test_when_real_run_refuses_to_file_blind(self):
        with tempfile.TemporaryDirectory() as tmp:
            report_path = os.path.join(tmp, "health.json")
            with open(report_path, "w", encoding="utf-8") as handle:
                json.dump(structural_report(), handle)
            out, err = io.StringIO(), io.StringIO()
            rc = main(
                ["--report", report_path, "--runtime-report", report_path,
                 "--url", "http://127.0.0.1:1", "--token", "t"],
                stdout=out, stderr=err,
            )
            self.assertEqual(rc, 1)
            self.assertIn("refusing to file blind", err.getvalue())


if __name__ == "__main__":
    unittest.main()


class IncrementalContractTests(unittest.TestCase):
    """The responsibility/fan-out family gets a bounded-step contract."""

    def test_incremental_kind_gets_bounded_step_verify_and_no_impact_contract(self):
        report = structural_report()
        report["findings"].append({
            "id": "fanout-hotspot:deadbeef0001",
            "pillar": "boundary-integrity",
            "severity": "high",
            "path": "gateway-go/internal/pipeline/chat",
            "evidence": "Direct internal fan-out is 90; bars are 20/50.",
            "why": "A broad dependency surface.",
            "remediation": "Depend on narrow capability ports.",
            "verify": "Re-run the bench and confirm this finding disappears.",
            "priority": 96.0,
            "related_paths": [],
        })
        cands = structural_candidates(report)
        fan = [c for c in cands if c["source"].endswith("deadbeef0001")]
        self.assertEqual(len(fan), 1)
        c = fan[0]
        # The disappearance ask is replaced by the bounded-step contract.
        self.assertNotIn("confirm this finding disappears", c["proposedChange"])
        self.assertIn("CANNOT disappear in one session", c["proposedChange"])
        self.assertIn("ONE bounded structural step", c["proposedChange"])
        # No finding-present impact contract for the incremental family.
        self.assertNotIn("impactContract", c)

    def test_non_incremental_kind_keeps_disappearance_contract(self):
        cands = structural_candidates(structural_report())
        hub = [c for c in cands if c["source"].startswith("health-finding:volatile-hub:")]
        self.assertEqual(len(hub), 1)
        self.assertIn("Verify:", hub[0]["proposedChange"])
        self.assertIn("impactContract", hub[0])
        self.assertEqual(hub[0]["impactContract"]["target"], 0)

    def test_v3_domain_prefixed_incremental_id_is_recognized(self):
        report = structural_report()
        report["schema_version"] = 3
        for f in report["findings"]:
            f["domain"] = "structure"
        report["findings"].append({
            "id": "structure:diffuse-change-responsibility:cafe00112233",
            "domain": "structure",
            "pillar": "responsibility-cohesion",
            "severity": "high",
            "path": "gateway-go/internal/platform/mailarchive",
            "evidence": "Partner entropy=0.81 across 5 components.",
            "why": "No focused component boundary.",
            "remediation": "Choose one owning capability.",
            "verify": "Re-run the bench and confirm this finding disappears.",
            "priority": 91.0,
            "related_paths": [],
        })
        cands = structural_candidates(report)
        diff = [c for c in cands if "cafe00112233" in c["source"]]
        self.assertEqual(len(diff), 1)
        self.assertIn("CANNOT disappear in one session", diff[0]["proposedChange"])
        self.assertNotIn("impactContract", diff[0])


class MinerStatusTests(unittest.TestCase):
    """The status drop is the anti-silent-degradation hook: rsi-status L4 reads
    it, so a v3→v2 fallback (nine silent days, 2026-07-18 → 07-27) or a dead
    bench shows on the operator card instead of only in unit stderr."""

    def test_write_miner_status_roundtrip_and_atomic(self):
        import health_finding_miner as hfm

        with tempfile.TemporaryDirectory() as tmp:
            path = os.path.join(tmp, "nested", "status.json")
            err = io.StringIO()
            hfm.write_miner_status(
                {"lastRunAtMs": 1, "structuralSource": "health-bench-v3", "fallbackReason": ""},
                err, path=path,
            )
            with open(path, encoding="utf-8") as handle:
                payload = json.load(handle)
            self.assertEqual(payload["structuralSource"], "health-bench-v3")
            self.assertFalse(os.path.exists(path + ".tmp"))
            self.assertEqual(err.getvalue(), "")

    def test_write_miner_status_never_raises(self):
        import health_finding_miner as hfm

        err = io.StringIO()
        # Directory path where the file should be — open() fails, but the miner run must not.
        with tempfile.TemporaryDirectory() as tmp:
            hfm.write_miner_status({"a": 1}, err, path=tmp)
        self.assertIn("WARN", err.getvalue())

    def test_main_records_unavailable_bench_status(self):
        import health_finding_miner as hfm

        with tempfile.TemporaryDirectory() as tmp:
            status_path = os.path.join(tmp, "status.json")
            original = hfm.miner_status_path
            hfm.miner_status_path = lambda: status_path
            try:
                rc = main(
                    ["--report", os.path.join(tmp, "missing.json"), "--url", "http://127.0.0.1:1"],
                    stdout=io.StringIO(), stderr=io.StringIO(),
                )
            finally:
                hfm.miner_status_path = original
            self.assertEqual(rc, 1)
            with open(status_path, encoding="utf-8") as handle:
                payload = json.load(handle)
            self.assertEqual(payload["structuralSource"], "unavailable")
            self.assertTrue(payload["fallbackReason"])
