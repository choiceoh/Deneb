"""Action-oriented human and Markdown reports for Health Bench 2.0."""

from __future__ import annotations

import re
from typing import Any, Iterable

from .model import Finding, Report, SEVERITY_ORDER, stable_id


def _one_line(value: object) -> str:
    return " ".join(str(value).split())


def _unique(values: Iterable[str]) -> list[str]:
    return list(dict.fromkeys(value for value in values if value))


def _summary(values: Iterable[str], *, limit: int = 2) -> str:
    unique = _unique(_one_line(value) for value in values)
    if not unique:
        return "No explanation supplied."
    rendered = "; ".join(unique[:limit])
    if len(unique) > limit:
        rendered += f" (+{len(unique) - limit} related signals)"
    return rendered


def _first_sentence(value: str, *, limit: int = 96) -> str:
    text = _one_line(value)
    sentence = re.split(r"(?<=[.!?])\s+", text, maxsplit=1)[0]
    if len(sentence) <= limit:
        return sentence
    return sentence[: limit - 1].rstrip() + "…"


def _intervention_sort_key(item: dict[str, Any]) -> tuple[float, int, str]:
    severity = str(item.get("severity", "info"))
    return (
        -float(item.get("priority", 0.0)),
        -SEVERITY_ORDER.get(severity, 0),
        str(item.get("id", "")),
    )


def group_interventions(report: Report) -> list[dict[str, Any]]:
    """Group findings that prescribe the same root-cause change.

    Grouping by pillar, remediation, and verification prevents repeated symptoms
    from turning into a noisy checklist while keeping unrelated fixes separate.
    Priority uses the maximum risk, not the sum, so duplicate findings cannot
    inflate an intervention's rank.
    """
    groups: dict[tuple[str, str, str], list[Finding]] = {}
    for finding in report.findings:
        key = (
            finding.pillar,
            _one_line(finding.remediation).casefold(),
            _one_line(finding.verify).casefold(),
        )
        groups.setdefault(key, []).append(finding)

    interventions: list[dict[str, Any]] = []
    for (pillar, _, _), findings in sorted(groups.items()):
        ordered = sorted(findings, key=Finding.sort_key)
        lead = ordered[0]
        severity = max(
            (finding.severity for finding in ordered),
            key=lambda value: SEVERITY_ORDER[value],
        )
        paths = sorted(
            {
                path
                for finding in ordered
                for path in (finding.path, *finding.related_paths)
                if path
            }
        )
        what = _summary((finding.evidence for finding in ordered), limit=2)
        why = _summary((finding.why for finding in ordered), limit=2)
        how = _one_line(lead.remediation)
        verify = _one_line(lead.verify)
        intervention_id = stable_id("intervention", pillar, how.casefold(), verify.casefold())
        interventions.append(
            {
                "id": intervention_id,
                "pillar": pillar,
                "severity": severity,
                "priority": round(max(finding.priority for finding in ordered), 2),
                "title": _first_sentence(how),
                "what": what,
                "why": why,
                "where": paths,
                "how": how,
                "verify": verify,
                "findings": [finding.id for finding in ordered],
            }
        )
    return sorted(interventions, key=_intervention_sort_key)


def _prepared_interventions(report: Report) -> list[dict[str, Any]]:
    if report.interventions:
        return sorted((dict(item) for item in report.interventions), key=_intervention_sort_key)
    return group_interventions(report)


def _where(item: dict[str, Any], *, limit: int = 4) -> str:
    raw = item.get("where", [])
    paths = [str(path) for path in raw] if isinstance(raw, (list, tuple)) else [str(raw)]
    paths = _unique(paths)
    if not paths:
        return "repository-wide"
    result = ", ".join(paths[:limit])
    if len(paths) > limit:
        result += f" (+{len(paths) - limit} more)"
    return result


def _field(item: dict[str, Any], key: str, fallback: str) -> str:
    value = item.get(key)
    return _one_line(value) if value else fallback


def _validate_top(top: int) -> None:
    if top < 0:
        raise ValueError("top must not be negative")


def render_human(report: Report, top: int = 5) -> str:
    """Render a terminal report with fixes before score detail."""
    _validate_top(top)
    interventions = _prepared_interventions(report)[:top]
    lines = ["Health Bench 2.0 — top fixes"]
    if not interventions:
        lines.append("  No actionable findings.")
    for index, item in enumerate(interventions, start=1):
        severity = str(item.get("severity", "info")).upper()
        title = _field(item, "title", _field(item, "what", "Fix finding"))
        lines.extend(
            [
                f"{index}. [{severity}] {title}",
                f"   What:  {_field(item, 'what', title)}",
                f"   Why:   {_field(item, 'why', 'Reduces maintenance and change risk.')}",
                f"   Where: {_where(item)}",
                f"   How:   {_field(item, 'how', _field(item, 'title', 'Address the root cause.'))}",
                f"   Verify: {_field(item, 'verify', 'Re-run Health Bench 2.0.')}",
            ]
        )

    lines.extend(
        [
            "",
            f"Score: {report.overall:.1f}/100  profile={report.profile}  "
            f"confidence={report.confidence:.1f}%  revision={report.revision}",
            "Pillars:",
        ]
    )
    for pillar in sorted(report.pillars, key=lambda value: value.id):
        lines.append(
            f"  {pillar.title} ({pillar.id}): {pillar.score:.1f}  weight={pillar.weight:g}"
        )

    readiness = [
        f"{name}={'unknown' if state is None else 'pass' if state else 'fail'}"
        for name, state in sorted(report.readiness.items())
    ]
    if readiness:
        lines.append("Readiness: " + ", ".join(readiness))
    unavailable = [
        evidence
        for evidence in report.evidence
        if evidence.status == "unavailable"
    ]
    if unavailable:
        lines.append("Evidence gaps:")
        for evidence in sorted(unavailable, key=lambda value: value.name):
            required = "required" if evidence.required else "optional"
            lines.append(f"  {evidence.name} ({required}): {_one_line(evidence.detail)}")
    return "\n".join(lines) + "\n"


def _markdown_text(value: str) -> str:
    return _one_line(value).replace("|", "\\|")


def render_markdown(report: Report, top: int = 5) -> str:
    """Render a GitHub Step Summary with fixes before the scorecard."""
    _validate_top(top)
    interventions = _prepared_interventions(report)[:top]
    lines = ["## Top fixes"]
    if not interventions:
        lines.append("No actionable findings.")
    for index, item in enumerate(interventions, start=1):
        severity = str(item.get("severity", "info")).upper()
        title = _markdown_text(_field(item, "title", _field(item, "what", "Fix finding")))
        what = _markdown_text(_field(item, "what", title))
        lines.extend(
            [
                f"### {index}. {severity}: {title}",
                f"- **What:** {what}",
                f"- **Why:** {_markdown_text(_field(item, 'why', 'Reduces maintenance and change risk.'))}",
                f"- **Where:** `{_markdown_text(_where(item))}`",
                f"- **How:** {_markdown_text(_field(item, 'how', _field(item, 'title', 'Address the root cause.')))}",
                f"- **Verify:** `{_markdown_text(_field(item, 'verify', 'Re-run Health Bench 2.0.'))}`",
            ]
        )

    lines.extend(
        [
            "",
            "## Score",
            f"**{report.overall:.1f}/100** · profile `{report.profile}` · "
            f"confidence {report.confidence:.1f}% · revision `{report.revision}`",
            "",
            "| Pillar | Score | Weight |",
            "|---|---:|---:|",
        ]
    )
    for pillar in sorted(report.pillars, key=lambda value: value.id):
        lines.append(
            f"| {_markdown_text(pillar.title)} | {pillar.score:.1f} | {pillar.weight:g} |"
        )

    if report.readiness:
        lines.extend(["", "## Readiness"])
        for name, state in sorted(report.readiness.items()):
            rendered = "unknown" if state is None else "pass" if state else "fail"
            lines.append(f"- `{name}`: **{rendered}**")
    unavailable = sorted(
        (evidence for evidence in report.evidence if evidence.status == "unavailable"),
        key=lambda value: value.name,
    )
    if unavailable:
        lines.extend(["", "## Evidence gaps"])
        for evidence in unavailable:
            required = "required" if evidence.required else "optional"
            lines.append(
                f"- `{evidence.name}` ({required}): {_markdown_text(evidence.detail)}"
            )
    return "\n".join(lines) + "\n"
