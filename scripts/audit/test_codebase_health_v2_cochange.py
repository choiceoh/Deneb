"""Cross-component seam classification for responsibility co-change.

Dependency-injection roots and exact protocol facades touch many components in
one change by design; flagging them as diffuse responsibility is a false
positive. The classification must be scoped: genuine leaf packages with high
cross-component co-change still get flagged, and every seam's rate still feeds
the subscore so it cannot hide churn.
"""

from __future__ import annotations

import contextlib
import unittest
from pathlib import Path

from health_v2 import architecture, inventory
from health_v2.architecture_contracts import (
    COMPOSITION_ROOT_COMPONENTS,
    CROSS_COMPONENT_TRANSPORT_SEAMS,
    VOLATILE_DOMAIN_ASSEMBLY_HUBS,
    is_composition_root,
    is_cross_component_seam,
    is_volatile_domain_assembly_hub,
)
from health_v2.history import ChangeCommit, HistoryFacts
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


class CrossComponentSeamTest(unittest.TestCase):
    def test_when_is_composition_root_recognizes_wiring_layer_only(self) -> None:
        self.assertTrue(is_composition_root("internal/runtime/server"))
        self.assertTrue(is_composition_root("internal/runtime/bootstrap"))
        self.assertFalse(is_composition_root("internal/domain/skills/genesis"))
        self.assertFalse(is_composition_root("internal/runtime/rpc"))
        self.assertIn("runtime/server", COMPOSITION_ROOT_COMPONENTS)

    def test_when_transport_seam_is_exact_and_does_not_exempt_nested_features(self) -> None:
        seam = "internal/runtime/rpc/handler/handlerminiapp"
        self.assertIn(seam, CROSS_COMPONENT_TRANSPORT_SEAMS)
        self.assertTrue(is_cross_component_seam(seam))
        self.assertFalse(is_cross_component_seam(seam + "/dashboard"))
        self.assertFalse(is_cross_component_seam("internal/runtime/rpc/handler/mail"))

    def test_cochange_exemption_is_scoped_and_score_preserving(self) -> None:
        # Wiring root AND a domain leaf both cross a component boundary on every
        # commit (rate -> 1.0). One repo proves both properties.
        repo = _collect_cochange_fixture(
            {
                "server": "gateway-go/internal/runtime/server/wire.go",
                "transport": (
                    "gateway-go/internal/runtime/rpc/handler/handlerminiapp/workfeed.go"
                ),
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
        self.assertNotIn(
            "handlerminiapp",
            flagged,
            "exact transport seam must be exempt from the cochange finding",
        )

        # Score-preserving: the root's high rate still fed the tail subscore
        # (0-100, lower = worse), so the wiring layer cannot hide churn from the
        # pillar score.
        subscores = pillar.metrics.get("subscores", {})
        self.assertLess(subscores.get("cross_component_cochange_tail", 100.0), 100.0)


def _volatile_fixture_repo() -> inventory.RepositoryInventory:
    """Build a repo where genesis and a leaf hub both exceed volatile thresholds."""
    hub = "internal/domain/skills/genesis"
    leaf = "internal/domain/leaf/hub"
    consumers = tuple(f"internal/runtime/consumer{i}" for i in range(10))
    package_keys = (hub, leaf, *consumers)
    packages = {
        key: inventory.PackageFacts(
            key=key,
            path=f"gateway-go/{key}",
            package_name=key.rsplit("/", 1)[-1],
            source_loc=10,
            source_files=1,
            imports=(),
            exported_declarations=170 if key in {hub, leaf} else 1,
            dynamic_exported_contracts=0,
            max_dependency_bag_fields=0,
            package_doc_chars=0,
            guide_path=None,
            guide_strength=0.0,
        )
        for key in package_keys
    }
    graph = {key: frozenset() for key in package_keys}
    reverse = {key: frozenset() for key in package_keys}
    for consumer in consumers:
        reverse[hub] = frozenset(set(reverse[hub]) | {consumer})
        reverse[leaf] = frozenset(set(reverse[leaf]) | {consumer})
    commits = tuple(
        ChangeCommit(packages=((hub, 1), (leaf, 1))) for _ in range(22)
    )
    history = HistoryFacts(
        available=True,
        detail=f"{len(commits)} fixture changes measured",
        commits=commits,
        package_touches={hub: 22, leaf: 22},
        multipackage_touches={hub: 22, leaf: 22},
        cochange_counts={(hub, leaf): 22},
    )
    return inventory.RepositoryInventory(
        root=Path("/fixture"),
        module_path="example.invalid/fixture",
        packages=packages,
        graph=graph,
        reverse_graph=reverse,
        component_graph={},
        component_sccs=(),
        history=history,
        source_files=len(packages),
        source_loc=10 * len(packages),
    )


class VolatileAssemblyHubTest(unittest.TestCase):
    def test_when_genesis_is_assembly_hub_not_composition_root(self) -> None:
        hub = "internal/domain/skills/genesis"
        self.assertIn(hub, VOLATILE_DOMAIN_ASSEMBLY_HUBS)
        self.assertTrue(is_volatile_domain_assembly_hub(hub))
        self.assertFalse(is_composition_root(hub))

    def test_volatile_finding_exemption_is_scoped_and_score_preserving(self) -> None:
        repo = _volatile_fixture_repo()
        cohesion = architecture._responsibility_cohesion(repo)
        locality = architecture._change_locality(repo)

        contract = [
            f for f in cohesion.findings if f.id.startswith("volatile-contract-responsibility:")
        ]
        hubs = [f for f in locality.findings if f.id.startswith("volatile-hub:")]
        flagged = " ".join(f.path for f in contract + hubs)

        self.assertNotIn("skills/genesis", flagged, "genesis hub must be exempt")
        self.assertIn("domain/leaf/hub", flagged, "non-hub volatile package must still fire")
        self.assertLess(
            cohesion.metrics.get("subscores", {}).get("volatile_contract_blast_tail", 100.0),
            100.0,
        )
        self.assertLess(
            locality.metrics.get("subscores", {}).get("volatile_dependency_hubs", 100.0),
            100.0,
        )


if __name__ == "__main__":
    unittest.main()
