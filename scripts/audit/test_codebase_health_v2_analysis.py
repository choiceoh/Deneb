"""Static and adversarial regression tests for Health Bench 2.0."""

from __future__ import annotations

import unittest
from pathlib import Path
from unittest import mock

from test_codebase_health_v2_support import (
    GitFixture as _GitFixture,
    architecture_pillars as _architecture_pillars,
    pillar as _pillar,
)
from health_v2 import ai_readiness, history, inventory, operations, testing


class StaticAnalysisFixtureTests(unittest.TestCase):
    _SOURCE = """\
package widget

func WidgetValue(input int) int { return input }
"""
    _BASE_TEST = """\
package widget

import "testing"

func TestWidgetReturnsValue(t *testing.T) {
    got := WidgetValue(2)
    if got != 2 {
        t.Fatalf("got %d", got)
    }
}
"""
    _CLONE = """\

func TestWidgetReturnsAnotherValue(t *testing.T) {
    got := WidgetValue(3)
    if got != 3 {
        t.Fatalf("got %d", got)
    }
}
"""

    def test_python_centralized_import_maps_risk_to_production_package(self) -> None:
        source = """\
import subprocess

def execute(path):
    process = subprocess.run(["tool"], check=False)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(str(process.returncode))
    if process.returncode:
        raise ValueError("command failed")
"""
        unrelated = """\
def test_unrelated_returns_value():
    assert 1 == 1
"""
        direct_source = """\
import subprocess

def call_tool():
    process = subprocess.run(["tool"], check=False)
    if process.returncode:
        raise RuntimeError("tool failed")
    return process.stdout
"""
        direct_test = """\
import runpy
from unittest import mock

def test_mock_transport_failure_returns_error():
    with mock.patch("subprocess.run", side_effect=OSError("spawn failed")):
        try:
            runpy.run_path("scripts/mock_transport.py")["call_tool"]()
        except OSError as error:
            assert "spawn failed" in str(error)
"""
        linked = """\
import tempfile
from unittest import mock

from health_v2 import runner

def test_execute_rejects_invalid_process_error_without_tmp_write():
    with tempfile.TemporaryDirectory() as folder:
        with mock.patch.object(runner.subprocess, "run", side_effect=OSError("failed")):
            try:
                runner.execute(folder)
            except OSError:
                pass
        assert folder
"""
        with _GitFixture() as fixture:
            fixture.write("scripts/audit/health_v2/runner.py", source)
            fixture.write("scripts/mock_transport.py", direct_source)
            fixture.write("scripts/dev/test_unrelated.py", unrelated)
            fixture.track()
            before, _ = testing.evaluate(fixture.root)

            fixture.write("scripts/audit/test_codebase_health_v2_runner.py", linked)
            fixture.write("scripts/dev/test_mock_transport.py", direct_test)
            fixture.track()
            after, _ = testing.evaluate(fixture.root)

        before_risk = _pillar(before, "test-effectiveness").metrics["risk_obligations"]
        after_risk = _pillar(after, "test-effectiveness").metrics["risk_obligations"]
        self.assertEqual((before_risk["satisfied"], before_risk["total"]), (0, 5))
        self.assertEqual((after_risk["satisfied"], after_risk["total"]), (5, 5))

    def test_kotlin_import_requires_a_per_case_production_call(self) -> None:
        source = """\
package example.transport

import io.ktor.client.HttpClient

class RemoteGateway {
    fun fetch(): String = "ok"
}
"""
        unused_import = """\
package example.contracts

import example.transport.RemoteGateway
import kotlin.test.Test
import kotlin.test.assertFailsWith

class RemoteGatewayContractTest {
    @Test
    fun remoteGatewayRejectsHttpErrorWithMockEngine() {
        // RemoteGateway() in a comment must not create production linkage.
        assertFailsWith<IllegalStateException> { error("stub") }
    }
}
"""
        called_subject = """\
package example.contracts

import example.transport.RemoteGateway
import kotlin.test.Test
import kotlin.test.assertEquals

class RemoteGatewayContractTest {
    @Test
    fun remoteGatewayRejectsHttpErrorWithMockEngine() {
        val gateway = RemoteGateway()
        assertEquals("ok", gateway.fetch())
    }
}
"""
        with _GitFixture() as fixture:
            fixture.write(
                "client-android/app/composeApp/src/commonMain/kotlin/example/transport/RemoteGateway.kt",
                source,
            )
            fixture.write(
                "client-android/app/composeApp/src/commonTest/kotlin/example/contracts/RemoteGatewayContractTest.kt",
                unused_import,
            )
            fixture.track()
            before, _ = testing.evaluate(fixture.root)

            fixture.write(
                "client-android/app/composeApp/src/commonTest/kotlin/example/contracts/RemoteGatewayContractTest.kt",
                called_subject,
            )
            fixture.track()
            after, _ = testing.evaluate(fixture.root)

        before_effective = _pillar(before, "test-effectiveness").metrics
        after_effective = _pillar(after, "test-effectiveness").metrics
        self.assertEqual(
            before_effective["risk_obligations"]["by_language"]["kotlin"],
            {"satisfied": 0, "total": 1, "score": 0.0},
        )
        self.assertEqual(
            after_effective["risk_obligations"]["by_language"]["kotlin"],
            {"satisfied": 1, "total": 1, "score": 100.0},
        )
        self.assertEqual(before_effective["subject_locality"]["by_language"]["kotlin"], 0.0)
        self.assertEqual(after_effective["subject_locality"]["by_language"]["kotlin"], 100.0)

    def test_kotlin_extension_function_is_a_discoverable_subject(self) -> None:
        source = """\
package example.client

class RemoteClient

fun RemoteClient.refreshModels(): String = "ready"
"""
        test = """\
package example.client

import kotlin.test.Test
import kotlin.test.assertEquals

class AdminContractTest {
    @Test
    fun refreshModelsReturnsReadyState() {
        assertEquals("ready", RemoteClient().refreshModels())
    }
}
"""
        with _GitFixture() as fixture:
            fixture.write(
                "client-android/app/composeApp/src/commonMain/kotlin/example/client/RemoteClientExtensions.kt",
                source,
            )
            fixture.write(
                "client-android/app/composeApp/src/commonTest/kotlin/example/client/AdminContractTest.kt",
                test,
            )
            fixture.track()
            pillars, _ = testing.evaluate(fixture.root)

        locality = _pillar(pillars, "test-effectiveness").metrics["subject_locality"]
        self.assertEqual(locality["by_language"]["kotlin"], 100.0)

    def test_generated_suite_contributes_one_effective_shape_only(self) -> None:
        source = """\
package example.wire

import kotlinx.serialization.Serializable

@Serializable
data class WireRecord(val value: String = "")
"""
        generated_test = """\
// Code generated by scripts/gen_wire.py; DO NOT EDIT.
package example.wire

import kotlin.test.Test
import kotlin.test.assertFailsWith

class WireRecordGeneratedContractTest {
    @Test
    fun wireRecordInvalidDecodeRejectsWrongShape() {
        WireRecord()
        assertFailsWith<IllegalArgumentException> { error("invalid decode") }
    }

    @Test
    fun clonedRowInvalidDecodeRejectsWrongShape() {
        WireRecord()
        assertFailsWith<IllegalArgumentException> { error("invalid decode") }
    }
}
"""
        with _GitFixture() as fixture:
            fixture.write(
                "client-android/app/composeApp/src/commonMain/kotlin/example/wire/WireRecord.kt",
                source,
            )
            fixture.write("scripts/gen_wire.py", "# deterministic fixture generator\n")
            fixture.write(
                "Makefile",
                "wire-check:\n\tpython3 scripts/gen_wire.py --check\n",
            )
            fixture.track()
            before, _ = testing.evaluate(fixture.root)

            fixture.write(
                "client-android/app/composeApp/src/commonTest/kotlin/example/wire/WireRecordGeneratedContractTest.kt",
                generated_test,
            )
            fixture.track()
            after, _ = testing.evaluate(fixture.root)

        after_effective = _pillar(after, "test-effectiveness").metrics
        before_maintainable = _pillar(before, "test-maintainability").metrics
        after_maintainable = _pillar(after, "test-maintainability").metrics
        self.assertEqual(after_effective["test_cases_by_language"]["kotlin"], 1)
        self.assertEqual(after_effective["generated_contract_shapes"], 1)
        self.assertEqual(
            after_effective["risk_obligations"]["by_language"]["kotlin"],
            {"satisfied": 1, "total": 1, "score": 100.0},
        )
        self.assertEqual(after_effective["subject_locality"]["by_language"]["kotlin"], 100.0)
        self.assertEqual(
            before_maintainable["semantic_shape_uniqueness"],
            after_maintainable["semantic_shape_uniqueness"],
        )
        self.assertEqual(after_maintainable["generated_tests_excluded"], 1)

    def test_history_timeout_returns_unavailable_facts_instead_of_raising(self) -> None:
        timeout = history.subprocess.TimeoutExpired(["git", "log"], timeout=30)
        with mock.patch.object(history.subprocess, "run", side_effect=timeout):
            facts = history.collect_history(Path("/fixture"))

        self.assertFalse(facts.available)
        self.assertEqual(facts.commits, ())
        self.assertIn("git history unavailable", facts.detail)
        self.assertIn("timed out", facts.detail)

    def test_ai_tracked_files_git_error_returns_unavailable_inventory(self) -> None:
        failed = mock.Mock(returncode=128, stdout=b"")
        with mock.patch.object(
            ai_readiness.subprocess,
            "run",
            return_value=failed,
        ) as run:
            tracked = ai_readiness._tracked_files(Path("/fixture"))

        self.assertIsNone(tracked)
        run.assert_called_once_with(
            ["git", "-C", "/fixture", "ls-files", "-z"],
            capture_output=True,
            check=False,
            timeout=8,
        )

    def test_cloned_asserted_shapes_do_not_manufacture_confidence(self) -> None:
        with _GitFixture() as fixture:
            fixture.write("gateway-go/internal/widget/widget.go", self._SOURCE)
            fixture.write("gateway-go/internal/widget/widget_test.go", self._BASE_TEST)
            fixture.track()
            before, _ = testing.evaluate(fixture.root)

            fixture.write(
                "gateway-go/internal/widget/widget_test.go",
                self._BASE_TEST + self._CLONE,
            )
            after, _ = testing.evaluate(fixture.root)

        before_effective = _pillar(before, "test-effectiveness")
        after_effective = _pillar(after, "test-effectiveness")
        before_maintainable = _pillar(before, "test-maintainability")
        after_maintainable = _pillar(after, "test-maintainability")
        self.assertLessEqual(after_effective.score, before_effective.score)
        self.assertLess(after_maintainable.score, before_maintainable.score)
        self.assertLess(
            after_maintainable.metrics["semantic_shape_uniqueness"]["score"],
            before_maintainable.metrics["semantic_shape_uniqueness"]["score"],
        )

    def test_lexical_noise_cannot_launder_oracle_or_risk_evidence(self) -> None:
        go_source = """\
package widget

import "net/http"

// error and fake are documentation noise, not executable risk evidence.
const riskNoise = "error fake"

func FetchWidget() { _ = http.Client{} }
"""
        go_test = """\
package widget

import "testing"

func TestFetchWidgetReturns(t *testing.T) {
    _ = FetchWidget
    // t.Fatal("error") and a fake RoundTripper are not executable evidence.
    _ = "t.Fatal(\\\"error\\\") fake RoundTripper"
}
"""
        ts_source = """\
export async function loadWidget(): Promise<Response> {
  return fetch("/widget")
}
"""
        ts_test = """\
import { test } from "vitest"

test("loadWidget returns", () => {
  // expect(value).toBe(error) with a fake transport is still only a comment.
  const noise = "expect(value).toBe(error) fake"
  void noise
})
"""
        with _GitFixture() as fixture:
            fixture.write("gateway-go/internal/widget/widget.go", go_source)
            fixture.write("gateway-go/internal/widget/widget_test.go", go_test)
            fixture.write("andromeda/src/widget.ts", ts_source)
            fixture.write("andromeda/src/widget.test.ts", ts_test)
            fixture.track()

            pillars, _ = testing.evaluate(fixture.root)

        effective = _pillar(pillars, "test-effectiveness")
        oracle = effective.metrics["oracle_unique_shapes"]["by_language"]
        obligations = effective.metrics["risk_obligations"]["by_language"]
        self.assertEqual(oracle["go"], 0.0)
        self.assertEqual(oracle["typescript"], 0.0)
        self.assertEqual(obligations["go"], {"satisfied": 0, "total": 2, "score": 0.0})
        self.assertEqual(
            obligations["typescript"],
            {"satisfied": 0, "total": 2, "score": 0.0},
        )

    def test_active_language_with_zero_cases_scores_all_case_evidence_zero(self) -> None:
        helper_only = """\
package widget

import "testing"

func failHelper(t *testing.T) { t.Fatal("helper without a test case") }
"""
        with _GitFixture() as fixture:
            fixture.write("gateway-go/internal/widget/widget.go", self._SOURCE)
            fixture.write("gateway-go/internal/widget/widget_test.go", helper_only)
            fixture.track()

            pillars, _ = testing.evaluate(fixture.root)

        effective = _pillar(pillars, "test-effectiveness")
        maintainable = _pillar(pillars, "test-maintainability")
        self.assertEqual(effective.metrics["test_cases_by_language"]["go"], 0)
        self.assertEqual(
            effective.metrics["oracle_unique_shapes"]["by_language"]["go"], 0.0
        )
        self.assertEqual(
            effective.metrics["subject_locality"]["by_language"]["go"], 0.0
        )
        self.assertEqual(
            maintainable.metrics["intent_naming"]["by_language"]["go"], 0.0
        )
        self.assertEqual(
            maintainable.metrics["semantic_shape_uniqueness"]["by_language"]["go"],
            0.0,
        )

    def test_split_clones_and_empty_files_cannot_improve_maintainability(self) -> None:
        split_clone = self._BASE_TEST.replace(
            "TestWidgetReturnsValue", "TestWidgetReturnsAnotherValue"
        ).replace("WidgetValue(2)", "WidgetValue(3)").replace("got != 2", "got != 3")
        hazardous = self._BASE_TEST.replace(
            'import "testing"', 'import (\n    "testing"\n    "time"\n)'
        ).replace(
            "    got := WidgetValue(2)",
            "    time.Sleep(time.Millisecond)\n    got := WidgetValue(2)",
        )

        with _GitFixture() as fixture:
            fixture.write("gateway-go/internal/widget/widget.go", self._SOURCE)
            fixture.write("gateway-go/internal/widget/widget_test.go", self._BASE_TEST)
            fixture.track()
            base, _ = testing.evaluate(fixture.root)

            fixture.write("gateway-go/internal/widget/widget_more_test.go", split_clone)
            fixture.track()
            cloned, _ = testing.evaluate(fixture.root)

            fixture.write("gateway-go/internal/widget/widget_test.go", hazardous)
            fixture.track()
            hazardous_only, _ = testing.evaluate(fixture.root)

            fixture.write(
                "gateway-go/internal/widget/empty_test.go",
                "package widget\n\n// This file intentionally has no parsed test case.\n",
            )
            fixture.track()
            with_empty, _ = testing.evaluate(fixture.root)

        base_maintainable = _pillar(base, "test-maintainability")
        cloned_maintainable = _pillar(cloned, "test-maintainability")
        self.assertLess(
            cloned_maintainable.metrics["semantic_shape_uniqueness"]["score"],
            base_maintainable.metrics["semantic_shape_uniqueness"]["score"],
        )
        self.assertLessEqual(cloned_maintainable.score, base_maintainable.score)
        before_empty = _pillar(hazardous_only, "test-maintainability").metrics[
            "isolation"
        ]
        after_empty = _pillar(with_empty, "test-maintainability").metrics["isolation"]
        self.assertEqual(after_empty["score"], before_empty["score"])
        self.assertEqual(
            after_empty["unsafe_unique_shapes"],
            before_empty["unsafe_unique_shapes"],
        )

    def test_comment_padding_changes_only_unscored_size_diagnostic(self) -> None:
        padding = "// diagnostic-only padding\n\n" * 400
        with _GitFixture() as fixture:
            fixture.write("gateway-go/internal/widget/widget.go", self._SOURCE)
            fixture.write("gateway-go/internal/widget/widget_test.go", self._BASE_TEST)
            fixture.track()
            before, _ = testing.evaluate(fixture.root)

            fixture.write(
                "gateway-go/internal/widget/widget_test.go",
                self._BASE_TEST + padding,
            )
            fixture.track()
            after, _ = testing.evaluate(fixture.root)

        before_maintainable = _pillar(before, "test-maintainability")
        after_maintainable = _pillar(after, "test-maintainability")
        before_size = before_maintainable.metrics["test_file_size_diagnostic"]
        after_size = after_maintainable.metrics["test_file_size_diagnostic"]

        self.assertEqual(after_maintainable.score, before_maintainable.score)
        self.assertEqual(
            after_maintainable.metrics["semantic_shape_uniqueness"],
            before_maintainable.metrics["semantic_shape_uniqueness"],
        )
        self.assertEqual(
            after_maintainable.metrics["intent_naming"],
            before_maintainable.metrics["intent_naming"],
        )
        self.assertEqual(
            after_maintainable.metrics["isolation"],
            before_maintainable.metrics["isolation"],
        )
        self.assertIs(before_size["scored"], False)
        self.assertIs(after_size["scored"], False)
        self.assertEqual(before_size["over_700"], 0)
        self.assertEqual(after_size["over_700"], 1)
        self.assertGreater(after_size["max_lines"], before_size["max_lines"])

    def test_placeholder_guides_are_rejected_and_ancestor_help_is_attenuated(self) -> None:
        substantive = """\
# Internal architecture

This guide explains the purpose and responsibility of the internal runtime tree. The entry point owns request orchestration, while leaf packages own domain behavior and must not reverse the dependency direction. A maintainer changing a contract should begin at the owning package and inspect callers before editing shared types.

## Boundaries, invariants, and verification

The boundary invariant is that adapters may depend on domain contracts, but domain code never depends on transport details. Tests verify observable results, failure propagation, cancellation, and persistence recovery. Keep ownership local, document any new dependency, and run the focused package test plus the repository architecture check after each structural change.
"""
        with _GitFixture() as fixture:
            fixture.write("gateway-go/go.mod", "module example.invalid/fixture\n")
            fixture.write(
                "gateway-go/internal/near/near.go",
                "package near\n\nfunc Value() int { return 1 }\n",
            )
            fixture.write(
                "gateway-go/internal/far/deep/thing/thing.go",
                "package thing\n\nfunc Value() int { return 1 }\n",
            )
            fixture.write(
                "gateway-go/internal/CLAUDE.md",
                "# Architecture\n\nTODO: add entry point, responsibility, and tests.\n",
            )
            fixture.track()
            placeholder = inventory.collect(fixture.root)

            fixture.write("gateway-go/internal/CLAUDE.md", substantive)
            fixture.track()
            documented = inventory.collect(fixture.root)

        self.assertIsNone(placeholder.packages["internal/near"].guide_path)
        self.assertIsNone(
            placeholder.packages["internal/far/deep/thing"].guide_path
        )
        near = documented.packages["internal/near"]
        far = documented.packages["internal/far/deep/thing"]
        self.assertEqual(near.guide_path, "gateway-go/internal/CLAUDE.md")
        self.assertEqual(far.guide_path, "gateway-go/internal/CLAUDE.md")
        self.assertGreater(near.guide_strength, far.guide_strength)
        self.assertGreater(far.guide_strength, 0.0)
        self.assertLess(near.guide_strength, 1.0)

    def test_trivial_source_cannot_dilute_hotspots_and_receiver_ids_are_unique(self) -> None:
        def method(receiver: str, *, runtime_risks: bool = False) -> str:
            lines = [f"func (value *{receiver}) Run(input int) int {{"]
            if runtime_risks:
                lines.extend(
                    [
                        '    _ = os.Remove("/tmp/worker")',
                        "    _ = context.Background()",
                        "    _ = http.Client{}",
                    ]
                )
            lines.append("    result := input")
            indent = "    "
            for threshold in range(12):
                lines.append(f"{indent}if result > {threshold} {{")
                indent += "    "
            lines.append(f"{indent}result++")
            for _ in range(12):
                indent = indent[:-4]
                lines.append(f"{indent}}}")
            lines.extend(["    return result", "}"])
            return "\n".join(lines)

        risky = (
            "package worker\n\n"
            "import (\n"
            '    "context"\n'
            '    "net/http"\n'
            '    "os"\n'
            ")\n\n"
            "type Alpha struct{}\n"
            "type Beta struct{}\n\n"
            + method("Alpha", runtime_risks=True)
            + "\n\n"
            + method("Beta")
            + "\n"
        )
        trivial = "package worker\n\n" + "\n".join(
            f"func Trivial{index}(value int) int {{ return value + {index} }}"
            for index in range(80)
        )
        with _GitFixture() as fixture:
            fixture.write("gateway-go/internal/worker/worker.go", risky)
            fixture.track()
            before, _ = operations.evaluate(fixture.root)

            fixture.write("gateway-go/internal/worker/trivial.go", trivial)
            fixture.track()
            after, _ = operations.evaluate(fixture.root)

        before_complexity = _pillar(before, "complexity-hotspots")
        after_complexity = _pillar(after, "complexity-hotspots")
        self.assertLessEqual(after_complexity.score, before_complexity.score)
        self.assertLessEqual(
            _pillar(after, "runtime-safety").score,
            _pillar(before, "runtime-safety").score,
        )
        run_findings = [
            item for item in before_complexity.findings if item.evidence.startswith("*Alpha.Run") or item.evidence.startswith("*Beta.Run")
        ]
        self.assertEqual(len(run_findings), 2)
        self.assertEqual(len({item.id for item in run_findings}), 2)

    def test_panic_exemption_requires_documentation_recovery_and_relevant_call(self) -> None:
        undocumented = """\
package widget

func MustValue(value int) int {
    if value < 0 {
        panic("negative value")
    }
    return value
}
"""
        documented = undocumented.replace(
            "func MustValue",
            "// MustValue panics if its input violates the non-negative invariant.\nfunc MustValue",
        )
        relevant_test = """\
package widget

import "testing"

func TestMustValuePanicsAndIsRecoverable(t *testing.T) {
    defer func() {
        if recover() == nil {
            t.Fatal("expected panic")
        }
    }()
    MustValue(-1)
}
"""
        irrelevant_test = relevant_test.replace(
            "TestMustValuePanicsAndIsRecoverable", "TestOtherPathPanicsAndIsRecoverable"
        ).replace("    MustValue(-1)", '    panic("fixture only")')

        with _GitFixture() as fixture:
            fixture.write("gateway-go/internal/widget/widget.go", undocumented)
            fixture.write("gateway-go/internal/widget/widget_test.go", relevant_test)
            fixture.track()
            missing_doc, _ = operations.evaluate(fixture.root)

            fixture.write("gateway-go/internal/widget/widget.go", documented)
            fixture.write("gateway-go/internal/widget/widget_test.go", irrelevant_test)
            fixture.track()
            irrelevant, _ = operations.evaluate(fixture.root)

            fixture.write("gateway-go/internal/widget/widget_test.go", relevant_test)
            fixture.track()
            proven, _ = operations.evaluate(fixture.root)

        self.assertEqual(_pillar(missing_doc, "runtime-safety").metrics["fatal_paths"], 1)
        self.assertEqual(_pillar(irrelevant, "runtime-safety").metrics["fatal_paths"], 1)
        self.assertEqual(_pillar(proven, "runtime-safety").metrics["fatal_paths"], 0)

    def test_fake_do_not_edit_header_is_not_a_generated_test_exemption(self) -> None:
        fake = (
            "// Code generated by scripts/missing-generator.py; DO NOT EDIT.\n"
            + self._BASE_TEST
        )
        with _GitFixture() as fixture:
            fixture.write("gateway-go/internal/widget/widget.go", self._SOURCE)
            fixture.write("gateway-go/internal/widget/widget_test.go", fake)
            fixture.track()

            pillars, _ = testing.evaluate(fixture.root)

        effective = _pillar(pillars, "test-effectiveness")
        maintainable = _pillar(pillars, "test-maintainability")
        self.assertEqual(effective.metrics["test_cases"], 1)
        self.assertEqual(maintainable.metrics["generated_tests_excluded"], 0)
        self.assertEqual(maintainable.metrics["unproven_generated_markers"], 1)

    def test_untracked_scratch_files_do_not_change_inventory_or_scores(self) -> None:
        with _GitFixture() as fixture:
            fixture.write("gateway-go/go.mod", "module example.invalid/fixture\n")
            fixture.write("gateway-go/internal/widget/widget.go", self._SOURCE)
            fixture.write("gateway-go/internal/widget/widget_test.go", self._BASE_TEST)
            fixture.commit("seed tracked production and test files")

            before_inventory = inventory.collect(fixture.root)
            before_architecture = _architecture_pillars(before_inventory)
            before_testing, _ = testing.evaluate(fixture.root)

            fixture.write(
                "gateway-go/internal/scratch/scratch.go",
                "package scratch\n\nfunc Untracked() { panic(\"scratch\") }\n",
            )
            fixture.write(
                "gateway-go/internal/widget/scratch_test.go",
                self._BASE_TEST
                + self._CLONE
                + "\nfunc TestScratchSleeps(t *testing.T) { time.Sleep(time.Second) }\n",
            )

            after_inventory = inventory.collect(fixture.root)
            after_architecture = _architecture_pillars(after_inventory)
            after_testing, _ = testing.evaluate(fixture.root)

        self.assertEqual(
            (
                before_inventory.source_files,
                before_inventory.source_loc,
                before_inventory.packages,
                before_inventory.graph,
            ),
            (
                after_inventory.source_files,
                after_inventory.source_loc,
                after_inventory.packages,
                after_inventory.graph,
            ),
        )
        self.assertEqual(
            [(item.id, item.score, item.metrics) for item in before_architecture],
            [(item.id, item.score, item.metrics) for item in after_architecture],
        )
        self.assertEqual(
            [(item.id, item.score, item.metrics) for item in before_testing],
            [(item.id, item.score, item.metrics) for item in after_testing],
        )

    def test_merge_head_history_includes_pr_diff_like_a_squash_commit(self) -> None:
        with _GitFixture() as fixture:
            fixture.write(
                "gateway-go/internal/alpha/base.go",
                "package alpha\n\nfunc Base() {}\n",
            )
            base = fixture.commit("seed production history")

            fixture.git("checkout", "-q", "-b", "feature")
            fixture.write(
                "gateway-go/internal/alpha/feature.go",
                "package alpha\n\nfunc Feature() {}\n",
            )
            fixture.write(
                "gateway-go/internal/beta/feature.go",
                "package beta\n\nfunc Feature() {}\n",
            )
            feature = fixture.commit("change two production packages")

            fixture.git("checkout", "-q", fixture.initial_branch)
            fixture.write("README.md", "base branch moved\n")
            fixture.commit("advance base without production Go changes")
            fixture.git("merge", "-q", "--no-ff", "-m", "synthetic PR merge", "feature")
            with mock.patch.object(inventory, "MIN_HISTORY_COMMITS", 1):
                merge_history = inventory._collect_history(fixture.root)

            fixture.git("checkout", "-q", "-b", "squash-equivalent", base)
            fixture.git("cherry-pick", feature)
            with mock.patch.object(inventory, "MIN_HISTORY_COMMITS", 1):
                squash_history = inventory._collect_history(fixture.root)

        expected = inventory.ChangeCommit(
            packages=(("internal/alpha", 1), ("internal/beta", 1))
        )
        self.assertTrue(merge_history.available, merge_history.detail)
        self.assertTrue(squash_history.available, squash_history.detail)
        self.assertIn("HEAD merge diff included", merge_history.detail)
        self.assertEqual(merge_history.commits[0], expected)
        self.assertEqual(merge_history.commits[0], squash_history.commits[0])
        self.assertGreaterEqual(merge_history.package_touches["internal/alpha"], 2)
        self.assertEqual(merge_history.package_touches["internal/beta"], 1)

    def test_high_complexity_and_runtime_risk_score_below_simple_code(self) -> None:
        simple = """\
package worker

func Run(value int) int { return value + 1 }
"""
        nested = "value := input\n" + "".join(
            f"{'    ' * depth}if value > {depth} {{\n" for depth in range(1, 15)
        )
        nested += f"{'    ' * 15}panic(value)\n"
        nested += "".join(f"{'    ' * depth}}}\n" for depth in range(14, 0, -1))
        risky = (
            "package worker\n\n"
            "import (\n"
            '    "context"\n'
            '    "net/http"\n'
            '    "os"\n'
            ")\n\n"
            "func Run(input int) int {\n"
            "    _ = os.Remove(\"/tmp/work\")\n"
            "    _ = context.Background()\n"
            "    _ = http.Client{}\n"
            + "    "
            + nested.replace("\n", "\n    ").rstrip()
            + "\n    return value\n}\n"
        )

        with _GitFixture() as fixture:
            fixture.write("gateway-go/internal/worker/worker.go", simple)
            fixture.track()
            safe_pillars, _ = operations.evaluate(fixture.root)

            fixture.write("gateway-go/internal/worker/worker.go", risky)
            risky_pillars, _ = operations.evaluate(fixture.root)

        self.assertLess(
            _pillar(risky_pillars, "complexity-hotspots").score,
            _pillar(safe_pillars, "complexity-hotspots").score,
        )
        self.assertLess(
            _pillar(risky_pillars, "runtime-safety").score,
            _pillar(safe_pillars, "runtime-safety").score,
        )


if __name__ == "__main__":
    unittest.main()
