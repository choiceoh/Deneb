"""Baseline persistence and regression policy for Health Bench 2.0.

The baseline is a ratchet, not an allow-list for making a failing run green.
Updates therefore reject any score decrease and any newly introduced high-risk
finding before replacing the checked-in snapshot.
"""

from __future__ import annotations

import json
import math
import os
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Mapping

from .model import Report, RUBRIC_VERSION, SCHEMA_VERSION, SEVERITY_ORDER

OVERALL_TOLERANCE = 0.3
PILLAR_TOLERANCE = 1.0
BLOCKING_SEVERITIES = frozenset({"high", "critical"})
# --check only fails closed on *critical* findings. New "high" tips from the
# rolling git window were false-reding unrelated PRs without a product bug.
# Baseline *update* still rejects new high+critical (BLOCKING_SEVERITIES).
CHECK_FINDING_SEVERITIES = frozenset({"critical"})
_EPSILON = 1e-9

KEEP_POLICY = (
    "Health Bench 2.0 baseline. This is a one-way ratchet: update only after a "
    "genuine improvement. Never lower overall or pillar floors, or accept a new "
    "high/critical finding, merely to make --check pass."
)


class BaselineError(ValueError):
    """The baseline cannot be loaded or compared safely."""


class BaselineRegressionError(BaselineError):
    """An attempted baseline update would accept a regression."""

    def __init__(self, regressions: tuple[str, ...] | list[str]) -> None:
        self.regressions = tuple(regressions)
        super().__init__("; ".join(self.regressions))


@dataclass(frozen=True)
class CheckResult:
    """Structured result of comparing a report with an accepted baseline."""

    regressions: tuple[str, ...] = ()
    new_findings: tuple[str, ...] = ()

    @property
    def ok(self) -> bool:
        return not self.regressions

    @property
    def errors(self) -> tuple[str, ...]:
        """Alias useful to CLI callers that treat every regression as an error."""
        return self.regressions

    def __bool__(self) -> bool:
        return self.ok

    def format_lines(self) -> list[str]:
        if self.ok:
            return ["Health Bench 2.0 baseline check passed."]
        return [f"REGRESSION: {message}" for message in self.regressions]


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
            f"baseline schema_version={data.get('schema_version')!r} does not match "
            f"{SCHEMA_VERSION}; migrate the baseline explicitly"
        )
    if data.get("rubric_version") != RUBRIC_VERSION:
        raise BaselineError(
            f"baseline rubric_version={data.get('rubric_version')!r} does not match "
            f"{RUBRIC_VERSION}; rebaseline only as an explicit rubric migration"
        )

    for field_name in ("profile", "revision"):
        if not isinstance(data.get(field_name), str) or not data[field_name].strip():
            raise BaselineError(f"baseline {field_name} must be a non-empty string")
    _number(data.get("overall"), "overall")

    pillars = data.get("pillars")
    if not isinstance(pillars, dict) or not pillars:
        raise BaselineError("baseline pillars must be a non-empty object")
    for pillar_id, score in pillars.items():
        if not isinstance(pillar_id, str) or not pillar_id:
            raise BaselineError("baseline pillar ids must be non-empty strings")
        _number(score, f"pillars.{pillar_id}")

    high_findings = data.get("high_findings")
    if not isinstance(high_findings, dict):
        raise BaselineError("baseline high_findings must be an object")
    for finding_id, severity in high_findings.items():
        if not isinstance(finding_id, str) or not finding_id:
            raise BaselineError("baseline finding ids must be non-empty strings")
        if severity not in BLOCKING_SEVERITIES:
            raise BaselineError(
                f"baseline high_findings.{finding_id} has non-blocking severity {severity!r}"
            )

    tolerances = data.get("tolerances")
    if tolerances is not None:
        if not isinstance(tolerances, dict):
            raise BaselineError("baseline tolerances must be an object")
        overall = tolerances.get("overall")
        pillar = tolerances.get("pillar")
        if overall != OVERALL_TOLERANCE or pillar != PILLAR_TOLERANCE:
            raise BaselineError(
                "baseline tolerances do not match the rubric; tolerances are code policy, "
                "not baseline-controlled escape hatches"
            )
    return data


def load(path: str | Path) -> dict[str, Any]:
    """Load and validate a Health Bench 2.0 baseline."""
    baseline_path = Path(path)
    try:
        data = json.loads(baseline_path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise BaselineError(f"baseline not found: {baseline_path}") from exc
    except OSError as exc:
        raise BaselineError(f"baseline unreadable: {baseline_path}: {exc}") from exc
    except json.JSONDecodeError as exc:
        raise BaselineError(f"baseline is not valid JSON: {baseline_path}: {exc}") from exc
    return _validate(data)


def _report_scores(report: Report) -> tuple[float, dict[str, float]]:
    return (
        round(report.overall, 1),
        {pillar.id: round(pillar.score, 1) for pillar in report.pillars},
    )


def _blocking_findings(
    report: Report, *, severities: frozenset[str] | None = None
) -> dict[str, str]:
    allowed = BLOCKING_SEVERITIES if severities is None else severities
    return {
        finding.id: finding.severity
        for finding in report.findings
        if finding.severity in allowed
    }


def _ensure_compatible(report: Report, baseline: Mapping[str, Any]) -> None:
    _validate(dict(baseline))
    if report.profile != baseline["profile"]:
        raise BaselineError(
            f"report profile={report.profile!r} does not match baseline "
            f"profile={baseline['profile']!r}"
        )
    report_ids = {pillar.id for pillar in report.pillars}
    baseline_ids = set(baseline["pillars"])
    if report_ids != baseline_ids:
        missing = sorted(baseline_ids - report_ids)
        added = sorted(report_ids - baseline_ids)
        parts = []
        if missing:
            parts.append(f"missing pillars: {', '.join(missing)}")
        if added:
            parts.append(f"unbaselined pillars: {', '.join(added)}")
        raise BaselineError(
            "pillar set changed without a rubric version migration (" + "; ".join(parts) + ")"
        )


def _new_or_escalated_findings(
    report: Report,
    baseline: Mapping[str, Any],
    *,
    severities: frozenset[str] | None = None,
) -> tuple[list[str], list[str]]:
    accepted = baseline["high_findings"]
    finding_ids: list[str] = []
    messages: list[str] = []
    by_id = {finding.id: finding for finding in report.findings}
    for finding_id, severity in sorted(_blocking_findings(report, severities=severities).items()):
        old_severity = accepted.get(finding_id)
        finding = by_id[finding_id]
        if old_severity is None:
            finding_ids.append(finding_id)
            messages.append(
                f"new {severity} finding {finding_id} at {finding.path}: {finding.evidence}"
            )
        elif SEVERITY_ORDER[severity] > SEVERITY_ORDER[old_severity]:
            finding_ids.append(finding_id)
            messages.append(
                f"finding {finding_id} escalated from {old_severity} to {severity} at "
                f"{finding.path}"
            )
    return finding_ids, messages


def check(
    report: Report,
    baseline: Mapping[str, Any],
) -> CheckResult:
    """Compare ``report`` with ``baseline`` using the fixed regression policy."""
    _ensure_compatible(report, baseline)
    overall, pillar_scores = _report_scores(report)
    regressions: list[str] = []

    baseline_overall = _number(baseline["overall"], "overall")
    if overall < baseline_overall - OVERALL_TOLERANCE - _EPSILON:
        regressions.append(
            f"overall score {baseline_overall:.1f} -> {overall:.1f} "
            f"(allowed drop {OVERALL_TOLERANCE:.1f})"
        )

    for pillar_id in sorted(pillar_scores):
        old_score = _number(baseline["pillars"][pillar_id], f"pillars.{pillar_id}")
        new_score = pillar_scores[pillar_id]
        if new_score < old_score - PILLAR_TOLERANCE - _EPSILON:
            regressions.append(
                f"pillar {pillar_id} {old_score:.1f} -> {new_score:.1f} "
                f"(allowed drop {PILLAR_TOLERANCE:.1f})"
            )

    finding_ids, finding_messages = _new_or_escalated_findings(
        report, baseline, severities=CHECK_FINDING_SEVERITIES
    )
    regressions.extend(finding_messages)
    return CheckResult(tuple(regressions), tuple(finding_ids))


def snapshot(report: Report, *, provenance: Mapping[str, Any] | None = None) -> dict[str, Any]:
    """Build the canonical baseline payload for ``report`` without writing it."""
    overall, pillars = _report_scores(report)
    payload: dict[str, Any] = {
        "_keep_policy": KEEP_POLICY,
        "schema_version": SCHEMA_VERSION,
        "rubric_version": RUBRIC_VERSION,
        "profile": report.profile,
        "revision": report.revision,
        "overall": overall,
        "pillars": dict(sorted(pillars.items())),
        "high_findings": dict(sorted(_blocking_findings(report).items())),
        "tolerances": {
            "overall": OVERALL_TOLERANCE,
            "pillar": PILLAR_TOLERANCE,
        },
    }
    if provenance:
        payload["provenance"] = dict(provenance)
    return payload


def _strict_update_regressions(
    report: Report, baseline: Mapping[str, Any]
) -> tuple[str, ...]:
    _ensure_compatible(report, baseline)
    overall, pillar_scores = _report_scores(report)
    regressions: list[str] = []
    old_overall = _number(baseline["overall"], "overall")
    if overall < old_overall - _EPSILON:
        regressions.append(f"cannot lower overall baseline {old_overall:.1f} -> {overall:.1f}")
    for pillar_id in sorted(pillar_scores):
        old_score = _number(baseline["pillars"][pillar_id], f"pillars.{pillar_id}")
        new_score = pillar_scores[pillar_id]
        if new_score < old_score - _EPSILON:
            regressions.append(
                f"cannot lower pillar baseline {pillar_id} {old_score:.1f} -> {new_score:.1f}"
            )
    _, finding_messages = _new_or_escalated_findings(report, baseline)
    regressions.extend(f"cannot accept {message}" for message in finding_messages)
    return tuple(regressions)


def _write(path: Path, payload: Mapping[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary: str | None = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w",
            encoding="utf-8",
            dir=path.parent,
            prefix=f".{path.name}.",
            suffix=".tmp",
            delete=False,
        ) as handle:
            temporary = handle.name
            json.dump(payload, handle, indent=2, ensure_ascii=False)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
        temporary = None
    except OSError as exc:
        raise BaselineError(f"could not write baseline {path}: {exc}") from exc
    finally:
        if temporary is not None:
            try:
                os.unlink(temporary)
            except OSError:
                pass


def update(
    path: str | Path,
    report: Report,
    *,
    provenance: Mapping[str, Any] | None = None,
) -> dict[str, Any]:
    """Create or monotonically raise a baseline and return its payload."""
    baseline_path = Path(path)
    current: dict[str, Any] | None = None
    if baseline_path.exists():
        current = load(baseline_path)
        regressions = _strict_update_regressions(report, current)
        if regressions:
            raise BaselineRegressionError(regressions)
    if provenance is None and current is not None:
        existing_provenance = current.get("provenance")
        if isinstance(existing_provenance, dict):
            provenance = existing_provenance
    payload = snapshot(report, provenance=provenance)
    _write(baseline_path, payload)
    return payload


def _migration_source(source: str | Path | Mapping[str, Any]) -> dict[str, Any]:
    if isinstance(source, Mapping):
        payload = dict(source)
    else:
        source_path = Path(source)
        try:
            payload = json.loads(source_path.read_text(encoding="utf-8"))
        except FileNotFoundError as exc:
            raise BaselineError(f"migration source not found: {source_path}") from exc
        except OSError as exc:
            raise BaselineError(f"migration source unreadable: {source_path}: {exc}") from exc
        except json.JSONDecodeError as exc:
            raise BaselineError(f"migration source is not valid JSON: {source_path}: {exc}") from exc
    if not isinstance(payload, dict):
        raise BaselineError("migration source root must be a JSON object")
    if payload.get("schema_version") != SCHEMA_VERSION:
        raise BaselineError(
            f"migration source schema_version={payload.get('schema_version')!r} does not match {SCHEMA_VERSION}"
        )
    previous_rubric = payload.get("rubric_version")
    if not isinstance(previous_rubric, str) or not previous_rubric:
        raise BaselineError("migration source rubric_version must be a non-empty string")
    if previous_rubric == RUBRIC_VERSION:
        raise BaselineError(
            f"migration source already uses rubric {RUBRIC_VERSION}; use the one-way baseline update instead"
        )
    if not isinstance(payload.get("profile"), str) or not payload["profile"]:
        raise BaselineError("migration source profile must be a non-empty string")
    _number(payload.get("overall"), "migration source overall")
    return payload


def migrate(
    path: str | Path,
    report: Report,
    source: str | Path | Mapping[str, Any],
    *,
    provenance: Mapping[str, Any] | None = None,
) -> dict[str, Any]:
    """Remeasure and replace a baseline after an explicit rubric change.

    Scores are intentionally not compared across rubrics. The previous rubric
    and score are retained as provenance so a migration cannot masquerade as a
    monotonic quality improvement.
    """
    previous = _migration_source(source)
    if previous["profile"] != report.profile:
        raise BaselineError(
            f"report profile={report.profile!r} does not match migration source "
            f"profile={previous['profile']!r}"
        )
    migration: dict[str, Any] = dict(provenance or {})
    migration.update({
        "migration": "rubric-remeasurement",
        "previous_rubric": previous["rubric_version"],
        "previous_score": _number(previous["overall"], "migration source overall"),
        "note": "Scores are not converted or directly comparable across rubric versions.",
    })
    if isinstance(previous.get("provenance"), dict):
        migration["previous_provenance"] = dict(previous["provenance"])
    payload = snapshot(report, provenance=migration)
    _write(Path(path), payload)
    return payload


# Explicit aliases make call sites self-documenting while retaining the compact
# load/check/update API used by the CLI.
load_baseline = load
check_baseline = check
update_baseline = update
migrate_baseline = migrate
