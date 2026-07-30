"""Structure domain — map Health Bench 2.x pillars through 3.0 calibration.

The v2 evaluators remain the evidence collectors. Pillar scores are then
rescaled onto the eight Structure metrics so current main lands near the
design worksheet (~50 domain score). See docs/research/health-bench-3.0.md.
"""

from __future__ import annotations

import re
import shlex
import sys
from dataclasses import dataclass
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
_NON_GO_AI_LANES = ("kotlin", "typescript", "python")
_AI_LANE_GUIDE_DOC = Path("docs/agent-rules/product-lane-ai-maintainability.md")
_BACKTICK_RE = re.compile(r"`([^`\n]+)`")
_WORD_RE = re.compile(r"\b[A-Za-z_][A-Za-z0-9_]*\b")
_MAKE_TARGET_RE = re.compile(r"(?m)^([A-Za-z0-9_.%/-]+)\s*:(?!=)")
_KOTLIN_SYMBOL_RE = re.compile(
    r"(?m)^\s*(?:@\w+(?:\([^)]*\))?\s*)*"
    r"(?:internal\s+|private\s+|public\s+|actual\s+|expect\s+|suspend\s+|inline\s+)*"
    r"(?:data\s+class|sealed\s+class|enum\s+class|class|object|interface|fun)\s+"
    r"([A-Za-z_][A-Za-z0-9_]*)"
)
_TYPESCRIPT_SYMBOL_RE = re.compile(
    r"(?m)^\s*(?:export\s+)?(?:async\s+function|function|const|interface|type|class)\s+"
    r"([A-Za-z_][A-Za-z0-9_]*)"
)
_PYTHON_SYMBOL_RE = re.compile(r"(?m)^(?:def|class)\s+([A-Za-z_][A-Za-z0-9_]*)")
_LANE_GUIDE_CONTRACTS: dict[str, dict[str, Any]] = {
    "kotlin": {
        "heading": "## Kotlin Native Client",
        "source_prefixes": ("client-android/app/composeApp/src/",),
        "test_prefixes": ("client-android/app/composeApp/src/commonTest/",),
        "symbol_re": _KOTLIN_SYMBOL_RE,
    },
    "typescript": {
        "heading": "## TypeScript Desktop Workstation",
        "source_prefixes": ("andromeda/src/",),
        "test_prefixes": ("andromeda/src/",),
        "test_suffixes": (".test.ts", ".test.tsx"),
        "symbol_re": _TYPESCRIPT_SYMBOL_RE,
    },
    "python": {
        "heading": "## Python Audit And Ops Scripts",
        "source_prefixes": ("scripts/audit/", "scripts/dev/", "scripts/deploy/"),
        "test_prefixes": ("scripts/audit/test_", "scripts/dev/test_"),
        "symbol_re": _PYTHON_SYMBOL_RE,
    },
}


@dataclass(frozen=True)
class LaneGuideEvidence:
    lane: str
    status: str
    detail: str
    guide_path: str | None = None
    source_files: tuple[str, ...] = ()
    test_files: tuple[str, ...] = ()
    entry_symbols: tuple[str, ...] = ()
    verify_commands: tuple[str, ...] = ()

    @property
    def measured(self) -> bool:
        return self.status == "measured"

    def to_dict(self) -> dict[str, Any]:
        return {
            "status": self.status,
            "detail": self.detail,
            "guide_path": self.guide_path,
            "source_files": list(self.source_files),
            "test_files": list(self.test_files),
            "entry_symbols": list(self.entry_symbols),
            "verify_commands": list(self.verify_commands),
        }


def _v2_scores(root: Path, *, profile: str) -> tuple[dict[str, float], list[Any], list[Any]]:
    import codebase_health_v2 as v2

    report = v2.collect_report(root, profile=profile)
    scores = {pillar.id: pillar.score for pillar in report.pillars}
    return scores, report.findings, report.evidence


def _make_targets(root: Path) -> set[str]:
    try:
        text = (root / "Makefile").read_text(encoding="utf-8", errors="replace")
    except OSError:
        return set()
    return set(_MAKE_TARGET_RE.findall(text))


def _section(text: str, heading: str) -> str:
    start = text.find(heading)
    if start < 0:
        return ""
    next_heading = text.find("\n## ", start + len(heading))
    end = len(text) if next_heading < 0 else next_heading
    return text[start:end]


def _path_tokens(section: str) -> tuple[str, ...]:
    paths: set[str] = set()
    for token in _BACKTICK_RE.findall(section):
        cleaned = token.strip().lstrip("./")
        if not cleaned or " " in cleaned:
            continue
        if cleaned.endswith((".kt", ".ts", ".tsx", ".py", ".sh")):
            paths.add(cleaned)
    return tuple(sorted(paths))


def _command_tokens(section: str) -> tuple[str, ...]:
    commands: set[str] = set()
    for token in _BACKTICK_RE.findall(section):
        cleaned = token.strip().lstrip("$ ")
        if cleaned.startswith(("./scripts/", "scripts/")) and cleaned.endswith(".py"):
            continue
        if cleaned.startswith((
            "make ",
            "cd ",
            "pnpm ",
            "python",
            "./gradlew",
            "gradle ",
            "scripts/",
            "./scripts/",
        )):
            commands.add(cleaned)
    return tuple(sorted(commands))


def _symbols_in(paths: list[Path], root: Path, symbol_re: re.Pattern[str]) -> set[str]:
    symbols: set[str] = set()
    for path in paths:
        try:
            text = (root / path).read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        symbols.update(symbol_re.findall(text))
    return symbols


def _is_lane_test_path(path: Path, contract: dict[str, Any]) -> bool:
    posix = path.as_posix()
    suffixes = contract.get("test_suffixes")
    if suffixes:
        return posix.endswith(tuple(suffixes))
    return any(posix.startswith(prefix) for prefix in contract["test_prefixes"])


def _is_generated_path(path: Path) -> bool:
    posix = path.as_posix()
    return "/generated/" in posix or "/gen/" in posix


def _lane_command_valid(
    root: Path,
    lane: str,
    command: str,
    make_targets: set[str],
) -> bool:
    cleaned = command.strip().lstrip("$ ")
    cwd = root
    if cleaned.startswith("cd ") and "&&" in cleaned:
        prefix, cleaned = cleaned.split("&&", 1)
        try:
            target = shlex.split(prefix)[1]
        except (IndexError, ValueError):
            return False
        cwd = (root / target).resolve()
        if not cwd.exists():
            return False
        cleaned = cleaned.strip()
    try:
        parts = shlex.split(cleaned)
    except ValueError:
        return False
    if not parts:
        return False
    if parts[0] == "make" and len(parts) >= 2:
        target = parts[1]
        if target not in make_targets:
            return False
        args = set(parts[2:])
        if lane == "kotlin":
            return target.startswith("kotlin") or (target == "ci" and "ARGS=--kotlin" in args)
        if lane == "typescript":
            return target in {"ci"} and "ARGS=--andromeda" in args
        if lane == "python":
            return target in {"python-test", "python-lint", "health-v3-test"}
        return False
    if parts[0] in {"pnpm", "corepack"}:
        if lane != "typescript" or "andromeda" not in cwd.parts:
            return False
        subcommand = parts[2] if parts[0] == "corepack" and len(parts) > 2 else parts[1]
        return subcommand in {"verify", "test", "typecheck", "lint", "build", "gen:wire"}
    if parts[0] in {"./gradlew", "gradle"}:
        return lane == "kotlin" and "client-android" in cwd.parts
    if parts[0].startswith("python"):
        if lane != "python" or len(parts) < 2:
            return False
        if parts[1] == "-m":
            return len(parts) > 2 and parts[2] == "unittest"
        script = Path(parts[1].lstrip("./"))
        return script.exists() or (root / script).exists()
    if parts[0].startswith(("./scripts/", "scripts/")):
        script = Path(parts[0].lstrip("./"))
        if not (root / script).exists():
            return False
        if lane == "kotlin":
            return script.as_posix().startswith("scripts/dev/native-app")
        if lane == "python":
            return script.as_posix().startswith(("scripts/audit/", "scripts/dev/"))
    return False


def _validate_lane_guide(
    root: Path,
    guide_path: Path,
    text: str,
    lane: str,
    make_targets: set[str],
) -> LaneGuideEvidence:
    contract = _LANE_GUIDE_CONTRACTS[lane]
    section = _section(text, contract["heading"])
    if not section:
        return LaneGuideEvidence(
            lane=lane,
            status="unavailable",
            detail=f"missing section {contract['heading']}",
            guide_path=guide_path.as_posix(),
        )
    paths = [Path(path) for path in _path_tokens(section)]
    source_paths = [
        path for path in paths
        if any(path.as_posix().startswith(prefix) for prefix in contract["source_prefixes"])
        and not _is_lane_test_path(path, contract)
        and not _is_generated_path(path)
        and (root / path).exists()
    ]
    test_paths = [
        path for path in paths
        if _is_lane_test_path(path, contract)
        and (root / path).exists()
    ]
    source_symbols = _symbols_in(source_paths, root, contract["symbol_re"])
    mentioned_symbols = tuple(sorted(set(_WORD_RE.findall(section)) & source_symbols))
    commands = tuple(
        command for command in _command_tokens(section)
        if _lane_command_valid(root, lane, command, make_targets)
    )

    gaps: list[str] = []
    if not source_paths:
        gaps.append("no existing source path")
    if not test_paths:
        gaps.append("no existing test path")
    if not mentioned_symbols:
        gaps.append("no source-defined entry symbol")
    if not commands:
        gaps.append("no valid verify command")
    if gaps:
        return LaneGuideEvidence(
            lane=lane,
            status="unavailable",
            detail=", ".join(gaps),
            guide_path=guide_path.as_posix(),
            source_files=tuple(sorted(path.as_posix() for path in source_paths)),
            test_files=tuple(sorted(path.as_posix() for path in test_paths)),
            entry_symbols=mentioned_symbols,
            verify_commands=commands,
        )
    return LaneGuideEvidence(
        lane=lane,
        status="measured",
        detail="grounded guide names source, tests, entry symbols, and verify commands",
        guide_path=guide_path.as_posix(),
        source_files=tuple(sorted(path.as_posix() for path in source_paths)),
        test_files=tuple(sorted(path.as_posix() for path in test_paths)),
        entry_symbols=mentioned_symbols,
        verify_commands=commands,
    )


def _collect_ai_lane_guides(root: Path) -> dict[str, LaneGuideEvidence]:
    guide_path = _AI_LANE_GUIDE_DOC
    absolute = root / guide_path
    if not absolute.exists():
        return {
            lane: LaneGuideEvidence(
                lane=lane,
                status="unavailable",
                detail=f"missing {_AI_LANE_GUIDE_DOC.as_posix()}",
                guide_path=guide_path.as_posix(),
            )
            for lane in _NON_GO_AI_LANES
        }
    try:
        text = absolute.read_text(encoding="utf-8", errors="replace")
    except OSError as exc:
        return {
            lane: LaneGuideEvidence(
                lane=lane,
                status="unavailable",
                detail=f"guide unreadable: {exc}",
                guide_path=guide_path.as_posix(),
            )
            for lane in _NON_GO_AI_LANES
        }
    make_targets = _make_targets(root)
    return {
        lane: _validate_lane_guide(root, guide_path, text, lane, make_targets)
        for lane in _NON_GO_AI_LANES
    }


def _measured_ai_lanes(guides: dict[str, LaneGuideEvidence]) -> tuple[str, ...]:
    return tuple(lane for lane in _NON_GO_AI_LANES if guides.get(lane) and guides[lane].measured)


def _missing_ai_lanes(guides: dict[str, LaneGuideEvidence]) -> tuple[str, ...]:
    return tuple(lane for lane in _NON_GO_AI_LANES if lane not in _measured_ai_lanes(guides))


def _ai_lane_impact_coverage(guides: dict[str, LaneGuideEvidence]) -> float:
    measured = _measured_ai_lanes(guides)
    if len(measured) == len(_NON_GO_AI_LANES):
        return 1.0
    return AI_LANE_IMPACT["go"] + sum(AI_LANE_IMPACT[lane] for lane in measured)


def _ai_lane_evidence_detail(guides: dict[str, LaneGuideEvidence]) -> str:
    missing = _missing_ai_lanes(guides)
    if not missing:
        return "grounded guide evidence measured for kotlin, typescript, python"
    if len(missing) == len(_NON_GO_AI_LANES):
        return "unpaid product-impact share deducted from ai-maintainability"
    return f"partial non-Go guide evidence; missing lanes: {', '.join(missing)}"


def _unpaid_ai_lane_finding(guides: dict[str, LaneGuideEvidence]) -> Finding | None:
    missing = _missing_ai_lanes(guides)
    if not missing:
        return None
    if len(missing) == len(_NON_GO_AI_LANES):
        evidence = (
            f"AI maintainability credits only the Go lane ({AI_LANE_IMPACT['go']:.0%} "
            f"product impact); Kotlin/TS/Python shares ({1.0 - AI_LANE_IMPACT['go']:.0%}) "
            "are unpaid"
        )
    else:
        coverage = _ai_lane_impact_coverage(guides)
        evidence = (
            f"AI maintainability credits grounded guides for "
            f"{', '.join(('go', *_measured_ai_lanes(guides)))} "
            f"({coverage:.0%} product impact); {', '.join(missing)} shares remain unpaid"
        )
    return _finding(
        pillar="ai-maintainability",
        severity="high" if len(missing) == len(_NON_GO_AI_LANES) else "medium",
        path="docs/agent-rules",
        evidence=evidence,
        why="Agents changing native/desktop/ops surfaces lack scored navigation evidence",
        remediation="Add grounded AI guides with real symbols/paths/tests/verify commands per unpaid lane",
        verify="python3 scripts/audit/health-bench-v3.py --format json",
        priority=70.0,
    )


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
    ai_lane_guides = _collect_ai_lane_guides(root)
    ai_lane_impact = _ai_lane_impact_coverage(ai_lane_guides)
    ai_maintainability = clamp(go_ai * ai_lane_impact * _AI_EXTRA_TIGHTEN)
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

    unpaid_ai_lane_finding = _unpaid_ai_lane_finding(ai_lane_guides)
    if unpaid_ai_lane_finding is not None:
        findings_by_pillar["ai-maintainability"].append(unpaid_ai_lane_finding)
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
            "measured" if not _missing_ai_lanes(ai_lane_guides) else "unavailable",
            _ai_lane_evidence_detail(ai_lane_guides),
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
                "measured_product_lanes": ["go", *_measured_ai_lanes(ai_lane_guides)],
                "unmeasured_product_lanes": list(_missing_ai_lanes(ai_lane_guides)),
                "lane_impact_coverage": round(ai_lane_impact, 3),
                "lane_guide_evidence": {
                    lane: guide.to_dict() for lane, guide in ai_lane_guides.items()
                },
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
