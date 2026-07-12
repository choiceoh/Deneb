"""Composition-root exemption for the responsibility-cochange finding.

A dependency-injection / wiring root (runtime/server, runtime/bootstrap) touches
many components in one change by design; flagging it as diffuse responsibility is
a false positive. The exemption must be scoped: genuine leaf packages with high
cross-component co-change still get flagged.
"""

from __future__ import annotations

import unittest

from health_v2 import architecture, inventory
from health_v2.architecture_contracts import (
    COMPOSITION_ROOT_COMPONENTS,
    is_composition_root,
)
from test_codebase_health_v2_support import GitFixture

_GO = "package {pkg}\n\n// Package {pkg} does one thing.\nfunc F{n}() int {{ return {n} }}\n"


class CompositionRootExemptionTest(unittest.TestCase):
    def test_is_composition_root_recognizes_wiring_layer_only(self) -> None:
        self.assertTrue(is_composition_root("internal/runtime/server"))
        self.assertTrue(is_composition_root("internal/runtime/bootstrap"))
        self.assertFalse(is_composition_root("internal/domain/skills/genesis"))
        self.assertFalse(is_composition_root("internal/runtime/rpc"))
        self.assertIn("runtime/server", COMPOSITION_ROOT_COMPONENTS)

    def test_cochange_finding_exempts_root_but_flags_leaf(self) -> None:
        # Build a repo where BOTH the wiring root and a domain leaf co-change
        # across a component boundary on every commit (rate → 1.0). The finding
        # must fire for the leaf and NOT for the composition root.
        with GitFixture() as fixture:
            fixture.write("gateway-go/go.mod", "module example.invalid/fixture\n")
            files = {
                "server": "gateway-go/internal/runtime/server/wire.go",
                "leaf": "gateway-go/internal/domain/leaf/impl.go",
                "other": "gateway-go/internal/domain/other/impl.go",
            }
            pkgs = {"server": "server", "leaf": "leaf", "other": "other"}
            # >= 10 commits so each package clears the local_touches gate; every
            # commit touches all three files → every touch crosses a boundary.
            for n in range(22):  # >= MIN_HISTORY_COMMITS (20)
                for key, path in files.items():
                    fixture.write(path, _GO.format(pkg=pkgs[key], n=n))
                fixture.commit(f"c{n}")
            repo = inventory.collect(fixture.root)

        pillar = architecture._responsibility_cohesion(repo)
        # rule is encoded in the finding id prefix (stable_id).
        cochange = [
            f for f in pillar.findings if f.id.startswith("responsibility-cochange:")
        ]
        self.assertTrue(cochange, "fixture did not produce any cochange finding")
        flagged_paths = " ".join(f.path for f in cochange)
        self.assertIn("domain/leaf", flagged_paths, "leaf cochange should still fire")
        self.assertNotIn(
            "runtime/server",
            flagged_paths,
            "composition root must be exempt from the cochange finding",
        )

    def test_score_still_reflects_root_cochange(self) -> None:
        # Exemption is finding-only: the root's rate still feeds the subscore, so
        # the pillar cannot be gamed by moving churn into the wiring layer.
        with GitFixture() as fixture:
            fixture.write("gateway-go/go.mod", "module example.invalid/fixture\n")
            for n in range(22):  # >= MIN_HISTORY_COMMITS (20)
                fixture.write(
                    "gateway-go/internal/runtime/server/wire.go", _GO.format(pkg="server", n=n)
                )
                fixture.write(
                    "gateway-go/internal/domain/leaf/impl.go", _GO.format(pkg="leaf", n=n)
                )
                fixture.commit(f"c{n}")
            repo = inventory.collect(fixture.root)

        self.assertTrue(repo.history.available, "fixture history not collected")
        pillar = architecture._responsibility_cohesion(repo)
        subscores = pillar.metrics.get("subscores", {})
        # Finding-only exemption: the root's high cross-component rate still fed
        # the tail subscore (0-100, lower = worse), so it is NOT a perfect 100 —
        # the wiring layer cannot be used to hide churn from the pillar score.
        self.assertLess(subscores.get("cross_component_cochange_tail", 100.0), 100.0)


if __name__ == "__main__":
    unittest.main()
