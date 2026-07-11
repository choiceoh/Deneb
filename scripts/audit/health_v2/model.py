"""Stable report model and score helpers for Health Bench 2.0."""

from __future__ import annotations

import hashlib
import math
from dataclasses import dataclass, field
from typing import Any, Iterable

SCHEMA_VERSION = 2
RUBRIC_VERSION = "2.1.1"
SEVERITY_ORDER = {"critical": 4, "high": 3, "medium": 2, "low": 1, "info": 0}

# Product impact, not source volume. The gateway owns runtime behavior; native
# and desktop clients are user-facing; Python/shell/Rust are supporting lanes.
PRODUCT_LANE_IMPACT = {
    "go": 0.45,
    "kotlin": 0.25,
    "typescript": 0.15,
    "python": 0.10,
    "shell": 0.03,
    "rust": 0.02,
}


def clamp(value: float, low: float = 0.0, high: float = 100.0) -> float:
    return max(low, min(high, value))


def descending_grade(value: float, soft: float, hard: float) -> float:
    """Return 100 at/below ``soft`` and 0 at/above ``hard``."""
    if hard <= soft:
        raise ValueError("hard threshold must be greater than soft threshold")
    return 100.0 * (1.0 - clamp((value - soft) / (hard - soft), 0.0, 1.0))


def ascending_grade(value: float, hard: float, soft: float) -> float:
    """Return 0 at/below ``hard`` and 100 at/above ``soft``."""
    if soft <= hard:
        raise ValueError("soft threshold must be greater than hard threshold")
    return 100.0 * clamp((value - hard) / (soft - hard), 0.0, 1.0)


def geometric_mean(values: Iterable[float]) -> float:
    vals = [clamp(float(value)) for value in values]
    if not vals:
        return 0.0
    if any(value <= 0.0 for value in vals):
        return 0.0
    return math.exp(sum(math.log(value) for value in vals) / len(vals))


def tail_score(values: Iterable[float], *, soft: float, hard: float, count: int = 12) -> float:
    """Grade the worst tail so many tiny clean units cannot dilute hotspots."""
    if count <= 0:
        raise ValueError("tail count must be positive")
    worst = sorted((float(value) for value in values), reverse=True)[:count]
    if not worst:
        return 100.0
    penalty = sum(100.0 - descending_grade(value, soft, hard) for value in worst)
    # Missing slots are neutral, not a denominator waiting to be filled. Adding
    # clean functions/files therefore cannot dilute an existing hotspot.
    return 100.0 - penalty / count


def stable_id(rule: str, *parts: object) -> str:
    raw = "\x00".join([rule, *(str(part) for part in parts)])
    digest = hashlib.blake2s(raw.encode("utf-8"), digest_size=6).hexdigest()
    return f"{rule}:{digest}"


@dataclass(frozen=True)
class Evidence:
    name: str
    status: str
    detail: str
    required: bool = False

    def __post_init__(self) -> None:
        if self.status not in {"measured", "unavailable", "not_applicable"}:
            raise ValueError(f"invalid evidence status: {self.status}")

    def to_dict(self) -> dict[str, Any]:
        return {
            "name": self.name,
            "status": self.status,
            "detail": self.detail,
            "required": self.required,
        }


@dataclass(frozen=True)
class Finding:
    id: str
    pillar: str
    severity: str
    path: str
    evidence: str
    why: str
    remediation: str
    verify: str
    priority: float = 0.0
    related_paths: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        if self.severity not in SEVERITY_ORDER:
            raise ValueError(f"invalid finding severity: {self.severity}")
        if not 0.0 <= self.priority <= 100.0:
            raise ValueError(f"finding priority must be between 0 and 100: {self.priority}")
        for field_name in ("id", "pillar", "path", "evidence", "why", "remediation", "verify"):
            if not getattr(self, field_name):
                raise ValueError(f"finding {field_name} must not be empty")

    def sort_key(self) -> tuple[float, int, str]:
        return (-self.priority, -SEVERITY_ORDER[self.severity], self.id)

    def to_dict(self) -> dict[str, Any]:
        return {
            "id": self.id,
            "pillar": self.pillar,
            "severity": self.severity,
            "path": self.path,
            "evidence": self.evidence,
            "why": self.why,
            "remediation": self.remediation,
            "verify": self.verify,
            "priority": round(self.priority, 2),
            "related_paths": list(self.related_paths),
        }


@dataclass
class Pillar:
    id: str
    title: str
    weight: float
    score: float
    intent: str
    metrics: dict[str, Any] = field(default_factory=dict)
    findings: list[Finding] = field(default_factory=list)

    def __post_init__(self) -> None:
        self.score = clamp(float(self.score))
        if self.weight <= 0:
            raise ValueError("pillar weight must be positive")
        if any(finding.pillar != self.id for finding in self.findings):
            raise ValueError(f"finding assigned to the wrong pillar: {self.id}")

    def to_dict(self) -> dict[str, Any]:
        return {
            "id": self.id,
            "title": self.title,
            "weight": self.weight,
            "score": round(self.score, 1),
            "intent": self.intent,
            "metrics": self.metrics,
            "findings": [finding.id for finding in sorted(self.findings, key=Finding.sort_key)],
        }


@dataclass
class Report:
    profile: str
    revision: str
    pillars: list[Pillar]
    evidence: list[Evidence]
    readiness: dict[str, bool | None]
    interventions: list[dict[str, Any]] = field(default_factory=list)

    def __post_init__(self) -> None:
        ids = [pillar.id for pillar in self.pillars]
        if len(ids) != len(set(ids)):
            raise ValueError("pillar ids must be unique")
        finding_ids = [finding.id for pillar in self.pillars for finding in pillar.findings]
        if len(finding_ids) != len(set(finding_ids)):
            raise ValueError("finding ids must be unique")
        total_weight = sum(pillar.weight for pillar in self.pillars)
        if not math.isclose(total_weight, 100.0, abs_tol=0.01):
            raise ValueError(f"pillar weights must total 100, got {total_weight}")

    @property
    def overall(self) -> float:
        return sum(pillar.score * pillar.weight for pillar in self.pillars) / 100.0

    @property
    def confidence(self) -> float:
        applicable = [item for item in self.evidence if item.status != "not_applicable"]
        if not applicable:
            return 0.0
        measured = sum(item.status == "measured" for item in applicable)
        return 100.0 * measured / len(applicable)

    @property
    def healthy(self) -> bool:
        required_missing = any(item.required and item.status != "measured" for item in self.evidence)
        return (
            not required_missing
            and bool(self.readiness)
            and all(state is True for state in self.readiness.values())
        )

    @property
    def findings(self) -> list[Finding]:
        return sorted(
            (finding for pillar in self.pillars for finding in pillar.findings),
            key=Finding.sort_key,
        )

    def to_dict(self) -> dict[str, Any]:
        return {
            "schema_version": SCHEMA_VERSION,
            "rubric_version": RUBRIC_VERSION,
            "profile": self.profile,
            "revision": self.revision,
            "score": {
                "overall": round(self.overall, 1),
                "confidence": round(self.confidence, 1),
                "healthy": self.healthy,
            },
            "pillars": [pillar.to_dict() for pillar in self.pillars],
            "findings": [finding.to_dict() for finding in self.findings],
            "interventions": self.interventions,
            "readiness": self.readiness,
            "evidence_status": [item.to_dict() for item in self.evidence],
        }
