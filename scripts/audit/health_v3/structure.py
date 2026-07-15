"""Structure domain — map Health Bench 2.x pillars through 3.0 calibration.

The v2 evaluators remain the evidence collectors. Pillar scores are then
rescaled onto the eight Structure metrics so current main lands near the
design worksheet (~50 domain score). See docs/research/health-bench-3.0.md.
"""

from __future__ import annotations

import sys
from pathlib import Path
from typing import Any

from .model import (
    AI_LANE_IMPACT,
    DOMAIN_WEIGHTS,
    Domain,
    Evidence,
    Finding,
    Metric,
    clamp,
    stable_id,
)

AUDIT_DIR = Path(__file__).resolve().parent.parent
if str(AUDIT_DIR) not in sys.path:
    sys.path.insert(0, str(AUDIT_DIR))

# Calibration anchors from the design worksheet (v2 scores → v3 targets).
# Factors are derived so improving v2 evidence still raises v3 scores.
_CHANGE_BLAST_SCALE = 32.0 / 55.8  # avg(locality 55, cohesion 56.6)
_BOUNDARY_SCALE = 0.75
_CONTRACT_SCALE = 0.55
_AI_EXTRA_TIGHTEN = 0.90  # after unpaid-lane impact
_COMPLEXITY_SCALE = 0.70
_SAFETY_SCALE = 0.62
_TEST_TRUTH_SCALE = 0.70
_TEST_CRAFT_SCALE = 0.833
_DELIVERY_SCALE = 0.42
_DEAD_SURFACE_BOOTSTRAP = 40.0


def _v2_scores(root: Path, *, profile: str) -> tuple[dict[str, float], list[Any], list[Any]]:
    import codebase_health_v2 as v2

    report = v2.collect_report(root, profile=profile)
    scores = {pillar.id: pillar.score for pillar in report.pillars}
    return scores, report.findings, report.evidence


def _finding(
    *,
    pillar: str,
    severity: str,
    path: str,
    evidence: str,
    why: str,
    remediation: str,
    verify: str,
    priority: float,
) -> Finding:
    return Finding(
        id=stable_id(pillar, path, evidence[:80]),
        domain="structure",
        pillar=pillar,
        severity=severity,
        path=path,
        evidence=evidence,
        why=why,
        remediation=remediation,
        verify=verify,
        priority=priority,
    )


def _map_v2_findings(v2_findings: list[Any], pillar_map: dict[str, str]) -> dict[str, list[Finding]]:
    grouped: dict[str, list[Finding]] = {pid: [] for pid in pillar_map.values()}
    for raw in v2_findings:
        target = pillar_map.get(getattr(raw, "pillar", ""), "")
        if not target:
            continue
        grouped[target].append(
            Finding(
                id=f"structure:{raw.id}",
                domain="structure",
                pillar=target,
                severity=raw.severity,
                path=raw.path,
                evidence=raw.evidence,
                why=raw.why,
                remediation=raw.remediation,
                verify=raw.verify,
                priority=float(raw.priority),
                related_paths=tuple(raw.related_paths),
            )
        )
    return grouped


def evaluate_structure(root: Path, *, profile: str = "fast") -> Domain:
    scores, v2_findings, v2_evidence = _v2_scores(root, profile=profile)

    locality = scores.get("change-locality", 0.0)
    cohesion = scores.get("responsibility-cohesion", 0.0)
    boundary = scores.get("boundary-integrity", 0.0)
    contract = scores.get("contract-explicitness", 0.0)
    ai_nav = scores.get("ai-navigability", 0.0)
    ai_ready = scores.get("ai-change-readiness", 0.0)
    complexity = scores.get("complexity-hotspots", 0.0)
    safety = scores.get("runtime-safety", 0.0)
    test_eff = scores.get("test-effectiveness", 0.0)
    test_maint = scores.get("test-maintainability", 0.0)
    delivery = scores.get("delivery-confidence", 0.0)

    change_blast = clamp(((locality + cohesion) / 2.0) * _CHANGE_BLAST_SCALE)
    boundary_contracts = clamp(
        0.55 * boundary * _BOUNDARY_SCALE + 0.45 * contract * _CONTRACT_SCALE
    )
    go_ai = (ai_nav + ai_ready) / 2.0
    ai_maintainability = clamp(go_ai * AI_LANE_IMPACT["go"] * _AI_EXTRA_TIGHTEN)
    complexity_safety = clamp(
        0.5 * complexity * _COMPLEXITY_SCALE + 0.5 * safety * _SAFETY_SCALE
    )
    test_truth = clamp(test_eff * _TEST_TRUTH_SCALE)
    test_craft = clamp(test_maint * _TEST_CRAFT_SCALE)
    delivery_proof = clamp(delivery * _DELIVERY_SCALE)
    dead_surface = _DEAD_SURFACE_BOOTSTRAP

    pillar_map = {
        "change-locality": "change-blast",
        "responsibility-cohesion": "change-blast",
        "boundary-integrity": "boundary-contracts",
        "contract-explicitness": "boundary-contracts",
        "ai-navigability": "ai-maintainability",
        "ai-change-readiness": "ai-maintainability",
        "complexity-hotspots": "complexity-safety",
        "runtime-safety": "complexity-safety",
        "test-effectiveness": "test-truth",
        "test-maintainability": "test-craft",
        "delivery-confidence": "delivery-proof",
    }
    findings_by_pillar = _map_v2_findings(v2_findings, pillar_map)
    for pillar_id in (
        "change-blast",
        "boundary-contracts",
        "ai-maintainability",
        "complexity-safety",
        "test-truth",
        "test-craft",
        "delivery-proof",
        "dead-surface",
    ):
        findings_by_pillar.setdefault(pillar_id, [])

    unpaid = 1.0 - AI_LANE_IMPACT["go"]
    findings_by_pillar["ai-maintainability"].append(
        _finding(
            pillar="ai-maintainability",
            severity="high" if unpaid >= 0.5 else "medium",
            path="docs/agent-rules",
            evidence=(
                f"AI maintainability credits only the Go lane ({AI_LANE_IMPACT['go']:.0%} "
                f"product impact); Kotlin/TS/Python shares ({unpaid:.0%}) are unpaid"
            ),
            why="Agents changing native/desktop/ops surfaces lack scored navigation evidence",
            remediation="Add grounded AI guides with real symbols/paths/tests/verify commands per unpaid lane",
            verify="python3 scripts/audit/health-bench-v3.py --format json",
            priority=70.0,
        )
    )
    findings_by_pillar["dead-surface"].append(
        _finding(
            pillar="dead-surface",
            severity="medium",
            path="scripts/audit",
            evidence="Dead surface uses bootstrap inventory proxy until deep deadcode scoring lands",
            why="Unscored dead exports can accumulate while the domain looks stable",
            remediation="Run deep deadcode evidence and replace the bootstrap floor",
            verify="python3 scripts/audit/health-bench-v3.py --profile deep --format json",
            priority=40.0,
        )
    )
    findings_by_pillar["delivery-proof"].append(
        _finding(
            pillar="delivery-proof",
            severity="medium",
            path=".github/workflows",
            evidence=f"Delivery proof rescales v2 delivery-confidence {delivery:.1f} → {delivery_proof:.1f}",
            why="Workflow existence alone overstated merge protection under the world-class bar",
            remediation="Prove required-check and path-trigger coherence for high-impact lanes",
            verify="python3 scripts/audit/health-bench-v3.py --format json",
            priority=55.0,
        )
    )

    evidence = [
        Evidence(
            "structure-v2-source",
            "measured",
            f"mapped from health_v2 profile={profile}",
            required=True,
        ),
        Evidence(
            "ai-lanes-kotlin-typescript-python",
            "unavailable",
            "unpaid product-impact share deducted from ai-maintainability",
            required=False,
        ),
        Evidence(
            "dead-surface-deep",
            "bootstrap",
            f"bootstrap score {_DEAD_SURFACE_BOOTSTRAP}",
            required=False,
        ),
    ]
    for item in v2_evidence:
        status = getattr(item, "status", "measured")
        if status not in {"measured", "unavailable", "not_applicable", "bootstrap"}:
            status = "measured"
        evidence.append(
            Evidence(
                f"v2:{getattr(item, 'name', 'evidence')}",
                status,
                getattr(item, "detail", ""),
                required=False,
            )
        )

    metrics = [
        Metric(
            "change-blast",
            "Change blast",
            20,
            change_blast,
            "How many responsibilities/components does one feature touch?",
            {
                "v2_change_locality": locality,
                "v2_responsibility_cohesion": cohesion,
                "scale": _CHANGE_BLAST_SCALE,
            },
            findings_by_pillar["change-blast"],
        ),
        Metric(
            "boundary-contracts",
            "Boundary & contracts",
            14,
            boundary_contracts,
            "Are deps/ownership/typed contracts one-way?",
            {
                "v2_boundary_integrity": boundary,
                "v2_contract_explicitness": contract,
            },
            findings_by_pillar["boundary-contracts"],
        ),
        Metric(
            "ai-maintainability",
            "AI maintainability",
            16,
            ai_maintainability,
            "Can an agent find entrypoints and verify commands on every product lane?",
            {
                "v2_ai_navigability": ai_nav,
                "v2_ai_change_readiness": ai_ready,
                "go_lane_impact": AI_LANE_IMPACT["go"],
            },
            findings_by_pillar["ai-maintainability"],
        ),
        Metric(
            "complexity-safety",
            "Complexity & static safety",
            10,
            complexity_safety,
            "Are worst paths and error chains safe?",
            {
                "v2_complexity_hotspots": complexity,
                "v2_runtime_safety": safety,
            },
            findings_by_pillar["complexity-safety"],
        ),
        Metric(
            "test-truth",
            "Test truth",
            16,
            test_truth,
            "Do independent risk behaviors have real oracles?",
            {"v2_test_effectiveness": test_eff},
            findings_by_pillar["test-truth"],
        ),
        Metric(
            "test-craft",
            "Test craft",
            8,
            test_craft,
            "Are tests intentional, isolated, and discoverable?",
            {"v2_test_maintainability": test_maint},
            findings_by_pillar["test-craft"],
        ),
        Metric(
            "delivery-proof",
            "Delivery proof",
            10,
            delivery_proof,
            "Do high-impact lanes actually gate merges?",
            {"v2_delivery_confidence": delivery},
            findings_by_pillar["delivery-proof"],
        ),
        Metric(
            "dead-surface",
            "Dead surface",
            6,
            dead_surface,
            "Is dead export / unreferenced surface accumulating?",
            {"mode": "bootstrap"},
            findings_by_pillar["dead-surface"],
        ),
    ]
    return Domain(
        id="structure",
        title="Structure",
        weight=DOMAIN_WEIGHTS["structure"],
        metrics=metrics,
        evidence=evidence,
        ratcheted=True,
    )
