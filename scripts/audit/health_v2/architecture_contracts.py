"""Contract explicitness and AI-navigation architecture pillars."""

from __future__ import annotations

import math
from typing import Iterable

from .inventory import RepositoryInventory, component_for
from .model import (
    Finding,
    Pillar,
    ascending_grade,
    descending_grade,
    stable_id,
    tail_score,
)


VERIFY = (
    "Run `python3 scripts/audit/codebase-health-v2.py --json` and confirm this "
    "finding disappears without an architecture exception."
)


def _responsibility_cohesion(repo: RepositoryInventory) -> Pillar:
    pillar_id = "responsibility-cohesion"
    packages = list(repo.packages.values())
    if not packages:
        return Pillar(
            id=pillar_id,
            title="Responsibility cohesion",
            weight=6,
            score=0,
            intent="Keep one history-backed reason to change per package.",
            metrics={"measured": False},
        )

    partner_components: dict[str, dict[str, int]] = {item.key: {} for item in packages}
    cross_component_touches: dict[str, int] = {item.key: 0 for item in packages}
    eligible_component_touches: dict[str, int] = {item.key: 0 for item in packages}
    bulk_commits_excluded = 0
    # Each commit contributes at most once per partner component, so a bulk
    # change cannot manufacture combinatorial entropy through package pairs.
    for commit in repo.history.commits:
        changed = [package for package, _ in commit.packages if package in repo.packages]
        if len({component_for(package) for package in changed}) > 8:
            bulk_commits_excluded += 1
            continue
        for package in changed:
            eligible_component_touches[package] += 1
            own = component_for(package)
            others = {component_for(value) for value in changed if component_for(value) != own}
            if others:
                cross_component_touches[package] += 1
            counts = partner_components[package]
            for component in others:
                counts[component] = counts.get(component, 0) + 1

    cochange_rows: list[tuple[float, str, int, int]] = []
    entropy_rows: list[tuple[float, float, str, int]] = []
    volatile_rows: list[tuple[float, str, int, int, int]] = []
    if repo.history.available:
        for item in packages:
            touches = repo.history.package_touches.get(item.key, 0)
            local_touches = eligible_component_touches[item.key]
            if local_touches >= 10:
                crossed = cross_component_touches[item.key]
                cochange_rows.append(
                    (crossed / local_touches, item.key, crossed, local_touches)
                )
                counts = partner_components[item.key]
                total = sum(counts.values())
                if total:
                    dominant = max(counts.values()) / total
                    entropy = (
                        -sum((value / total) * math.log(value / total) for value in counts.values())
                        / math.log(len(counts))
                        if len(counts) > 1
                        else 0.0
                    )
                    entropy_rows.append((entropy, dominant, item.key, len(counts)))
            fanin = len(repo.reverse_graph[item.key])
            if touches and fanin:
                blast = (
                    item.exported_declarations
                    * fanin
                    * touches
                    / repo.history.commit_count
                )
                volatile_rows.append(
                    (blast, item.key, item.exported_declarations, fanin, touches)
                )

    subscores = {
        "cross_component_cochange_tail": tail_score(
            (rate for rate, _, _, _ in cochange_rows), soft=0.20, hard=0.70
        ),
        "volatile_contract_blast_tail": tail_score(
            (blast for blast, _, _, _, _ in volatile_rows), soft=50, hard=500
        ),
        "change_partner_entropy_tail": tail_score(
            (entropy for entropy, _, _, _ in entropy_rows), soft=0.25, hard=0.75
        ),
        "dominant_responsibility_tail": _worst_credit_mean(
            ascending_grade(dominant, 0.25, 0.65)
            for _, dominant, _, _ in entropy_rows
        ),
    }
    score = (
        0.20 * subscores["cross_component_cochange_tail"]
        + 0.45 * subscores["volatile_contract_blast_tail"]
        + 0.25 * subscores["change_partner_entropy_tail"]
        + 0.10 * subscores["dominant_responsibility_tail"]
        if repo.history.available else 0.0
    )
    findings: list[Finding] = []
    if not repo.history.available:
        findings.append(
            _finding(
                pillar=pillar_id,
                rule="responsibility-history-unavailable",
                path=".git",
                severity="high",
                evidence=repo.history.detail,
                why="Responsibility cohesion cannot be inferred honestly from static size.",
                remediation="Provide the required first-parent production history.",
                priority=100,
            )
        )

    for rate, package, multi, touches in sorted(cochange_rows, reverse=True)[:6]:
        if rate <= 0.25:
            continue
        findings.append(
            _finding(
                pillar=pillar_id,
                rule="responsibility-cochange",
                path=_package_path(repo, package),
                severity="high" if rate >= 0.80 else "medium",
                evidence=f"{multi}/{touches} changes ({100 * rate:.1f}%) crossed a component boundary.",
                why="A package that frequently changes across components does not localize its responsibility.",
                remediation="Align ownership with the recurring co-change component or introduce a stable port.",
                priority=82 + 15 * rate,
            )
        )

    for blast, package, exports, fanin, touches in sorted(volatile_rows, reverse=True)[:6]:
        if blast <= 50:
            continue
        findings.append(
            _finding(
                pillar=pillar_id,
                rule="volatile-contract-responsibility",
                path=_package_path(repo, package),
                severity="high" if blast >= 500 else "medium",
                evidence=(
                    f"Volatile contract blast is {blast:.1f}: {exports} exports × "
                    f"{fanin} dependents × {touches}/{repo.history.commit_count} changes."
                ),
                why="A broad, widely consumed API that changes often spreads one responsibility across the graph.",
                remediation="Stabilize a narrow port and move volatile policy behind the owning capability.",
                priority=85 + min(12, blast / 100),
            )
        )

    for entropy, dominant, package, components in sorted(entropy_rows, reverse=True)[:6]:
        if entropy > 0.25 or dominant < 0.65:
            findings.append(
                _finding(
                    pillar=pillar_id,
                    rule="diffuse-change-responsibility",
                    path=_package_path(repo, package),
                    severity="high" if entropy >= 0.75 and dominant <= 0.25 else "medium",
                    evidence=(f"Partner entropy={entropy:.2f} across {components} components; "
                              f"dominant responsibility share={dominant:.2f}."),
                    why="No focused component boundary explains the package's recent changes.",
                    remediation="Choose one owning capability and move the remaining change clusters behind ports.",
                    priority=78 + 12 * entropy + 8 * (1 - dominant),
                )
            )

    metrics = {
        "subscores": _rounded_map(subscores),
        "scoring_basis": [
            "cross-component cochange",
            "exports × fan-in × change frequency",
            "change-partner entropy",
            "dominant responsibility share",
        ],
        "bulk_commits_excluded_from_entropy": bulk_commits_excluded,
        "volatile_contract_hotspots": [
            {
                "package": package,
                "blast": round(blast, 2),
                "exports": exports,
                "fanin": fanin,
                "touches": touches,
            }
            for blast, package, exports, fanin, touches in sorted(volatile_rows, reverse=True)[:12]
        ],
        "responsibility_entropy_hotspots": [
            {
                "package": package,
                "entropy": round(entropy, 3),
                "dominant_share": round(dominant, 3),
                "partner_components": components,
            }
            for entropy, dominant, package, components in sorted(entropy_rows, reverse=True)[:12]
        ],
        "supporting_size_diagnostics": {
            "scored": False,
            "packages": [
            {
                "package": item.key,
                "files": item.source_files,
                "loc": item.source_loc,
            }
            for item in sorted(packages, key=lambda value: (-value.source_loc, value.key))[:12]
            ],
        },
    }
    return Pillar(
        id=pillar_id,
        title="Responsibility cohesion",
        weight=6,
        score=score,
        intent="Keep one history-backed reason to change per package.",
        metrics=metrics,
        findings=findings,
    )


def _contract_explicitness(repo: RepositoryInventory) -> Pillar:
    pillar_id = "contract-explicitness"
    packages = list(repo.packages.values())
    if not packages:
        return Pillar(
            id=pillar_id,
            title="Contract explicitness",
            weight=8,
            score=0,
            intent="Prefer explicit typed boundary contracts over dynamic exported values.",
            metrics={"measured": False},
        )

    exported = sum(item.exported_declarations for item in packages)
    dynamic = sum(item.dynamic_exported_contracts for item in packages)
    dynamic_ratio = dynamic / exported if exported else 0.0
    dynamic_tail = tail_score(
        (item.dynamic_exported_contracts for item in packages), soft=1, hard=10
    )
    contract_blasts = [
        (item.exported_declarations * len(repo.reverse_graph[item.key]), item)
        for item in packages
    ]
    subscores = {
        "dynamic_export_ratio": descending_grade(dynamic_ratio, 0.02, 0.15),
        "dynamic_contract_tail": dynamic_tail,
    }
    score = _bounded_average(
        [
            (subscores["dynamic_export_ratio"], 0.50),
            (subscores["dynamic_contract_tail"], 0.50),
        ]
    )
    findings: list[Finding] = []

    for item in sorted(
        packages, key=lambda package: (-package.dynamic_exported_contracts, package.key)
    )[:8]:
        if item.dynamic_exported_contracts <= 1:
            continue
        findings.append(
            _finding(
                pillar=pillar_id,
                rule="dynamic-exported-contract",
                path=item.path,
                severity="high" if item.dynamic_exported_contracts >= 10 else "medium",
                evidence=(
                    f"{item.dynamic_exported_contracts}/{item.exported_declarations} exported declarations "
                    "contain any, interface{}, map[string]any, or json.RawMessage."
                ),
                why="Dynamic exported values move schema errors from compile time into runtime paths.",
                remediation="Introduce typed request/result structs at the package boundary and decode raw wire data in adapters.",
                priority=78 + min(18, item.dynamic_exported_contracts),
            )
        )

    metrics = {
        "exported_declarations": exported,
        "dynamic_exported_contracts": dynamic,
        "dynamic_export_ratio": round(dynamic_ratio, 4),
        "subscores": _rounded_map(subscores),
        "dynamic_contract_hotspots": [
            {
                "package": item.key,
                "dynamic_exports": item.dynamic_exported_contracts,
                "exports": item.exported_declarations,
            }
            for item in sorted(
                packages, key=lambda package: (-package.dynamic_exported_contracts, package.key)
            )[:12]
        ],
        "dependency_bag_diagnostics": {
            "scored": False,
            "packages": [
                {"package": item.key, "fields": item.max_dependency_bag_fields}
                for item in sorted(
                    packages,
                    key=lambda package: (-package.max_dependency_bag_fields, package.key),
                )[:12]
            ],
        },
        "contract_blast_diagnostics": {
            "scored": False,
            "packages": [
                {"package": item.key, "blast": blast}
                for blast, item in sorted(
                    contract_blasts, key=lambda row: (-row[0], row[1].key)
                )[:12]
            ],
        },
    }
    return Pillar(
        id=pillar_id,
        title="Contract explicitness",
        weight=8,
        score=score,
        intent="Prefer explicit typed boundary contracts over dynamic exported values.",
        metrics=metrics,
        findings=findings,
    )


def _bounded_average(weighted: Iterable[tuple[float, float]]) -> float:
    values = list(weighted)
    if not values:
        return 0.0
    denominator = sum(weight for _, weight in values)
    average = sum(value * weight for value, weight in values) / denominator
    # A disastrous facet cannot be hidden by unrelated clean facets.
    return min(average, min(value for value, _ in values) + 30.0)


def _worst_credit_mean(credits: Iterable[float], count: int = 12) -> float:
    values = sorted((float(value) for value in credits))[:count]
    return sum(values) / len(values) if values else 100.0


def _package_path(repo: RepositoryInventory, package: str) -> str:
    item = repo.packages.get(package)
    return item.path if item is not None else f"gateway-go/{package}"


def _finding(
    *,
    pillar: str,
    rule: str,
    path: str,
    severity: str,
    evidence: str,
    why: str,
    remediation: str,
    priority: float,
    related: tuple[str, ...] = (),
    identity: tuple[str, ...] = (),
) -> Finding:
    return Finding(
        id=stable_id(rule, path, *identity),
        pillar=pillar,
        severity=severity,
        path=path,
        evidence=evidence,
        why=why,
        remediation=remediation,
        verify=VERIFY,
        priority=priority,
        related_paths=related,
    )


def _rounded_map(values: dict[str, float]) -> dict[str, float]:
    return {key: round(value, 1) for key, value in values.items()}
