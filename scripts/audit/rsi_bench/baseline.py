"""Baseline persistence and regression policy for RSI Bench 1.2."""

from __future__ import annotations

import json
import math
import os
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from .model import DOMAIN_WEIGHTS, MIN_CHECK_CONFIDENCE, RUBRIC_VERSION, SCHEMA_VERSION, SEVERITY_ORDER, Report

OVERALL_TOLERANCE = 0.3
PILLAR_TOLERANCE = 1.0
DOMAIN_TOLERANCE = 1.0
BLOCKING_SEVERITIES = frozenset({"high", "critical"})
_EPSILON = 1e-9

KEEP_POLICY = (
    "RSI Bench 1.2 baseline. One-way ratchet across overall and ratcheted Process "
    "and Utility metrics. Never lower floors or accept new high/critical findings "
    "merely to make --check pass."
)


class BaselineError(ValueError):
    """The baseline cannot be loaded or compared safely."""


class BaselineRegressionError(BaselineError):
    def __init__(self, regressions: tuple[str, ...] | list[str]) -> None:
        self.regressions = tuple(regressions)
        super().__init__("; ".join(self.regressions))


@dataclass(frozen=True)
class CheckResult:
    regressions: tuple[str, ...] = ()
    # Floors missed by a pillar whose evidence never resolved. The number is a
    # bootstrap constant, not a measurement, so calling it a regression sends the
    # reader hunting for code that got worse. It still fails the check —
    # starvation is a real fault — but it is named for what it is: on 2026-08-07
    # three unmeasured pillars dropped Process 6.4 points while every measured
    # pillar held or improved.
    unmeasured: tuple[str, ...] = ()

    @property
    def ok(self) -> bool:
        return not self.regressions and not self.unmeasured

    def format_lines(self) -> list[str]:
        if self.ok:
            return ["RSI Bench baseline check passed."]
        lines = [f"REGRESSION: {message}" for message in self.regressions]
        lines.extend(f"UNMEASURED: {message}" for message in self.unmeasured)
        return lines


def _number(value: object, field: str) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise BaselineError(f"baseline {field} must be a number")
    number = float(value)
    if not math.isfinite(number) or not 0.0 <= number <= 100.0:
        raise BaselineError(f"baseline {field} must be between 0 and 100")
    return number


def _validate(data: object) -> dict[str, Any]:
    if not isinstance(data, dict):
        raise BaselineError("baseline root must be a JSON object")
    if data.get("schema_version") != SCHEMA_VERSION:
        raise BaselineError(
            f"baseline schema_version={data.get('schema_version')!r} != {SCHEMA_VERSION}"
        )
    if data.get("rubric_version") != RUBRIC_VERSION:
        raise BaselineError(
            f"baseline rubric_version={data.get('rubric_version')!r} != {RUBRIC_VERSION}"
        )
    for field_name in ("profile", "revision"):
        if not isinstance(data.get(field_name), str) or not data[field_name].strip():
            raise BaselineError(f"baseline {field_name} must be a non-empty string")
    _number(data.get("overall"), "overall")

    domains = data.get("domains")
    if not isinstance(domains, dict) or set(domains) != set(DOMAIN_WEIGHTS):
        raise BaselineError(f"baseline domains must be {sorted(DOMAIN_WEIGHTS)}")
    for domain_id, score in domains.items():
        _number(score, f"domains.{domain_id}")

    pillars = data.get("pillars")
    if not isinstance(pillars, dict) or not pillars:
        raise BaselineError("baseline pillars must be a non-empty object")
    for pillar_id, score in pillars.items():
        _number(score, f"pillars.{pillar_id}")

    high_findings = data.get("high_findings")
    if not isinstance(high_findings, dict):
        raise BaselineError("baseline high_findings must be an object")
    for finding_id, severity in high_findings.items():
        if severity not in BLOCKING_SEVERITIES:
            raise BaselineError(f"baseline high_findings.{finding_id} invalid severity")

    ratcheted = data.get("ratcheted_domains", ["process", "utility"])
    if not isinstance(ratcheted, list) or not all(isinstance(x, str) for x in ratcheted):
        raise BaselineError("baseline ratcheted_domains must be a string list")
    return data


def load(path: Path) -> dict[str, Any]:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise BaselineError(f"could not read baseline {path}: {exc}") from exc
    return _validate(payload)


def _high_findings(report: Report) -> dict[str, str]:
    return {
        finding.id: finding.severity
        for finding in report.findings
        if finding.severity in BLOCKING_SEVERITIES
    }


def snapshot(report: Report, *, provenance: dict[str, Any] | None = None) -> dict[str, Any]:
    ratcheted = [domain.id for domain in report.domains if domain.ratcheted]
    payload: dict[str, Any] = {
        "_keep_policy": KEEP_POLICY,
        "schema_version": SCHEMA_VERSION,
        "rubric_version": RUBRIC_VERSION,
        "profile": report.profile,
        "revision": report.revision,
        "overall": report.overall,
        "domains": {domain.id: domain.score for domain in report.domains},
        "pillars": report.pillar_scores(),
        "high_findings": _high_findings(report),
        "ratcheted_domains": ratcheted,
        "tolerances": {
            "overall": OVERALL_TOLERANCE,
            "domain": DOMAIN_TOLERANCE,
            "pillar": PILLAR_TOLERANCE,
        },
    }
    if provenance:
        payload["provenance"] = provenance
    return payload


def _atomic_write(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    text = json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False) + "\n"
    fd, tmp_name = tempfile.mkstemp(prefix="rsi-bench-baseline-", dir=str(path.parent))
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(text)
        os.replace(tmp_name, path)
    finally:
        if os.path.exists(tmp_name):
            try:
                os.unlink(tmp_name)
            except OSError:
                pass


def update(
    path: Path,
    report: Report,
    *,
    provenance: dict[str, Any] | None = None,
    migrate_rubric: bool = False,
) -> None:
    payload = snapshot(report, provenance=provenance)
    if path.is_file() and not migrate_rubric:
        current = load(path)
        if current.get("rubric_version") != RUBRIC_VERSION:
            raise BaselineError(
                f"rubric_version {current.get('rubric_version')!r} != {RUBRIC_VERSION}; "
                "pass --migrate-rubric to replace the baseline"
            )
        regressions: list[str] = []
        if report.overall + _EPSILON < float(current["overall"]):
            regressions.append(
                f"overall {report.overall:.1f} < baseline {float(current['overall']):.1f}"
            )
        for domain_id, floor in current["domains"].items():
            domain = next(d for d in report.domains if d.id == domain_id)
            if domain_id in current.get("ratcheted_domains", ["process", "utility"]) and (
                domain.score + _EPSILON < float(floor)
            ):
                regressions.append(
                    f"domain {domain_id} {domain.score:.1f} < baseline {float(floor):.1f}"
                )
        for pillar_id, floor in current["pillars"].items():
            domain_id = pillar_id.split(".", 1)[0]
            if domain_id not in current.get("ratcheted_domains", []):
                continue
            score = report.pillar_scores().get(pillar_id)
            if score is None:
                regressions.append(f"missing pillar {pillar_id}")
            elif score + _EPSILON < float(floor):
                regressions.append(f"pillar {pillar_id} {score:.1f} < baseline {float(floor):.1f}")
        old_high = current.get("high_findings", {})
        new_high = _high_findings(report)
        for finding_id, severity in new_high.items():
            if finding_id not in old_high:
                regressions.append(f"new {severity} finding {finding_id}")
            elif SEVERITY_ORDER[severity] > SEVERITY_ORDER.get(old_high[finding_id], 0):
                regressions.append(f"escalated finding {finding_id}")
        if regressions:
            raise BaselineRegressionError(regressions)
    _atomic_write(path, payload)


def _unresolved_evidence(report: Report) -> dict[str, str]:
    """Pillar id -> why its evidence never resolved.

    Evidence names are dash-joined (`process-acceptor-trust`) while pillar ids are
    dot-joined; normalise so a floor miss can be attributed to a starved sample
    rather than to code that regressed.
    """
    out: dict[str, str] = {}
    for item in report.evidence:
        if item.status not in {"bootstrap", "unavailable"}:
            continue
        out[item.name.replace("-", ".", 1)] = f"{item.status}: {item.detail}"
    return out


def check(report: Report, baseline: dict[str, Any]) -> CheckResult:
    tolerances = baseline.get("tolerances") or {}
    overall_tol = float(tolerances.get("overall", OVERALL_TOLERANCE))
    domain_tol = float(tolerances.get("domain", DOMAIN_TOLERANCE))
    pillar_tol = float(tolerances.get("pillar", PILLAR_TOLERANCE))
    conf_floor = float(tolerances.get("confidence", MIN_CHECK_CONFIDENCE))
    ratcheted = set(baseline.get("ratcheted_domains") or ["process", "utility"])
    regressions: list[str] = []
    unmeasured: list[str] = []
    unresolved = _unresolved_evidence(report)

    if report.confidence + _EPSILON < conf_floor:
        regressions.append(
            f"confidence {report.confidence:.1f} < min {conf_floor:.1f} "
            "(evidence too thin to trust Utility+Process ratchet)"
        )

    if report.overall + overall_tol + _EPSILON < float(baseline["overall"]):
        regressions.append(
            f"overall {report.overall:.1f} < baseline {float(baseline['overall']):.1f} "
            f"(tol {overall_tol})"
        )
    for domain in report.domains:
        if domain.id not in ratcheted:
            continue
        floor = float(baseline["domains"][domain.id])
        if domain.score + domain_tol + _EPSILON < floor:
            starved = sorted(
                pillar for pillar in unresolved if pillar.startswith(f"{domain.id}.")
            )
            message = (
                f"domain {domain.id} {domain.score:.1f} < baseline {floor:.1f} (tol {domain_tol})"
            )
            if starved:
                unmeasured.append(f"{message}; starved pillars: {', '.join(starved)}")
            else:
                regressions.append(message)
    scores = report.pillar_scores()
    for pillar_id, floor in baseline["pillars"].items():
        domain_id = pillar_id.split(".", 1)[0]
        if domain_id not in ratcheted:
            continue
        score = scores.get(pillar_id)
        if score is None:
            regressions.append(f"missing pillar {pillar_id}")
        elif score + pillar_tol + _EPSILON < float(floor):
            message = (
                f"pillar {pillar_id} {score:.1f} < baseline {float(floor):.1f} (tol {pillar_tol})"
            )
            if pillar_id in unresolved:
                unmeasured.append(f"{message} — {unresolved[pillar_id]}")
            else:
                regressions.append(message)

    old_high = baseline.get("high_findings") or {}
    new_high = _high_findings(report)
    for finding_id, severity in new_high.items():
        if finding_id not in old_high:
            regressions.append(f"new {severity} finding {finding_id}")
        elif SEVERITY_ORDER[severity] > SEVERITY_ORDER.get(old_high[finding_id], 0):
            regressions.append(f"escalated finding {finding_id} to {severity}")
    return CheckResult(regressions=tuple(regressions), unmeasured=tuple(unmeasured))
