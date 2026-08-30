#!/usr/bin/env python3
"""design-lint.py — mechanical checks for the ADR 0007 design principles.

docs/adr/0007-design-north-star.md says a principle has to be *judgeable*, because
the ones that were not got ignored: the settings tree used the heaviest type token
as a section header in eleven places and every one of them passed review. Nothing
was wrong with the reviewers — there was no rule to point at.

This file is where a principle becomes a rule. It is deliberately small: only
principles that survive contact with a regex live here, and the rest stay judged by
eye against the preview goldens.

    scripts/dev/design-lint.py          # the gate (also `make design-lint`)

Entry points: main, rule_scaffolding_outweighs_content.
Verify: scripts/dev/design-lint.py
Boundary: this owns *mechanical* design rules for client-android only. Component
choice and verification procedure live in docs/agent-rules/native-design-system.md;
the principles themselves live in the ADR and win when they disagree.
"""

from __future__ import annotations

from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
SETTINGS_TREE = REPO / "client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/deneb"

# Principle 1 forbids scaffolding that outweighs content. Across a whole screen that
# is a judgement call, but inside the settings tree it collapses to one fact: the
# content IS the controls, so an 18sp/600 heading can only ever be a label shouting
# over the thing it labels.
HEAVY_TOKEN = "DenebType.cardTitle"
SETTINGS_FILES = ("Config", "DenebConfigScreen")


def rule_scaffolding_outweighs_content() -> list[str]:
    """Principle 1 — the heaviest type token must not label a settings screen."""
    hits: list[str] = []
    for path in sorted(SETTINGS_TREE.glob("*.kt")):
        if not path.name.startswith(SETTINGS_FILES):
            continue
        for n, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            if HEAVY_TOKEN in line:
                hits.append(f"{path.relative_to(REPO)}:{n}")
    return hits


def main() -> int:
    hits = rule_scaffolding_outweighs_content()
    if not hits:
        print("design-lint: ADR 0007 원리 위반 없음")
        return 0

    print(f"design-lint: 원리 1 위반 {len(hits)}건 — 비계가 콘텐츠보다 무겁다\n")
    for h in hits:
        print(f"  {h}")
    print(
        f"\n{HEAVY_TOKEN}(18sp/600)은 화면에서 가장 무거운 활자다. 설정 화면의 콘텐츠는"
        "\n컨트롤이므로, 그걸 붙인 제목은 자기가 이름 붙인 대상보다 커진다."
        "\n\n  섹션 헤더 → DenebSectionLabel(\"…\")   (12sp 트랙트, 컨테이너 바깥)"
        "\n  행 제목   → DenebType.rowTitle / rowTitleStrong"
        "\n\n근거: docs/adr/0007-design-north-star.md 원리 1"
    )
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
