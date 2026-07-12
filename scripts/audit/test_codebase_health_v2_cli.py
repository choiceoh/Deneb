"""Deep-evidence and CLI stream contracts for Health Bench 2.0."""

from __future__ import annotations

import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import codebase_health_v2 as health
from health_v2.baseline import snapshot
from test_codebase_health_v2_support import report as _report


class DeepEvidenceTests(unittest.TestCase):
    def test_deep_evidence_wrong_golangci_version_is_unavailable(self) -> None:
        def fake_run(
            command: list[str], _cwd: Path, *, timeout: int
        ) -> tuple[bool | None, str]:
            self.assertGreater(timeout, 0)
            if command == ["golangci-lint", "version"]:
                return True, "golangci-lint has version 2.4.0 built with go1.24"
            return True, "fixture passed"

        runner = mock.Mock(side_effect=fake_run)
        with mock.patch.object(health, "_run", runner):
            readiness, evidence, coverage = health._deep_evidence(Path("/fixture"))

        lint = next(item for item in evidence if item.name == "go-lint")
        commands = [call.args[0] for call in runner.call_args_list]
        self.assertIsNone(readiness["go-lint"])
        self.assertEqual(lint.status, "unavailable")
        self.assertIn("expected golangci-lint 2.5.x", lint.detail)
        self.assertNotIn(["golangci-lint", "run", "./..."], commands)
        self.assertIsNone(coverage)

    def test_deep_delivery_excludes_format_and_raw_coverage_from_score(self) -> None:
        delivery = health.Pillar(
            id="delivery-confidence",
            title="Delivery",
            weight=8,
            score=50.0,
            intent="Prevent product-relevant failures.",
        )
        readiness = {
            "go-format": False,
            "go-vet": True,
            "go-lint": True,
            "go-test": True,
            "go-race": True,
        }

        health._apply_deep_delivery([delivery], readiness, coverage=0.0)

        self.assertEqual(delivery.score, 70.0)
        self.assertEqual(delivery.metrics["deep_execution_score"], 100.0)
        self.assertNotIn("go-format", delivery.metrics["deep_execution_checks"])
        self.assertEqual(
            delivery.metrics["go_statement_coverage"],
            {
                "scored": False,
                "value": 0.0,
                "reason": "raw statement coverage does not prove behavior or oracle quality",
            },
        )


class CLIContractTests(unittest.TestCase):
    def test_main_failed_deep_readiness_is_required_when_requested(self) -> None:
        report = _report(readiness={"go-test": False})
        report.profile = "deep"
        stdout = io.StringIO()
        stderr = io.StringIO()

        with (
            mock.patch.object(health, "collect_report", return_value=report) as collect,
            mock.patch.object(health.sys, "stdout", stdout),
            mock.patch.object(health.sys, "stderr", stderr),
        ):
            exit_code = health.main(["--deep", "--require-readiness"])

        self.assertEqual(exit_code, 1)
        collect.assert_called_once_with(
            profile="deep", readiness_passed=set(), readiness_failed=set()
        )
        self.assertIn("executable readiness is failed or unmeasured", stdout.getvalue())
        self.assertEqual(stderr.getvalue(), "")

    def test_main_json_check_keeps_diagnostics_on_stderr(self) -> None:
        accepted = snapshot(_report())
        current = _report(69.0, 70.0)
        stdout = io.StringIO()
        stderr = io.StringIO()

        with tempfile.TemporaryDirectory(prefix="deneb-health-cli-") as folder:
            baseline = Path(folder) / "baseline.json"
            baseline.write_text(
                json.dumps(accepted, indent=2, sort_keys=True) + "\n",
                encoding="utf-8",
            )
            with (
                mock.patch.object(health, "collect_report", return_value=current),
                mock.patch.object(health.sys, "stdout", stdout),
                mock.patch.object(health.sys, "stderr", stderr),
            ):
                exit_code = health.main(
                    ["--format", "json", "--check", "--baseline", str(baseline)]
                )

        payload = json.loads(stdout.getvalue())
        self.assertEqual(exit_code, 1)
        self.assertEqual(payload["score"]["overall"], 69.5)
        self.assertNotIn("REGRESSION", stdout.getvalue())
        self.assertIn("REGRESSION: overall score", stderr.getvalue())

    def test_main_explicit_rubric_migration_uses_reviewed_score_band(self) -> None:
        report = _report(51.0, 49.0)
        stdout = io.StringIO()
        stderr = io.StringIO()

        with tempfile.TemporaryDirectory(prefix="deneb-health-cli-migration-") as folder:
            source = Path(folder) / "v2.0.json"
            target = Path(folder) / "v2.1.json"
            source.write_text(
                json.dumps(
                    {
                        "schema_version": 2,
                        "rubric_version": "2.0.0",
                        "profile": "fast",
                        "overall": 47.4,
                    }
                ),
                encoding="utf-8",
            )
            with (
                mock.patch.object(health, "collect_report", return_value=report),
                mock.patch.object(health.sys, "stdout", stdout),
                mock.patch.object(health.sys, "stderr", stderr),
            ):
                exit_code = health.main(
                    [
                        "--format",
                        "json",
                        "--migrate-rubric",
                        str(source),
                        "--migration-reason",
                        "Remove non-quality deductions in the fixture rubric.",
                        "--expect-band",
                        "45:55",
                        "--write-baseline",
                        str(target),
                    ]
                )
            migrated = json.loads(target.read_text(encoding="utf-8"))

        self.assertEqual(exit_code, 0)
        self.assertEqual(stderr.getvalue(), "")
        self.assertEqual(migrated["rubric_version"], "2.1.2")
        self.assertEqual(migrated["provenance"]["previous_score"], 47.4)
        self.assertEqual(
            migrated["provenance"]["reason"],
            "Remove non-quality deductions in the fixture rubric.",
        )


if __name__ == "__main__":
    unittest.main()
