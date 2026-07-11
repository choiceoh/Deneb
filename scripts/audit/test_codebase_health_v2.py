"""Model, baseline, report, and schema contracts for Health Bench 2.0."""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from test_codebase_health_v2_support import (
    AUDIT_DIR,
    assert_schema as _assert_schema,
    finding as _finding,
    report as _report,
)
from health_v2.baseline import (
    BaselineError,
    BaselineRegressionError,
    check,
    load,
    migrate,
    snapshot,
    update,
)
from health_v2.model import (
    Evidence,
    Pillar,
    Report,
    ascending_grade,
    descending_grade,
    geometric_mean,
    stable_id,
    tail_score,
)
from health_v2.report import render_human


class ModelContractTests(unittest.TestCase):
    def test_report_requires_weights_totaling_one_hundred(self) -> None:
        with self.assertRaisesRegex(ValueError, "weights must total 100"):
            Report(
                profile="fast",
                revision="abcdef1",
                pillars=[Pillar("only", "Only", 99, 50, "fixture")],
                evidence=[],
                readiness={},
            )

    def test_report_rejects_duplicate_pillar_and_finding_ids(self) -> None:
        with self.assertRaisesRegex(ValueError, "pillar ids must be unique"):
            Report(
                profile="fast",
                revision="abcdef1",
                pillars=[
                    Pillar("same", "First", 50, 50, "fixture"),
                    Pillar("same", "Second", 50, 50, "fixture"),
                ],
                evidence=[],
                readiness={},
            )

        with self.assertRaisesRegex(ValueError, "finding ids must be unique"):
            Report(
                profile="fast",
                revision="abcdef1",
                pillars=[
                    Pillar(
                        "alpha",
                        "Alpha",
                        50,
                        50,
                        "fixture",
                        findings=[_finding("same")],
                    ),
                    Pillar(
                        "beta",
                        "Beta",
                        50,
                        50,
                        "fixture",
                        findings=[_finding("same", pillar="beta")],
                    ),
                ],
                evidence=[],
                readiness={},
            )

    def test_json_and_finding_order_are_deterministic(self) -> None:
        report = _report(
            findings=[
                _finding("lower-priority", priority=10),
                _finding("b-tie", priority=70),
                _finding("a-tie", priority=70),
            ]
        )
        first = json.dumps(
            report.to_dict(), indent=2, sort_keys=True, ensure_ascii=False
        )
        second = json.dumps(
            report.to_dict(), indent=2, sort_keys=True, ensure_ascii=False
        )

        self.assertEqual(first, second)
        self.assertEqual(
            [item["id"] for item in json.loads(first)["findings"]],
            ["a-tie", "b-tie", "lower-priority"],
        )
        self.assertEqual(
            stable_id("rule", "path", "symbol"),
            stable_id("rule", "path", "symbol"),
        )

    def test_unavailable_evidence_or_readiness_is_not_healthy(self) -> None:
        unknown_readiness = _report(readiness={"go-test": None})
        missing_evidence = _report(
            evidence=[Evidence("inventory", "unavailable", "git failed", required=True)],
            readiness={"go-test": True},
        )

        self.assertFalse(unknown_readiness.healthy)
        self.assertFalse(missing_evidence.healthy)
        self.assertLess(missing_evidence.confidence, 100.0)

    def test_score_helpers_are_monotonic(self) -> None:
        values = [-1, 0, 5, 10, 15, 20, 30]
        descending = [descending_grade(value, 5, 20) for value in values]
        ascending = [ascending_grade(value, 5, 20) for value in values]

        self.assertEqual(descending, sorted(descending, reverse=True))
        self.assertEqual(ascending, sorted(ascending))
        self.assertLessEqual(
            tail_score([10, 20, 90], soft=20, hard=100),
            tail_score([10, 20, 80], soft=20, hard=100),
        )
        self.assertGreater(geometric_mean([60, 90]), geometric_mean([50, 90]))
        with self.assertRaises(ValueError):
            descending_grade(10, 20, 20)
        with self.assertRaises(ValueError):
            ascending_grade(10, 20, 20)


class BaselineRatchetTests(unittest.TestCase):
    def test_overall_and_pillar_regressions_cannot_mask_each_other(self) -> None:
        accepted = snapshot(_report())

        overall = check(_report(69.5, 69.5), accepted)
        masked_pillar = check(_report(68.0, 72.0), accepted)

        self.assertFalse(overall.ok)
        self.assertTrue(any("overall score" in item for item in overall.regressions))
        self.assertFalse(masked_pillar.ok)
        self.assertTrue(any("pillar alpha" in item for item in masked_pillar.regressions))
        self.assertFalse(
            any("overall score" in item for item in masked_pillar.regressions)
        )

    def test_new_high_finding_fails_even_when_scores_do_not_regress(self) -> None:
        accepted = snapshot(_report())
        current = _report(findings=[_finding("new-contract-gap", severity="high")])

        result = check(current, accepted)

        self.assertFalse(result.ok)
        self.assertEqual(result.new_findings, ("new-contract-gap",))
        self.assertTrue(any("new high finding" in item for item in result.regressions))

    def test_update_refuses_to_lower_baseline(self) -> None:
        with tempfile.TemporaryDirectory(prefix="deneb-health-baseline-") as folder:
            path = Path(folder) / "baseline.json"
            accepted = update(path, _report())

            with self.assertRaises(BaselineRegressionError):
                update(path, _report(69.9, 70.0))

            self.assertEqual(load(path), accepted)

    def test_update_preserves_migration_provenance(self) -> None:
        with tempfile.TemporaryDirectory(prefix="deneb-health-provenance-") as folder:
            path = Path(folder) / "baseline.json"
            provenance = {"migration": "v1-to-v2", "v1_baseline_score": 77.1}
            update(path, _report(), provenance=provenance)

            updated = update(path, _report(71.0, 70.0))

            self.assertEqual(updated["provenance"], provenance)

    def test_rubric_migration_remeasures_and_preserves_previous_provenance(
        self,
    ) -> None:
        source = {
            "schema_version": 2,
            "rubric_version": "2.0.0",
            "profile": "fast",
            "revision": "old-revision",
            "overall": 97.0,
            "pillars": {"legacy": 97.0},
            "high_findings": {},
            "provenance": {"migration": "v1-to-v2-remeasurement"},
        }
        with tempfile.TemporaryDirectory(prefix="deneb-health-migration-") as folder:
            path = Path(folder) / "baseline.json"

            migrated = migrate(
                path,
                _report(51.0, 49.0),
                source,
                provenance={"approved_for": "rubric-2.1-fixture"},
            )

            self.assertEqual(load(path), migrated)
        self.assertEqual(migrated["rubric_version"], "2.1.1")
        self.assertEqual(migrated["overall"], 50.0)
        self.assertEqual(
            migrated["provenance"],
            {
                "approved_for": "rubric-2.1-fixture",
                "migration": "rubric-remeasurement",
                "previous_rubric": "2.0.0",
                "previous_score": 97.0,
                "note": (
                    "Scores are not converted or directly comparable across rubric "
                    "versions."
                ),
                "previous_provenance": {"migration": "v1-to-v2-remeasurement"},
            },
        )

    def test_rubric_migration_rejects_same_rubric_and_profile_mismatch(self) -> None:
        with tempfile.TemporaryDirectory(prefix="deneb-health-migration-") as folder:
            path = Path(folder) / "baseline.json"
            with self.assertRaisesRegex(BaselineError, "already uses rubric 2.1.1"):
                migrate(path, _report(), snapshot(_report()))

            old_deep = {
                "schema_version": 2,
                "rubric_version": "2.0.0",
                "profile": "deep",
                "overall": 70.0,
            }
            with self.assertRaisesRegex(BaselineError, "does not match migration source"):
                migrate(path, _report(), old_deep)


class HumanReportTests(unittest.TestCase):
    def test_action_fields_appear_before_score(self) -> None:
        output = render_human(
            _report(findings=[_finding("contract-gap", severity="high")])
        )
        labels = ["What:", "Why:", "Where:", "How:", "Verify:", "Score:"]
        offsets = [output.index(label) for label in labels]

        self.assertEqual(offsets, sorted(offsets))


class SchemaContractTests(unittest.TestCase):
    def test_report_and_baseline_payloads_satisfy_checked_in_schemas(self) -> None:
        report_schema_path = AUDIT_DIR / "health-v2-report.schema.json"
        baseline_schema_path = AUDIT_DIR / "health-v2-baseline.schema.json"
        report_schema = json.loads(report_schema_path.read_text(encoding="utf-8"))
        baseline_schema = json.loads(baseline_schema_path.read_text(encoding="utf-8"))
        report_payload = _report(findings=[_finding("schema-fixture")]).to_dict()
        baseline_payload = snapshot(_report())

        self.assertEqual(
            report_schema["$schema"], "https://json-schema.org/draft/2020-12/schema"
        )
        self.assertEqual(
            baseline_schema["$schema"],
            "https://json-schema.org/draft/2020-12/schema",
        )
        _assert_schema(
            self, json.loads(json.dumps(report_payload)), report_schema, "report"
        )
        _assert_schema(
            self,
            json.loads(json.dumps(baseline_payload)),
            baseline_schema,
            "baseline",
        )


if __name__ == "__main__":
    unittest.main()
