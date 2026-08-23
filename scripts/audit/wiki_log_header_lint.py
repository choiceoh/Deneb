#!/usr/bin/env python3
"""wiki_log_header_lint — project 로그.md entry headers follow one grammar.

`## [YYYY-MM-DD] <op> | <주제>` is the entry header the layout defines, and
`RotateProjectLog` keeps the newest N *H2 sections*. Only two writers emit the
grammar (site visits, meeting attendance); the dreamer and the wiki tool append
free text, so entry-internal sub-headings ("## 요지", "## 핵심") are counted as
entries and rotation can split one entry across the archive boundary.

Advisory: it reports non-conforming headers so the drift is visible and the
rotation boundary can be fixed deliberately. Exits 1 when violations exist.
"""

from __future__ import annotations

import argparse
import os
import re
import sys

ENTRY_HEADER = re.compile(r"^## \[\d{4}-\d{2}-\d{2}\] \S+ \| .+$")
H2 = re.compile(r"^## .+$")
LOG_FILES = ("로그.md", "로그-보관.md")


def scan(wiki_dir: str) -> tuple[int, list[tuple[str, int, str]]]:
    conforming = 0
    violations: list[tuple[str, int, str]] = []
    projects = os.path.join(wiki_dir, "프로젝트")
    for root, _dirs, files in os.walk(projects):
        for name in files:
            if name not in LOG_FILES:
                continue
            path = os.path.join(root, name)
            rel = os.path.relpath(path, wiki_dir).replace(os.sep, "/")
            try:
                with open(path, encoding="utf-8", errors="ignore") as fh:
                    lines = fh.read().split("\n")
            except OSError:
                continue
            for lineno, line in enumerate(lines, start=1):
                if not H2.match(line):
                    continue
                if ENTRY_HEADER.match(line):
                    conforming += 1
                else:
                    violations.append((rel, lineno, line.strip()))
    return conforming, violations


def main() -> int:
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    ap.add_argument("--wiki", default=os.path.expanduser("~/.deneb/wiki"))
    ap.add_argument("--show", type=int, default=15, help="how many violations to print")
    args = ap.parse_args()

    conforming, violations = scan(args.wiki)
    for rel, lineno, text in violations[: args.show]:
        print(f"{rel}:{lineno}  {text}")
    if len(violations) > args.show:
        print(f"… and {len(violations) - args.show} more")
    print(
        f"wiki_log_header_lint: {len(violations)} non-conforming H2 header(s), "
        f"{conforming} conforming"
    )
    return 1 if violations else 0


if __name__ == "__main__":
    sys.exit(main())
