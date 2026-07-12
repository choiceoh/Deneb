"""Composition-root exemption for the responsibility-cochange finding.

A dependency-injection / wiring root (runtime/server, runtime/bootstrap) touches
many components in one change by design; flagging it as diffuse responsibility is
a false positive. The exemption must be scoped: genuine leaf packages with high
cross-component co-change still get flagged, and the root's rate still feeds the
subscore so it cannot be used to hide churn.
"""

from __future__ import annotations

import contextlib
import unittest

from health_v2 import architecture, inventory
from health_v2.architecture_contracts import (
    COMPOSITION_ROOT_COMPONENTS,
    is_composition_root,
)
from test_codebase_health_v2_support import GitFixture

_GO = "package {pkg}\n\n// Package {pkg} does one thing.\nfunc F{n}() int {{ return {n} }}\n"


def _collect_cochange_fixture(files: dict[str, str]) -> inventory.RepositoryInventory:
    """Build a repo where every file co-changes across >= MIN_HISTORY_COMMITS
    commits, then collect it. Cleanup is best-effort: the TemporaryDirectory can
    lose a race with git's own file handles on some filesystems (Errno 39 on
    .git), which is a teardown artifact, not a measurement error.
    """
    fixture = GitFixture()
    try:
        fixture.write("gateway-go/go.mod", "module example.invalid/fixture\n")
        for n in range(22):  # >= MIN_HISTORY_COMMITS (20)
            for pkg, path in files.items():
                fixture.write(path, _GO.format(pkg=pkg.split("/")[-1], n=n))
            fixture.commit(f"c{n}")
        return inventory.collect(fixture.root)
    finally:
        with contextlib.suppress(OSError):
            fixture.close()


class CompositionRootExemptionTest(unittest.TestCase):
    def test_is_composition_root_recognizes_wiring_layer_only(self) -> None:
        self.assertTrue(is_composition_root("internal/runtime/server"))
        self.assertTrue(is_composition_root("internal/runtime/bootstrap"))
        self.assertFalse(is_composition_root("internal/domain/skills/genesis"))
        self.assertFalse(is_composition_root("internal/runtime/rpc"))
        self.assertIn("runtime/server", COMPOSITION_ROOT_COMPONENTS)

    def test_cochange_exemption_is_scoped_and_score_preserving(self) -> None:
        # Wiring root AND a domain leaf both cross a component boundary on every
        # commit (rate -> 1.0). One repo proves both properties.
        repo = _collect_cochange_fixture(
            {
                "server": "gateway-go/internal/runtime/server/wire.go",
                "leaf": "gateway-go/internal/domain/leaf/impl.go",
                "other": "gateway-go/internal/domain/other/impl.go",
            }
        )
        self.assertTrue(repo.history.available, "fixture history not collected")

        pillar = architecture._responsibility_cohesion(repo)
        # rule is encoded in the finding id prefix (stable_id).
        cochange = [
            f for f in pillar.findings if f.id.startswith("responsibility-cochange:")
        ]
        self.assertTrue(cochange, "fixture did not produce any cochange finding")
        flagged = " ".join(f.path for f in cochange)

        # Scoped: the leaf still fires, the composition root is exempt.
        self.assertIn("domain/leaf", flagged, "leaf cochange should still fire")
        self.assertNotIn(
            "runtime/server",
            flagged,
            "composition root must be exempt from the cochange finding",
        )

        # Score-preserving: the root's high rate still fed the tail subscore
        # (0-100, lower = worse), so the wiring layer cannot hide churn from the
        # pillar score.
        subscores = pillar.metrics.get("subscores", {})
        self.assertLess(subscores.get("cross_component_cochange_tail", 100.0), 100.0)


if __name__ == "__main__":
    unittest.main()
