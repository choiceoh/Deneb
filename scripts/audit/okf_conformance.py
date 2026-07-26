#!/usr/bin/env python3
"""Check the live wiki against Open Knowledge Format v0.2 §11 conformance.

Deneb's wiki already targets OKF (page.go carries the `resource` field and
defaults `type` at render). §11 makes conformance narrow and checkable:

  1. every non-reserved .md file has a parseable YAML frontmatter block,
  2. every frontmatter block carries a non-empty `type`,
  3. reserved filenames (index.md, log.md) are not concept documents.

Everything else in the spec is SHOULD-level, so this reports those separately
as advisories rather than failures — notably §4's UTF-8 requirement, which the
mail charset gap has broken before (a GBK sender name landed in a summary).

Deliberately NOT a Go test: the wiki lives outside the repo, so a test over it
would go red locally and green in CI. Run it on the machine that holds the data.

    python3 scripts/audit/okf_conformance.py [--wiki DIR]

Exit 1 when a MUST-level rule fails.
"""
import argparse
import os
import pathlib
import re
import sys

RESERVED = {"index.md", "log.md", "_index.md"}
SPEC = "https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md"


def frontmatter(lines):
    """Return the frontmatter lines, or None when the block is malformed."""
    if not lines or lines[0] != "---":
        return None
    for i in range(1, len(lines)):
        if lines[i] == "---":
            return lines[1:i]
    return None


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--wiki", default=os.path.expanduser("~/.deneb/wiki"))
    args = ap.parse_args()
    root = pathlib.Path(args.wiki)
    if not root.is_dir():
        print(f"위키 없음: {root}")
        return 0

    must, advisory, pages = [], [], 0
    for p in sorted(root.rglob("*.md")):
        rel = p.relative_to(root)
        if p.name in RESERVED:
            continue
        pages += 1
        try:
            text = p.read_text(encoding="utf-8")
        except UnicodeDecodeError as e:
            # §4 requires UTF-8. Advisory because §11 does not restate it.
            advisory.append(f"비-UTF-8 (§4): {rel} — {e}")
            continue
        fm = frontmatter(text.split("\n"))
        if fm is None:
            must.append(f"프론트매터 파싱 불가 (§11.1): {rel}")
            continue
        types = [m.group(1) for line in fm if (m := re.match(r"^type:\s*(\S+)", line))]
        if not types:
            must.append(f"type 없음 (§11.2): {rel}")
        elif len(types) > 1:
            advisory.append(f"type 중복 {types}: {rel}")

    print(f"OKF v0.2 적합성 — 개념 문서 {pages}개  ({SPEC})")
    for line in must:
        print(f"  ✗ {line}")
    for line in advisory:
        print(f"  ! {line}")
    if not must and not advisory:
        print("  ✓ MUST 위반 없음, 권고 위반 없음")
    elif not must:
        print(f"  ✓ MUST 위반 없음 (권고 {len(advisory)}건)")
    return 1 if must else 0


if __name__ == "__main__":
    sys.exit(main())
