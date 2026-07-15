"""Architecture and maintainability pillars for Health Bench 2.0."""

from __future__ import annotations

import math
import statistics
from collections import Counter
from pathlib import Path
from typing import Iterable

from .ai_readiness import (
    LANE_COVERAGE,
    collect as collect_ai_readiness,
    evaluate_change_readiness as _ai_change_readiness,
    evaluate_navigability as _ai_navigability,
)
from .architecture_contracts import (
    _bounded_average,
    _contract_explicitness,
    _finding,
    _package_path,
    _responsibility_cohesion,
    _rounded_map,
    is_composition_root,
)
from .inventory import RepositoryInventory, collect, component_for
from .model import (
    Evidence,
    Finding,
    Pillar,
    ascending_grade,
    descending_grade,
    tail_score,
)

# Paths express architectural role. A shared kernel placed under runtime is an
# ambiguous boundary and should move rather than receive a scorer exception.
LAYER_RANK = {
    "testutil": 0,
    "codegen": 1,
    "core": 1,
    "hanja": 1,
    "infra": 1,
    # Agent loop lives under ai/agent (formerly top-level agentsys/).
    "ai": 2,
    "domain": 2,
    "platform": 2,
    "pipeline": 3,
    "runtime": 5,
}
DEFAULT_RANK = 2
COMPOSITION_ROOTS = {
    "internal/pipeline/chat",
    "internal/runtime/bootstrap",
    "internal/runtime/server",
    "internal/runtime/serverauto",
    "internal/runtime/serverchat",
    "internal/runtime/servermail",
    "internal/runtime/serverport",
    "internal/runtime/serverwire",
    "internal/runtime/serverwire/early",
    "internal/runtime/serverwire/late",
    "internal/runtime/serverwire/porttypes",
}


def evaluate(root: str | Path) -> tuple[list[Pillar], list[Evidence]]:
    """Evaluate the six architecture pillars for ``root``."""

    repo = collect(root)
    ai_facts = collect_ai_readiness(repo)
    pillars = [
        _boundary_integrity(repo),
        _change_locality(repo),
        _responsibility_cohesion(repo),
        _contract_explicitness(repo),
        _ai_navigability(repo, ai_facts),
        _ai_change_readiness(repo, ai_facts),
    ]
    static_status = "measured" if repo.packages else "unavailable"
    static_detail = (
        f"{len(repo.packages)} packages, {sum(len(v) for v in repo.graph.values())} "
        f"internal edges, {repo.source_files} files/{repo.source_loc} LOC"
        if repo.packages
        else "gateway-go/internal production sources were not found"
    )
    history_status = "measured" if repo.history.available else "unavailable"
    evidence = [
        Evidence(
            name="go-architecture-inventory",
            status=static_status,
            detail=static_detail,
            required=True,
        ),
        Evidence(
            name="git-change-history",
            status=history_status,
            detail=repo.history.detail,
            required=True,
        ),
        Evidence(
            name="go-contract-surface",
            status=static_status,
            detail=(
                "exported signatures, dependency bags, and reverse dependencies measured"
                if repo.packages
                else "contract surface could not be measured"
            ),
            required=True,
        ),
        Evidence(
            name="architecture-navigation",
            status="measured" if ai_facts.available else "unavailable",
            detail=(
                ai_facts.detail
                + f"; measured_product_lanes=['go']; product-impact coverage={LANE_COVERAGE:.3f}"
                if ai_facts.available
                else "navigation evidence could not be cross-checked"
            ),
            required=True,
        ),
        Evidence(
            name="ai-maintainability-kotlin",
            status="unavailable",
            detail="Rubric 2.2 AI cross-check currently measures Go only; Kotlin lowers report confidence but is not scored as zero.",
            required=False,
        ),
        Evidence(
            name="ai-maintainability-typescript",
            status="unavailable",
            detail="Rubric 2.2 AI cross-check currently measures Go only; TypeScript lowers report confidence but is not scored as zero.",
            required=False,
        ),
        Evidence(
            name="ai-maintainability-python",
            status="unavailable",
            detail="Rubric 2.2 AI cross-check currently measures Go only; Python lowers report confidence but is not scored as zero.",
            required=False,
        ),
    ]
    return pillars, evidence


def _boundary_integrity(repo: RepositoryInventory) -> Pillar:
    pillar_id = "boundary-integrity"
    if not repo.packages:
        return Pillar(
            id=pillar_id,
            title="Boundary integrity",
            weight=12,
            score=0,
            intent="Keep dependency direction, component ownership, and change blast radius explicit.",
            metrics={"measured": False},
        )

    forbidden: list[tuple[str, str]] = []
    for source, targets in repo.graph.items():
        source_top = _top_level(source)
        if source_top == "testutil":
            continue
        source_rank = LAYER_RANK.get(source_top, DEFAULT_RANK)
        for target in targets:
            target_top = _top_level(target)
            if target_top == source_top:
                continue
            if LAYER_RANK.get(target_top, DEFAULT_RANK) > source_rank:
                forbidden.append((source, target))

    cyclic_nodes = sum(len(component) for component in repo.component_sccs)
    direct_credits: list[float] = []
    direct_rows: list[tuple[int, str, int, int]] = []
    two_hop_credits: list[float] = []
    two_hop_rows: list[tuple[int, str, int, int]] = []
    for package in repo.packages:
        fanout = len(repo.graph[package])
        if package in COMPOSITION_ROOTS:
            direct_soft, direct_hard = 20, 50
            two_soft, two_hard = 80, 150
        else:
            direct_soft, direct_hard = 8, 20
            two_soft, two_hard = 25, 60
        direct_credits.append(descending_grade(fanout, direct_soft, direct_hard))
        if fanout > direct_soft:
            direct_rows.append((fanout, package, direct_soft, direct_hard))
        two_hop = _two_hop_reach(repo, package)
        two_hop_credits.append(descending_grade(two_hop, two_soft, two_hard))
        if two_hop > two_soft:
            two_hop_rows.append((two_hop, package, two_soft, two_hard))

    subscores = {
        "forbidden_edges": descending_grade(len(forbidden), 0, 10),
        "component_cycles": descending_grade(cyclic_nodes, 0, 8),
        "direct_fanout_tail": _worst_credit_mean(direct_credits),
        "two_hop_reach_tail": _worst_credit_mean(two_hop_credits),
    }
    score = _bounded_average(
        [
            (subscores["forbidden_edges"], 0.25),
            (subscores["component_cycles"], 0.25),
            (subscores["direct_fanout_tail"], 0.30),
            (subscores["two_hop_reach_tail"], 0.20),
        ]
    )

    findings: list[Finding] = []
    grouped_forbidden: dict[str, list[str]] = {}
    for source, target in forbidden:
        grouped_forbidden.setdefault(source, []).append(target)
    for source, targets in sorted(
        grouped_forbidden.items(), key=lambda item: (-len(item[1]), item[0])
    ):
        findings.append(
            _finding(
                pillar=pillar_id,
                rule="upward-import",
                path=_package_path(repo, source),
                severity="high" if len(targets) >= 3 else "medium",
                evidence=(
                    f"{source} imports {len(targets)} higher-layer package(s): "
                    + ", ".join(sorted(targets)[:6])
                ),
                why=(
                    "An upward dependency makes lower-level behavior depend on orchestration "
                    "placement and obscures the real ownership boundary."
                ),
                remediation=(
                    "Move the shared contract to core/domain or invert the dependency through a "
                    "narrow port; do not add a rank exception."
                ),
                priority=90 + min(9, len(targets)),
                related=tuple(_package_path(repo, target) for target in sorted(targets)),
            )
        )

    for component in repo.component_sccs:
        related = tuple(f"gateway-go/internal/{name}" for name in component)
        common_top = {name.split("/", 1)[0] for name in component}
        path = (
            f"gateway-go/internal/{next(iter(common_top))}"
            if len(common_top) == 1
            else "gateway-go/internal"
        )
        findings.append(
            _finding(
                pillar=pillar_id,
                rule="component-cycle",
                path=path,
                severity="high" if len(component) >= 4 else "medium",
                evidence=f"Collapsed component SCC contains {len(component)} nodes: {', '.join(component)}",
                why=(
                    "Packages compile because the cycle is indirect, but subsystem ownership still "
                    "runs in both directions and forces cross-component reasoning."
                ),
                remediation=(
                    "Extract the shared contracts into a neutral port and make orchestration flow in "
                    "one direction across these components."
                ),
                priority=min(100, 88 + len(component)),
                related=related,
                identity=component,
            )
        )

    for fanout, package, soft, hard in sorted(direct_rows, reverse=True)[:8]:
        findings.append(
            _finding(
                pillar=pillar_id,
                rule="fanout-hotspot",
                path=_package_path(repo, package),
                severity="high" if fanout >= hard else "medium",
                evidence=f"Direct internal fan-out is {fanout}; role-adjusted soft/hard bars are {soft}/{hard}.",
                why="A broad dependency surface makes routine changes require repository-wide context.",
                remediation=(
                    "Move feature composition to owning modules and depend on narrow capability ports "
                    "instead of importing unrelated implementations."
                ),
                priority=70 + min(25, 25 * fanout / max(hard, 1)),
                related=tuple(
                    _package_path(repo, item) for item in sorted(repo.graph[package])[:12]
                ),
            )
        )

    for reach, package, soft, hard in sorted(two_hop_rows, reverse=True)[:5]:
        findings.append(
            _finding(
                pillar=pillar_id,
                rule="two-hop-blast",
                path=_package_path(repo, package),
                severity="high" if reach >= hard else "medium",
                evidence=f"One- and two-hop internal reach is {reach}; role-adjusted bars are {soft}/{hard}.",
                why="High two-hop reach means a local edit requires understanding distant contracts and adapters.",
                remediation="Split orchestration by capability and replace transitive implementation knowledge with ports.",
                priority=65 + min(20, 20 * reach / max(hard, 1)),
            )
        )

    metrics = {
        "packages": len(repo.packages),
        "internal_edges": sum(len(targets) for targets in repo.graph.values()),
        "forbidden_edges": len(forbidden),
        "component_sccs": [list(item) for item in repo.component_sccs],
        "cyclic_component_nodes": cyclic_nodes,
        "subscores": _rounded_map(subscores),
        "fanout_hotspots": [
            {"package": package, "fanout": count}
            for count, package, _, _ in sorted(direct_rows, reverse=True)[:12]
        ],
        "two_hop_hotspots": [
            {"package": package, "reach": count}
            for count, package, _, _ in sorted(two_hop_rows, reverse=True)[:12]
        ],
    }
    return Pillar(
        id=pillar_id,
        title="Boundary integrity",
        weight=12,
        score=score,
        intent="Keep dependency direction, component ownership, and change blast radius explicit.",
        metrics=metrics,
        findings=findings,
    )


def _change_locality(repo: RepositoryInventory) -> Pillar:
    pillar_id = "change-locality"
    history = repo.history
    if not history.available:
        return Pillar(
            id=pillar_id,
            title="Change locality",
            weight=10,
            score=0,
            intent="Keep changes local and prevent volatile packages from amplifying repository-wide risk.",
            metrics={"measured": False, "reason": history.detail},
            findings=[
                _finding(
                    pillar=pillar_id,
                    rule="history-unavailable",
                    path=".git",
                    severity="high",
                    evidence=history.detail,
                    why="Without change history, locality and volatile dependency hubs cannot be measured honestly.",
                    remediation="Provide at least 20 first-parent production-Go commits in the checkout.",
                    priority=100,
                )
            ],
        )

    package_counts = [commit.package_count for commit in history.commits]
    component_counts = [
        len({component_for(package) for package, _ in commit.packages})
        for commit in history.commits
    ]
    dominant_shares = [
        max(count for _, count in commit.packages) / commit.file_count
        for commit in history.commits
        if commit.file_count
    ]
    p90_packages = _quantile(package_counts, 0.90)
    p90_components = _quantile(component_counts, 0.90)
    median_dominant = statistics.median(dominant_shares) if dominant_shares else 0.0
    current_touches = {
        package: touches
        for package, touches in history.package_touches.items()
        if package in repo.packages
    }
    observed_hottest_package, observed_hottest_touches = (
        max(current_touches.items(), key=lambda item: (item[1], item[0]))
        if current_touches
        else ("", 0)
    )
    observed_hottest_share = observed_hottest_touches / history.commit_count

    # Raw touch frequency measures where product work happens, not whether that
    # work is local. Score only packages recurring in cross-component changes;
    # this supplies the missing integration-bottleneck evidence. A composition
    # root is expected to participate in feature wiring, so keep its raw and
    # cross-component frequency as evidence while leaving its coupling to the
    # scatter/co-change and current-graph volatile-hub signals.
    cross_component_touches: Counter[str] = Counter()
    for commit in history.commits:
        changed = [package for package, _ in commit.packages if package in repo.packages]
        if len({component_for(package) for package in changed}) > 1:
            cross_component_touches.update(changed)
    scored_touches = {
        package: touches
        for package, touches in cross_component_touches.items()
        if not _is_composition_root_package(package)
    }
    scored_hottest_package, scored_hottest_touches = (
        max(scored_touches.items(), key=lambda item: (item[1], item[0]))
        if scored_touches
        else ("", 0)
    )
    scored_hottest_share = scored_hottest_touches / history.commit_count

    hot_risks: list[tuple[float, str, int, int]] = []
    for package in repo.packages:
        touches = history.package_touches.get(package, 0)
        fanin = len(repo.reverse_graph[package])
        risk = (touches / history.commit_count) * fanin
        if risk > 0:
            hot_risks.append((risk, package, touches, fanin))
    hot_risk_score = tail_score((risk for risk, _, _, _ in hot_risks), soft=0.30, hard=3.0)

    subscores = {
        "p90_packages_per_change": descending_grade(p90_packages, 2, 6),
        "p90_components_per_change": descending_grade(p90_components, 1, 5),
        "median_dominant_package_share": ascending_grade(median_dominant, 0.55, 0.85),
        "non_composition_integration_hotspot_share": descending_grade(
            scored_hottest_share, 0.15, 0.45
        ),
        "volatile_dependency_hubs": hot_risk_score,
    }
    score = _bounded_average([(value, 0.20) for value in subscores.values()])

    findings: list[Finding] = []
    if p90_packages > 2 or p90_components > 1:
        findings.append(
            _finding(
                pillar=pillar_id,
                rule="scattered-change",
                path="gateway-go/internal",
                severity="high" if p90_packages >= 6 or p90_components >= 5 else "medium",
                evidence=(
                    f"Across {history.commit_count} commits, p90 touches {p90_packages:.0f} packages/"
                    f"{p90_components:.0f} components; median dominant-package share is {median_dominant:.2f}."
                ),
                why="Feature work spread across components increases regression surface and review cost.",
                remediation=(
                    "Move feature-specific wiring and policy behind the owning package so routine changes "
                    "touch one component plus its contract tests."
                ),
                priority=90,
            )
        )

    if scored_hottest_package and scored_hottest_share > 0.15:
        findings.append(
            _finding(
                pillar=pillar_id,
                rule="change-hotspot",
                path=_package_path(repo, scored_hottest_package),
                severity="high" if scored_hottest_share >= 0.45 else "medium",
                evidence=(
                    f"{scored_hottest_package} changed across components in "
                    f"{scored_hottest_touches}/{history.commit_count} "
                    f"production-Go commits ({100 * scored_hottest_share:.1f}%)."
                ),
                why=(
                    "A package recurring in a large share of cross-component changes is an "
                    "integration bottleneck."
                ),
                remediation="Move domain-specific assembly out and leave a stable, small composition contract.",
                priority=92,
            )
        )

    for risk, package, touches, fanin in sorted(hot_risks, reverse=True)[:6]:
        if risk <= 0.30:
            continue
        findings.append(
            _finding(
                pillar=pillar_id,
                rule="volatile-hub",
                path=_package_path(repo, package),
                severity="high" if risk >= 3.0 else "medium",
                evidence=(
                    f"Volatile-hub index is {risk:.2f}: {touches}/{history.commit_count} commits "
                    f"times {fanin} direct dependents."
                ),
                why="Many dependents consume a contract that changes frequently, amplifying every edit.",
                remediation="Stabilize and narrow the public contract, then move volatile implementation behind it.",
                priority=80 + min(15, risk * 4),
                related=tuple(
                    _package_path(repo, item) for item in sorted(repo.reverse_graph[package])[:12]
                ),
            )
        )

    for (left, right), count in sorted(
        history.cochange_counts.items(), key=lambda item: (-item[1], item[0])
    )[:5]:
        if count < 10 or left not in repo.packages or right not in repo.packages:
            continue
        findings.append(
            _finding(
                pillar=pillar_id,
                rule="cochange-coupling",
                path=_package_path(repo, left),
                severity="medium",
                evidence=f"{left} and {right} changed together in {count} recent commits.",
                why="Repeated cross-package co-change is evidence that the current boundary does not localize work.",
                remediation="Extract the shared reason-to-change or replace the shared implementation knowledge with a port.",
                priority=60 + min(20, count / 3),
                related=(_package_path(repo, right),),
                identity=(right,),
            )
        )

    metrics = {
        "history_commits": history.commit_count,
        "p90_packages_per_change": round(p90_packages, 2),
        "p90_components_per_change": round(p90_components, 2),
        "median_dominant_package_share": round(median_dominant, 3),
        "observed_hottest_package": observed_hottest_package,
        "observed_hottest_package_share": round(observed_hottest_share, 3),
        "scored_hottest_package": scored_hottest_package,
        "scored_hottest_package_share": round(scored_hottest_share, 3),
        "scored_hottest_cross_component_touches": scored_hottest_touches,
        "subscores": _rounded_map(subscores),
        "volatile_hubs": [
            {
                "package": package,
                "risk": round(risk, 3),
                "touches": touches,
                "dependents": fanin,
            }
            for risk, package, touches, fanin in sorted(hot_risks, reverse=True)[:12]
        ],
        "cochange_pairs": [
            {"left": pair[0], "right": pair[1], "commits": count}
            for pair, count in sorted(
                history.cochange_counts.items(), key=lambda item: (-item[1], item[0])
            )[:12]
        ],
    }
    return Pillar(
        id=pillar_id,
        title="Change locality",
        weight=10,
        score=score,
        intent="Keep changes local and prevent volatile packages from amplifying repository-wide risk.",
        metrics=metrics,
        findings=findings,
    )


def _top_level(package: str) -> str:
    parts = package.split("/")
    return parts[1] if len(parts) > 1 and parts[0] == "internal" else parts[0]


def _is_composition_root_package(package: str) -> bool:
    """Keep the hotspot exception on the root package, not its whole subtree."""

    return is_composition_root(package) and package == f"internal/{component_for(package)}"


def _two_hop_reach(repo: RepositoryInventory, package: str) -> int:
    direct = set(repo.graph[package])
    reached = set(direct)
    for target in direct:
        reached.update(repo.graph.get(target, ()))
    reached.discard(package)
    return len(reached)


def _worst_credit_mean(credits: Iterable[float], count: int = 12) -> float:
    values = sorted((float(value) for value in credits))[:count]
    return sum(values) / len(values) if values else 100.0


def _quantile(values: Iterable[float], quantile: float) -> float:
    ordered = sorted(float(value) for value in values)
    if not ordered:
        return 0.0
    index = max(0, min(len(ordered) - 1, math.ceil(quantile * len(ordered)) - 1))
    return ordered[index]
