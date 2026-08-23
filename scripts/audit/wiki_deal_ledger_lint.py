#!/usr/bin/env python3
"""wiki_deal_ledger_lint — counterparty ledgers live in exactly one folder.

`프로젝트/거래/<slug>.md` is where `UpsertDealPage` writes and the only folder
`knownCounterparties` lists, so a ledger anywhere else loses its recall anchor
and the next approval capture re-mints a duplicate at the fixed path. Two
violation classes:

  stray  — a `type: deal` page outside 프로젝트/거래, excluding pages inside a
           project folder (the layout places those: a cross-project supply
           ledger such as 프로젝트/남도에코-케이블/대표.md is legitimate).
  split  — the same ledger slug present in more than one folder.

Read-only. Exits 1 on violations so it can gate a fixture tree in CI; run it
against the live wiki by hand.
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from collections import defaultdict

LEDGER_DIR = "프로젝트/거래"
PROJECT_PREFIX = "프로젝트/"
FRONTMATTER = re.compile(r"\A---\n(.*?)\n---", re.S)
TYPE_LINE = re.compile(r"^type:\s*(\S+)", re.M)


def page_type(path: str) -> str:
    try:
        with open(path, encoding="utf-8", errors="ignore") as fh:
            head = fh.read(4096)
    except OSError:
        return ""
    m = FRONTMATTER.match(head)
    if not m:
        return ""
    t = TYPE_LINE.search(m.group(1))
    return t.group(1).strip().lower() if t else ""


def in_project_folder(rel: str) -> bool:
    """True for pages the project layout places (프로젝트/<name>/…)."""
    if not rel.startswith(PROJECT_PREFIX):
        return False
    return "/" in rel[len(PROJECT_PREFIX):]


def scan(wiki_dir: str) -> tuple[list[str], dict[str, list[str]]]:
    stray: list[str] = []
    by_slug: dict[str, list[str]] = defaultdict(list)
    for root, _dirs, files in os.walk(wiki_dir):
        for name in files:
            if not name.endswith(".md"):
                continue
            abs_path = os.path.join(root, name)
            rel = os.path.relpath(abs_path, wiki_dir).replace(os.sep, "/")
            if page_type(abs_path) != "deal":
                continue
            if rel.startswith(LEDGER_DIR + "/"):
                by_slug[name].append(rel)
                continue
            if in_project_folder(rel):
                continue  # layout-placed, not a stray ledger
            stray.append(rel)
            by_slug[name].append(rel)
    return sorted(stray), {k: sorted(v) for k, v in by_slug.items() if len(v) > 1}


def main() -> int:
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    ap.add_argument("--wiki", default=os.path.expanduser("~/.deneb/wiki"))
    args = ap.parse_args()

    stray, split = scan(args.wiki)
    for rel in stray:
        print(f"stray  {rel} — type: deal outside {LEDGER_DIR}/")
    for slug, paths in sorted(split.items()):
        print(f"split  {slug} — {', '.join(paths)}")
    total = len(stray) + len(split)
    print(
        f"wiki_deal_ledger_lint: {total} violation(s) "
        f"({len(stray)} stray, {len(split)} split)"
    )
    return 1 if total else 0


if __name__ == "__main__":
    sys.exit(main())
