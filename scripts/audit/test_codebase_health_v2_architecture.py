"""AI-maintainability architecture contracts for Health Bench 2.1."""

from __future__ import annotations

from collections import Counter
from pathlib import Path
import unittest

from health_v2 import ai_readiness, architecture, delivery, inventory, operations, testing
from health_v2.history import ChangeCommit, HistoryFacts
from test_codebase_health_v2_support import GitFixture, pillar


class ArchitectureRubricTests(unittest.TestCase):
    _SOURCE = """\
package widget

func WidgetValue(input int) int { return input }
"""
    _TEST = """\
package widget

import "testing"

func TestWidgetValue(t *testing.T) {
    if got := WidgetValue(2); got != 2 {
        t.Fatalf("got %d", got)
    }
}
"""

    def test_typed_ports_are_not_counted_as_dynamic_contracts(self) -> None:
        source = """\
package widget

type Reader interface {
    Read([]byte) (int, error)
}

type Empty interface{}
type Payload any
"""
        code = inventory._mask_literals(inventory._remove_comments(source))

        exported, dynamic = inventory._exported_contract_counts(code)

        self.assertEqual(exported, 3)
        self.assertEqual(dynamic, 2)

    def test_universal_generic_constraints_are_not_dynamic_values(self) -> None:
        source = """\
package widget

func Decode[T any](raw []byte) (T, error) { var zero T; return zero, nil }
func Identity[T interface{}](value T) T { return value }

type Box[T any] struct {
    Value T
}

type DynamicBox[T any] struct {
    Value any
}

func DynamicInput[T any](value any) T { var zero T; return zero }
func DynamicMap[T ~map[string]any](value T) T { return value }
"""
        code = inventory._mask_literals(inventory._remove_comments(source))

        exported, dynamic = inventory._exported_contract_counts(code)

        self.assertEqual(exported, 6)
        self.assertEqual(dynamic, 3)

    def test_rubric_weights_include_ai_change_readiness_and_total_one_hundred(
        self,
    ) -> None:
        workflow = """\
name: fixture
on: push
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - run: go test -count=1 ./...
"""
        with GitFixture() as fixture:
            fixture.write("gateway-go/go.mod", "module example.invalid/fixture\n")
            fixture.write("gateway-go/internal/widget/widget.go", self._SOURCE)
            fixture.write("gateway-go/internal/widget/widget_test.go", self._TEST)
            fixture.write(".github/workflows/fixture.yml", workflow)
            fixture.track()

            groups = [
                architecture.evaluate(fixture.root)[0],
                operations.evaluate(fixture.root)[0],
                testing.evaluate(fixture.root)[0],
                delivery.evaluate(fixture.root)[0],
            ]

        pillars = [item for group in groups for item in group]
        by_id = {item.id: item for item in pillars}
        self.assertIn("ai-change-readiness", by_id)
        self.assertEqual(by_id["ai-change-readiness"].weight, 8)
        self.assertEqual(sum(item.weight for item in pillars), 100)
        self.assertEqual(len(by_id), len(pillars))

    def test_placeholder_and_false_guide_references_receive_no_credit(self) -> None:
        placeholder = """\
# Widget

TODO: document the entry point, package responsibility, and tests.
"""
        false_guide = """\
# Widget ownership and entry point

This package owns widget behavior and its local architecture boundary. Start in
`ghost_entry.go` at `GhostEntry`, which is claimed to be the registry for every
change. Adapters must depend inward through a narrow contract, and callers must
never bypass that dependency direction or mutate state outside the owner.

## Invariants and verification

The invariant is that changes must preserve deterministic results and failure
propagation. Run `make imaginary-check` before merging. The named file, symbol,
and command intentionally do not exist; descriptive prose alone must not make
an unverified navigation path or definition of done score as executable truth.
"""
        with GitFixture() as fixture:
            fixture.write("gateway-go/go.mod", "module example.invalid/fixture\n")
            fixture.write("gateway-go/internal/widget/widget.go", self._SOURCE)
            fixture.write("gateway-go/internal/widget/widget_test.go", self._TEST)
            fixture.write("gateway-go/internal/widget/CLAUDE.md", placeholder)
            fixture.write("Makefile", "real-check:\n\tgo test ./gateway-go/...\n")
            fixture.track()

            placeholder_repo = inventory.collect(fixture.root)
            placeholder_facts = ai_readiness.collect(placeholder_repo)

            fixture.write("gateway-go/internal/widget/CLAUDE.md", false_guide)
            fixture.track()
            false_repo = inventory.collect(fixture.root)
            false_facts = ai_readiness.collect(false_repo)

        placeholder_signal = placeholder_facts.packages["internal/widget"]
        self.assertIsNone(placeholder_signal.guide_path)
        self.assertEqual(placeholder_signal.guide_quality, 0.0)
        self.assertEqual(placeholder_signal.entrypoint_score, 0.0)
        self.assertEqual(placeholder_signal.verification_score, 0.0)

        false_signal = false_facts.packages["internal/widget"]
        self.assertEqual(false_signal.guide_path, "gateway-go/internal/widget/CLAUDE.md")
        self.assertEqual(false_signal.valid_entry_files, ())
        self.assertEqual(false_signal.valid_entry_symbols, ())
        self.assertEqual(false_signal.valid_commands, ())
        self.assertEqual(false_signal.entrypoint_score, 0.0)
        self.assertEqual(false_signal.verification_score, 0.0)

        navigability = architecture._ai_navigability(false_repo, false_facts)
        readiness = architecture._ai_change_readiness(false_repo, false_facts)
        self.assertEqual(navigability.metrics["risk_weighted_valid_entrypoints"], 0.0)
        self.assertEqual(navigability.metrics["risk_weighted_specific_verification"], 0.0)
        self.assertEqual(readiness.metrics["subscores"]["entrypoint_registry_index"], 0.0)
        self.assertEqual(readiness.metrics["subscores"]["verification_specificity"], 0.0)
        self.assertTrue(any("actual source symbol" in item.why for item in navigability.findings))
        self.assertEqual(pillar([readiness], "ai-change-readiness"), readiness)

    def test_composition_root_frequency_is_preserved_but_not_scored_as_hotspot(
        self,
    ) -> None:
        repo = self._locality_repo(
            [ChangeCommit(packages=(("internal/runtime/server", 1),)) for _ in range(20)]
        )

        result = architecture._change_locality(repo)

        self.assertEqual(result.metrics["observed_hottest_package"], "internal/runtime/server")
        self.assertEqual(result.metrics["observed_hottest_package_share"], 1.0)
        self.assertEqual(result.metrics["scored_hottest_package"], "")
        self.assertEqual(result.metrics["scored_hottest_package_share"], 0.0)
        self.assertEqual(
            result.metrics["subscores"]["non_composition_integration_hotspot_share"],
            100.0,
        )
        self.assertEqual(result.score, 100.0)

    def test_frequently_changed_local_package_is_not_a_false_hotspot(self) -> None:
        repo = self._locality_repo(
            [ChangeCommit(packages=(("internal/domain/widget", 1),)) for _ in range(20)]
        )

        result = architecture._change_locality(repo)

        self.assertEqual(result.metrics["observed_hottest_package"], "internal/domain/widget")
        self.assertEqual(result.metrics["observed_hottest_package_share"], 1.0)
        self.assertEqual(result.metrics["scored_hottest_package"], "")
        self.assertEqual(result.metrics["scored_hottest_package_share"], 0.0)
        self.assertEqual(
            result.metrics["subscores"]["non_composition_integration_hotspot_share"],
            100.0,
        )
        self.assertEqual(result.score, 100.0)
        self.assertFalse(any(item.id.startswith("change-hotspot:") for item in result.findings))

    def test_non_composition_cross_component_hotspot_remains_penalized(self) -> None:
        repo = self._locality_repo(
            [
                ChangeCommit(
                    packages=(
                        ("internal/domain/widget", 1),
                        ("internal/infra/store", 1),
                    )
                )
                for _ in range(20)
            ]
        )

        result = architecture._change_locality(repo)

        self.assertEqual(result.metrics["scored_hottest_package_share"], 1.0)
        self.assertEqual(
            result.metrics["subscores"]["non_composition_integration_hotspot_share"],
            0.0,
        )
        self.assertEqual(result.score, 30.0)
        self.assertTrue(any(item.id.startswith("change-hotspot:") for item in result.findings))

    def test_package_nested_under_composition_root_cannot_gain_exemption(self) -> None:
        repo = self._locality_repo(
            [
                ChangeCommit(
                    packages=(
                        ("internal/domain/widget", 1),
                        ("internal/runtime/server/widget", 1),
                    )
                )
                for _ in range(20)
            ]
        )

        result = architecture._change_locality(repo)

        self.assertEqual(
            result.metrics["scored_hottest_package"],
            "internal/runtime/server/widget",
        )
        self.assertEqual(
            result.metrics["subscores"]["non_composition_integration_hotspot_share"],
            0.0,
        )
        self.assertEqual(result.score, 30.0)

    def test_composition_root_exemption_does_not_hide_scattered_changes(self) -> None:
        repo = self._locality_repo(
            [
                ChangeCommit(
                    packages=(
                        ("internal/domain/widget", 1),
                        ("internal/infra/store", 1),
                        ("internal/runtime/server", 1),
                    )
                )
                for _ in range(20)
            ]
        )

        result = architecture._change_locality(repo)

        self.assertEqual(result.metrics["observed_hottest_package"], "internal/runtime/server")
        self.assertLess(result.metrics["subscores"]["p90_packages_per_change"], 100.0)
        self.assertLess(result.metrics["subscores"]["p90_components_per_change"], 100.0)
        self.assertLess(result.score, 50.0)
        self.assertTrue(any(item.id.startswith("scattered-change:") for item in result.findings))

    @staticmethod
    def _locality_repo(commits: list[ChangeCommit]) -> inventory.RepositoryInventory:
        package_keys = sorted({package for commit in commits for package, _ in commit.packages})
        packages = {
            key: inventory.PackageFacts(
                key=key,
                path=f"gateway-go/{key}",
                package_name=key.rsplit("/", 1)[-1],
                source_loc=10,
                source_files=1,
                imports=(),
                exported_declarations=1,
                dynamic_exported_contracts=0,
                max_dependency_bag_fields=0,
                package_doc_chars=0,
                guide_path=None,
                guide_strength=0.0,
            )
            for key in package_keys
        }
        touches: Counter[str] = Counter()
        multipackage: Counter[str] = Counter()
        cochange: Counter[tuple[str, str]] = Counter()
        for commit in commits:
            changed = [package for package, _ in commit.packages]
            touches.update(changed)
            if len(changed) > 1:
                multipackage.update(changed)
            for index, left in enumerate(changed):
                for right in changed[index + 1 :]:
                    cochange[(left, right)] += 1
        history = HistoryFacts(
            available=True,
            detail=f"{len(commits)} fixture changes measured",
            commits=tuple(commits),
            package_touches=dict(touches),
            multipackage_touches=dict(multipackage),
            cochange_counts=dict(cochange),
        )
        graph = {key: frozenset() for key in package_keys}
        return inventory.RepositoryInventory(
            root=Path("/fixture"),
            module_path="example.invalid/fixture",
            packages=packages,
            graph=graph,
            reverse_graph=graph,
            component_graph={},
            component_sccs=(),
            history=history,
            source_files=len(packages),
            source_loc=10 * len(packages),
        )


if __name__ == "__main__":
    unittest.main()
