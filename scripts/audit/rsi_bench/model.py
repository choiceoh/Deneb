"""Stable report model and composite helpers for RSI Bench 1.2."""

from __future__ import annotations

import hashlib
import math
from dataclasses import dataclass, field
from typing import Any, Iterable

SCHEMA_VERSION = 1
RUBRIC_VERSION = "1.3.0"
# Soft-confirm from evolve watches may score rates, but never above this ceiling —
# lifecycle confirm/rollback remains the hard oracle (PACE).
SOFT_RESOLVE_SCORE_CAP = 55.0
# Run-to-run byClass rate tolerance for BabelJudge swap-consistency proxy.
SWAP_RATE_TOLERANCE = 0.05
# Proxy ceilings until literal BabelJudge KO/EN swap / EvoAgentBench ability-graph land.
SWAP_CONSISTENCY_PROXY_CAP = 58.0
SWAP_CONSISTENCY_SATURATED_CAP = 52.0
ABILITY_TRANSFER_PROXY_CAP = 58.0
# --check refuses to ratchet when measured evidence share is too thin.
MIN_CHECK_CONFIDENCE = 60.0
SEVERITY_ORDER = {"critical": 4, "high": 3, "medium": 2, "low": 1, "info": 0}

DOMAIN_WEIGHTS = {
    "process": 0.55,
    "utility": 0.45,
}

MIN_RESOLVED_FOR_HARD = 3
UNMEASURED_RATE_FLOOR = 28.0


def clamp(value: float, low: float = 0.0, high: float = 100.0) -> float:
    return max(low, min(high, value))


def geometric_composite(
    domain_scores: dict[str, float], weights: dict[str, float] = DOMAIN_WEIGHTS
) -> float:
    if set(domain_scores) != set(weights):
        raise ValueError(f"domain keys mismatch: {sorted(domain_scores)} vs {sorted(weights)}")
    weight_sum = sum(weights.values())
    if not math.isclose(weight_sum, 1.0, abs_tol=1e-9):
        raise ValueError(f"domain weights must sum to 1.0, got {weight_sum}")
    for name, score in domain_scores.items():
        if score <= 0.0:
            return 0.0
        if not math.isfinite(score):
            raise ValueError(f"domain {name} score must be finite")
    log_sum = sum(weights[name] * math.log(clamp(domain_scores[name])) for name in weights)
    return round(math.exp(log_sum), 1)


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
        if self.status not in {"measured", "unavailable", "not_applicable", "bootstrap"}:
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
    domain: str
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
            raise ValueError(f"finding priority out of range: {self.priority}")
        for field_name in ("id", "domain", "pillar", "path", "evidence", "why", "remediation", "verify"):
            if not getattr(self, field_name):
                raise ValueError(f"finding {field_name} must not be empty")

    def sort_key(self) -> tuple[float, int, str]:
        return (-self.priority, -SEVERITY_ORDER[self.severity], self.id)

    def to_dict(self) -> dict[str, Any]:
        return {
            "id": self.id,
            "domain": self.domain,
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
class Metric:
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
            raise ValueError("metric weight must be positive")

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
class Domain:
    id: str
    title: str
    weight: float
    metrics: list[Metric]
    evidence: list[Evidence] = field(default_factory=list)
    ratcheted: bool = True

    def __post_init__(self) -> None:
        if self.id not in DOMAIN_WEIGHTS:
            raise ValueError(f"unknown domain id: {self.id}")
        if not math.isclose(self.weight, DOMAIN_WEIGHTS[self.id], abs_tol=1e-9):
            raise ValueError(f"domain {self.id} weight must be {DOMAIN_WEIGHTS[self.id]}")
        total = sum(metric.weight for metric in self.metrics)
        if not math.isclose(total, 100.0, abs_tol=0.01):
            raise ValueError(f"domain {self.id} metric weights must total 100, got {total}")

    @property
    def score(self) -> float:
        return round(sum(m.score * m.weight for m in self.metrics) / 100.0, 1)

    @property
    def findings(self) -> list[Finding]:
        return sorted(
            (finding for metric in self.metrics for finding in metric.findings),
            key=Finding.sort_key,
        )

    def to_dict(self) -> dict[str, Any]:
        return {
            "id": self.id,
            "title": self.title,
            "weight": self.weight,
            "score": self.score,
            "ratcheted": self.ratcheted,
            "metrics": [metric.to_dict() for metric in self.metrics],
            "findings": [finding.to_dict() for finding in self.findings],
            "evidence_status": [item.to_dict() for item in self.evidence],
        }


@dataclass
class Report:
    profile: str
    revision: str
    domains: list[Domain]
    evidence: list[Evidence] = field(default_factory=list)
    readiness: dict[str, bool | None] = field(default_factory=dict)

    def __post_init__(self) -> None:
        ids = [domain.id for domain in self.domains]
        if sorted(ids) != sorted(DOMAIN_WEIGHTS):
            raise ValueError(f"report must include all domains, got {ids}")
        finding_ids = [f.id for domain in self.domains for f in domain.findings]
        if len(finding_ids) != len(set(finding_ids)):
            raise ValueError("finding ids must be unique")

    @property
    def overall(self) -> float:
        scores = {domain.id: domain.score for domain in self.domains}
        return geometric_composite(scores)

    @property
    def confidence(self) -> float:
        applicable = [item for item in self.evidence if item.status not in {"not_applicable"}]
        if not applicable:
            return 0.0
        measured = sum(item.status == "measured" for item in applicable)
        return round(100.0 * measured / len(applicable), 1)

    @property
    def findings(self) -> list[Finding]:
        return sorted(
            (finding for domain in self.domains for finding in domain.findings),
            key=Finding.sort_key,
        )

    def pillar_scores(self) -> dict[str, float]:
        out: dict[str, float] = {}
        for domain in self.domains:
            for metric in domain.metrics:
                out[f"{domain.id}.{metric.id}"] = round(metric.score, 1)
        return out

    def to_dict(self) -> dict[str, Any]:
        return {
            "schema_version": SCHEMA_VERSION,
            "rubric_version": RUBRIC_VERSION,
            "profile": self.profile,
            "revision": self.revision,
            "score": {
                "overall": self.overall,
                "confidence": self.confidence,
                "domains": {domain.id: domain.score for domain in self.domains},
            },
            "domains": [domain.to_dict() for domain in self.domains],
            "findings": [finding.to_dict() for finding in self.findings],
            "readiness": self.readiness,
            "evidence_status": [item.to_dict() for item in self.evidence],
        }


def weighted_mean(pairs: Iterable[tuple[float, float]]) -> float:
    items = list(pairs)
    if not items:
        return 0.0
    total_w = sum(weight for weight, _ in items)
    if total_w <= 0:
        return 0.0
    return sum(weight * score for weight, score in items) / total_w


def grade_rate_high_good(rate: float, soft: float, hard: float) -> float:
    """Map a 'higher is better' rate onto 0–100 using soft/hard floors."""
    if rate >= soft:
        return 100.0
    if rate <= hard:
        return 0.0
    return 100.0 * (rate - hard) / (soft - hard)


def grade_rate_low_good(rate: float, soft: float, hard: float) -> float:
    """Map a 'lower is better' rate onto 0–100."""
    if rate <= soft:
        return 100.0
    if rate >= hard:
        return 0.0
    return 100.0 * (hard - rate) / (hard - soft)
