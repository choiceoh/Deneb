"""Behavior-oriented delivery scoring contracts for Health Bench 2.1."""

from __future__ import annotations

import unittest
from pathlib import Path
from unittest import mock

from health_v2 import delivery, testing
from health_v2.model import PRODUCT_LANE_IMPACT
from test_codebase_health_v2_support import GitFixture


class DeliveryCapabilityTests(unittest.TestCase):
    def test_git_spawn_error_returns_required_unavailable_evidence(self) -> None:
        with mock.patch.object(
            delivery.subprocess,
            "run",
            side_effect=OSError("git executable unavailable"),
        ):
            pillars, evidence = delivery.evaluate(Path("/fixture"))

        self.assertEqual(pillars, [])
        self.assertEqual(len(evidence), 1)
        self.assertEqual(evidence[0].status, "unavailable")
        self.assertTrue(evidence[0].required)
        self.assertIn("tracked-file inventory unavailable", evidence[0].detail)

    def test_workflow_read_error_is_skipped_without_false_gate_evidence(self) -> None:
        tracked = frozenset({".github/workflows/verify.yml"})
        workflow = """\
name: quality
on: push
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: go test ./...
"""
        with GitFixture() as fixture:
            fixture.write(".github/workflows/verify.yml", workflow)
            fixture.track()
            with mock.patch.object(
                Path,
                "read_text",
                side_effect=OSError("workflow vanished"),
            ):
                workflows = delivery._read_workflows(fixture.root, tracked)

        self.assertEqual(workflows, {})

    def test_scores_only_executable_failure_prevention_commands(self) -> None:
        fake_workflow = """\
# gofmt; go build; go test -tags=integration; pnpm exec playwright test
name: gofmt go build playwright presentation only
on: push
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok # go test -tags=integration; playwright
      - run: echo 'go build ./...'
      - run: printf 'pnpm exec playwright test'
      - run: CMD='go test -count=1 ./...'
      - run: if false; then go build ./...; fi
"""
        direct_workflow = """\
name: quality
on: push
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: |
          go test -tags=integration ./...
"""
        disabled_workflow = """\
name: disabled gates
on: push
jobs:
  dead-job:
    if: false
    runs-on: ubuntu-latest
    steps:
      - run: go build ./...
  verify:
    runs-on: ubuntu-latest
    steps:
      - if: ${{ false }}
        run: pnpm exec playwright test
      - run: echo live
"""
        advisory_workflow = """\
name: advisory only
on: workflow_dispatch # push is documentation, not an event
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: go build ./...
"""
        main_ignored_workflow = """\
name: never on main
on:
  push:
    branches-ignore: [main]
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: go build ./...
"""
        dev_only_workflow = """\
name: development branch only
on:
  push:
    branches:
      - experimental/**
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: go build ./...
"""
        filtered_pr_workflow = """\
name: non-main pull requests only
on:
  pull_request:
    branches-ignore: [main]
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: go build ./...
"""
        docs_only_workflow = """\
name: documentation pull requests only
on:
  pull_request:
    paths: [docs/**, "**/*.md"]
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: go build ./...
"""
        nonblocking_workflow = """\
name: nonblocking gates
on: pull_request
jobs:
  ignored-job:
    continue-on-error: ${{ true }}
    runs-on: ubuntu-latest
    steps:
      - run: go build ./...
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: pnpm exec playwright test || true
"""
        delegated_workflow = """\
name: quality
on: push
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/quality
      - run: make web-check
"""
        local_action = """\
name: local quality
runs:
  using: composite
  steps:
    - shell: bash
      run: |
        go build ./...
        go test -tags=integration ./...
"""

        def capabilities(root: Path) -> dict[tuple[str, str], bool]:
            workflows = delivery._read_workflows(root)
            return {
                (item.lane, item.name): item.present
                for item in delivery._capabilities(root, workflows)
            }

        with GitFixture() as fixture:
            workflow = fixture.root / ".github/workflows/verify.yml"
            fixture.write(".github/workflows/verify.yml", fake_workflow)
            fixture.track()
            fake = capabilities(fixture.root)
            scored_names = {name for _, name in fake}
            self.assertTrue(
                {"format", "machine-result", "coverage"}.isdisjoint(scored_names)
            )
            self.assertNotIn(("go", "format"), fake)
            self.assertFalse(fake[("go", "build")])
            self.assertFalse(fake[("go", "integration")])
            self.assertFalse(fake[("typescript", "integration")])

            fixture.write(".github/workflows/verify.yml", advisory_workflow)
            fixture.track()
            self.assertEqual(delivery._read_workflows(fixture.root), {})

            fixture.write(".github/workflows/verify.yml", main_ignored_workflow)
            fixture.track()
            self.assertEqual(delivery._read_workflows(fixture.root), {})

            fixture.write(".github/workflows/verify.yml", dev_only_workflow)
            fixture.track()
            self.assertEqual(delivery._read_workflows(fixture.root), {})

            fixture.write(".github/workflows/verify.yml", filtered_pr_workflow)
            fixture.track()
            self.assertEqual(delivery._read_workflows(fixture.root), {})

            fixture.write(".github/workflows/verify.yml", docs_only_workflow)
            fixture.track()
            self.assertEqual(delivery._read_workflows(fixture.root), {})

            fixture.write(".github/workflows/verify.yml", nonblocking_workflow)
            fixture.track()
            nonblocking = capabilities(fixture.root)
            self.assertFalse(nonblocking[("go", "build")])
            self.assertFalse(nonblocking[("typescript", "integration")])

            fixture.write(".github/workflows/verify.yml", disabled_workflow)
            fixture.track()
            disabled = capabilities(fixture.root)
            self.assertFalse(disabled[("go", "build")])
            self.assertFalse(disabled[("typescript", "integration")])

            fixture.write(".github/workflows/verify.yml", direct_workflow)
            fixture.track()
            direct = capabilities(fixture.root)
            self.assertTrue(direct[("go", "integration")])
            self.assertFalse(direct[("typescript", "integration")])

            fixture.write(".github/workflows/verify.yml", delegated_workflow)
            fixture.write(".github/actions/quality/action.yml", local_action)
            fixture.write("Makefile", "web-check:\n\tpnpm exec playwright test\n")
            fixture.track()
            expanded = capabilities(fixture.root)
            self.assertTrue(expanded[("go", "build")])
            self.assertTrue(expanded[("go", "integration")])
            self.assertTrue(expanded[("typescript", "integration")])
            before, _ = delivery.evaluate(fixture.root)

            renamed = workflow.with_name("renamed-quality.yaml")
            workflow.rename(renamed)
            fixture.track()
            after, _ = delivery.evaluate(fixture.root)

        self.assertEqual(before[0].score, after[0].score)
        self.assertEqual(before[0].metrics, after[0].metrics)

    def test_changes_composite_when_capability_importance_and_lane_impact_shift(self) -> None:
        def capability(
            lane: str, name: str, present: bool, importance: float
        ) -> delivery._Capability:
            return delivery._Capability(
                lane=lane,
                name=name,
                present=present,
                importance=importance,
                evidence=f"{name} evidence",
                remediation=f"Enable {name}.",
            )

        def score(capabilities: list[delivery._Capability]):
            with (
                mock.patch.object(
                    delivery, "_read_workflows", return_value={"fixture.yml": "run"}
                ),
                mock.patch.object(
                    delivery, "_tracked_files", return_value=frozenset()
                ),
                mock.patch.object(
                    delivery, "_capabilities", return_value=capabilities
                ),
            ):
                pillars, _ = delivery.evaluate(Path("/fixture"))
            return pillars[0]

        go_major_missing = score(
            [
                capability("go", "major", False, 3),
                capability("go", "minor", True, 1),
                capability("python", "major", True, 3),
                capability("python", "minor", True, 1),
            ]
        )
        go_minor_missing = score(
            [
                capability("go", "major", True, 3),
                capability("go", "minor", False, 1),
                capability("python", "major", True, 3),
                capability("python", "minor", True, 1),
            ]
        )
        python_major_missing = score(
            [
                capability("go", "major", True, 3),
                capability("go", "minor", True, 1),
                capability("python", "major", False, 3),
                capability("python", "minor", True, 1),
            ]
        )

        self.assertLess(go_major_missing.score, go_minor_missing.score)
        self.assertLess(go_major_missing.score, python_major_missing.score)
        self.assertEqual(go_major_missing.metrics["lane_scores"]["go"], 25.0)
        self.assertEqual(go_major_missing.metrics["lane_scores"]["python"], 100.0)
        self.assertGreater(
            go_major_missing.metrics["lane_impact"]["go"],
            go_major_missing.metrics["lane_impact"]["python"],
        )
        impact_total = PRODUCT_LANE_IMPACT["go"] + PRODUCT_LANE_IMPACT["python"]
        self.assertEqual(
            go_major_missing.metrics["lane_impact"]["go"],
            round(PRODUCT_LANE_IMPACT["go"] / impact_total, 3),
        )

    def test_preserves_score_when_untracked_delivery_evidence_added(self) -> None:
        tracked_workflow = """\
name: tracked gate
on: pull_request
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: go test -count=1 ./...
"""
        scratch_workflow = """\
name: untracked score laundering
on: pull_request
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: |
          go build ./...
          go test -race ./...
          pnpm run build
          pnpm exec playwright test
          shellcheck scripts/**/*.sh
"""
        with GitFixture() as fixture:
            fixture.write(".github/workflows/tracked.yml", tracked_workflow)
            fixture.commit("seed tracked delivery gate")
            before, _ = delivery.evaluate(fixture.root)

            fixture.write(".github/workflows/archive/fake.yml", scratch_workflow)
            fixture.commit("archive a non-workflow fixture")
            nested, _ = delivery.evaluate(fixture.root)

            fixture.write(".github/workflows/scratch.yml", scratch_workflow)
            fixture.write("scripts/deploy/scratch.sh", "#!/bin/sh\nexit 0\n")
            fixture.write("uv.lock", "untracked dependency lock\n")
            after, _ = delivery.evaluate(fixture.root)

        self.assertEqual(before[0].score, nested[0].score)
        self.assertEqual(before[0].metrics, nested[0].metrics)
        self.assertEqual(before[0].score, after[0].score)
        self.assertEqual(before[0].metrics, after[0].metrics)

    def test_when_applies_same_product_impact_weighting_to_test_evidence(self) -> None:
        go_gap = testing._language_average({"go": 0.0, "python": 100.0})
        python_gap = testing._language_average({"go": 100.0, "python": 0.0})

        self.assertLess(go_gap, python_gap)
        self.assertAlmostEqual(
            go_gap,
            100.0
            * PRODUCT_LANE_IMPACT["python"]
            / (PRODUCT_LANE_IMPACT["go"] + PRODUCT_LANE_IMPACT["python"]),
        )

    def test_limits_support_lane_gap_severity_without_core_escalation(self) -> None:
        def finding(language: str):
            return testing._signal_finding(
                pillar="test-effectiveness",
                signal="fixture-gap",
                score=0.0,
                language=language,
                path=f"{language}/fixture",
                evidence="fixture lane has no behavior evidence",
                why="The fixture models a missing test lane.",
                remediation="Add role-appropriate behavior evidence.",
                verify="make health-v2-test",
            )

        go_gap = finding("go")
        python_gap = finding("python")

        self.assertEqual(go_gap.severity, "high")
        self.assertEqual(python_gap.severity, "medium")
        self.assertGreater(go_gap.priority, python_gap.priority)


if __name__ == "__main__":
    unittest.main()
