#!/usr/bin/env python3
"""wiki_title_lint.py — advisory lint for project rep-page titles.

The rule (docs/agent-rules/wiki-layout.md, operator-confirmed 2026-07-21): a
rep title is the project's NAME — events, stage words, and dates are banned;
site/region tokens are part of the name and must be preserved. Since the
folder-code pivot the title is also the human display name everywhere, so a
dirty title is a user-visible wart on top of a search hazard.

Mirrors gateway-go/internal/domain/wiki/title_lint.go (keep the two rule sets
in step). Deterministic, read-only, importable — prints violations and exits 1
when any exist, so it can gate an operator sweep without ever editing pages
(title edits change search ranking; renames stay human decisions).

Usage: scripts/audit/wiki_title_lint.py [--wiki ~/.deneb/wiki]
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

TRAILING_STAGE_WORDS = {"협의", "입찰", "검토", "재검토", "기회", "송부", "대기", "요청", "문의"}
STATUS_TAIL_WORDS = ("현재", "현안", "재송부", "재검토", "대기", "협상", "진행", "완료")
DATE_FRAGMENT = re.compile(r"(?:^|\s)\d{1,2}[./]\d{1,2}(?:\s|$)")
# A 년-suffixed year names a contract vintage ("2026년 모듈 공급 계약") and is
# part of the name; a bare year is an event timestamp and violates.
YEAR_FRAGMENT = re.compile(r"20\d{2}년?")
TITLE_LINE = re.compile(r"^title:\s*(.+)$", re.M)
RESERVED_DIRS = {"거래", "메일분석", "자료", "회의록"}


def lint_title(title: str) -> list[str]:
    """Return violations for one title; empty when it is a clean name."""
    title = title.strip()
    if not title:
        return []
    violations: list[str] = []

    fields = title.split()
    if fields and fields[-1] in TRAILING_STAGE_WORDS:
        violations.append(f"말단 단계어 '{fields[-1]}' — 단계는 stage: 필드의 몫")

    for sep in (" — ", " – "):
        if sep in title:
            tail = title.split(sep, 1)[1]
            if any(w in tail for w in STATUS_TAIL_WORDS):
                violations.append(f"상태 꼬리 '{tail.strip()}' — 상태는 summary·로그의 몫")
            break

    dated = bool(DATE_FRAGMENT.search(title)) or any(
        not m.endswith("년") for m in YEAR_FRAGMENT.findall(title)
    )
    if dated:
        violations.append("날짜 토큰 — 날짜는 로그 섹션의 몫")
    return violations


def rep_pages(wiki: Path):
    """Yield (relative path, title) for every project rep page."""
    project_root = wiki / "프로젝트"
    if not project_root.is_dir():
        return
    for folder in sorted(project_root.iterdir()):
        if not folder.is_dir() or folder.name in RESERVED_DIRS:
            continue
        rep = folder / "대표.md"
        if not rep.is_file():
            continue
        m = TITLE_LINE.search(rep.read_text(encoding="utf-8", errors="replace"))
        if m:
            yield rep.relative_to(wiki), m.group(1).strip()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--wiki", default=str(Path.home() / ".deneb" / "wiki"))
    args = parser.parse_args()

    wiki = Path(args.wiki).expanduser()
    if not wiki.is_dir():
        print(f"wiki_title_lint: wiki not found at {wiki}", file=sys.stderr)
        return 2

    total = 0
    dirty = 0
    for rel, title in rep_pages(wiki):
        total += 1
        violations = lint_title(title)
        if violations:
            dirty += 1
            print(f"{rel}: {title}")
            for v in violations:
                print(f"  - {v}")
    print(f"wiki_title_lint: {total} rep pages, {dirty} violating")
    return 1 if dirty else 0


if __name__ == "__main__":
    sys.exit(main())
