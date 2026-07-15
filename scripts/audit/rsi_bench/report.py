"""Human/markdown rendering for RSI Bench."""

from __future__ import annotations

from .model import RUBRIC_VERSION, Report


def render_human(report: Report, *, top: int = 5) -> str:
    lines = [
        f"RSI Bench  overall = {report.overall:.1f}/100  "
        f"(rubric {RUBRIC_VERSION}, profile {report.profile})",
        f"revision {report.revision}",
        "",
        "Domains",
    ]
    for domain in report.domains:
        flag = "" if domain.ratcheted else " [advisory]"
        lines.append(f"  {domain.id:<12} {domain.score:5.1f}  (w={domain.weight:.2f}){flag}")
        for metric in sorted(domain.metrics, key=lambda m: m.score):
            lines.append(f"    - {metric.id:<22} {metric.score:5.1f}  (w={metric.weight})")
    lines.append("")
    findings = report.findings[:top]
    if findings:
        lines.append(f"Top interventions ({len(findings)}/{len(report.findings)})")
        for finding in findings:
            lines.append(f"  [{finding.severity}] {finding.id}")
            lines.append(f"    What: {finding.evidence}")
            lines.append(f"    Why:  {finding.why}")
            lines.append(f"    Where:{finding.path}")
            lines.append(f"    How:  {finding.remediation}")
            lines.append(f"    Verify: {finding.verify}")
    else:
        lines.append("No findings.")
    lines.append("")
    return "\n".join(lines) + "\n"


def render_markdown(report: Report, *, top: int = 5) -> str:
    lines = [
        f"# RSI Bench — {report.overall:.1f}/100",
        "",
        f"- rubric: `{RUBRIC_VERSION}`",
        f"- profile: `{report.profile}`",
        f"- revision: `{report.revision}`",
        "",
        "## Domains",
        "",
        "| Domain | Score | Weight | Ratchet |",
        "|---|---:|---:|---|",
    ]
    for domain in report.domains:
        lines.append(
            f"| {domain.id} | {domain.score:.1f} | {domain.weight:.2f} | "
            f"{'yes' if domain.ratcheted else 'advisory'} |"
        )
    lines.extend(["", "## Metrics", ""])
    for domain in report.domains:
        lines.append(f"### {domain.title}")
        lines.append("")
        for metric in domain.metrics:
            lines.append(f"- **{metric.id}** `{metric.score:.1f}` (w={metric.weight}) — {metric.intent}")
        lines.append("")
    findings = report.findings[:top]
    if findings:
        lines.append("## Top interventions")
        lines.append("")
        for finding in findings:
            lines.append(f"### {finding.id}")
            lines.append("")
            lines.append(f"- **What:** {finding.evidence}")
            lines.append(f"- **Why:** {finding.why}")
            lines.append(f"- **Where:** `{finding.path}`")
            lines.append(f"- **How:** {finding.remediation}")
            lines.append(f"- **Verify:** `{finding.verify}`")
            lines.append("")
    return "\n".join(lines)
