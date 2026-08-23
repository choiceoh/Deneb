#!/usr/bin/env python3
"""gold_repoint — repoint recall gold paths at the pages they now live in.

The gold sets store `gold_paths` as wiki-relative paths, and `recall-bench`
scores a hit when a result path CONTAINS one of them. Every wiki restructure
therefore rots part of the gold: the 2026-07-19 rename of 61 project folders to
deal codes killed 29 of 105 cases at the time, and the 2026-08-23 deal-ledger
consolidation moved another set. Rotted gold reads exactly like a retrieval
regression — that is how a healthy search once measured 63.5%.

Resolution is deterministic and conservative. A path is repointed only when
exactly one candidate survives, in this order:

  1. git-follow — the wiki is a git repository, so `git log --follow` names the
     path a file was renamed from/to. Evidence, not inference.
  2. unique basename — that filename lives at exactly one live path.
  3. unique title — one page's frontmatter title matches the dead path's stem.
  4. unique project folder — a folder-shaped gold path (no .md, which the bench
     matches against every page under it) resolves to the one live project
     folder whose 대표.md title STARTS WITH the old folder name. Folder renames
     to deal codes leave no per-file rename to follow, and the title is the
     human alias the code folder kept.

Anything ambiguous, or a page that is simply gone (content deleted, not moved),
is reported and left alone: a wrong pointer is worse than a visible hole,
because the bench then scores it as a miss forever.

WRITE PATH (documented, operator-gated per docs/agent-rules/
product-lane-ai-maintainability.md): the default run is read-only and prints
what it would do. `--apply` is the explicit operator opt-in that rewrites the
gold files, and it keeps a `.bak-<epoch>` copy of every file it touches.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import time
from collections import defaultdict

GOLD_PREFIX = "wiki-qa-gold"
FRONTMATTER = re.compile(r"\A---\n(.*?)\n---", re.S)
TITLE_LINE = re.compile(r"^title:\s*(.+)$", re.M)


def live_pages(wiki_dir: str) -> list[str]:
    """Every page on disk, wiki-relative, sorted for determinism."""
    out = []
    for root, _dirs, files in os.walk(wiki_dir):
        if ".git" in root.split(os.sep):
            continue
        for name in files:
            if name.endswith(".md"):
                rel = os.path.relpath(os.path.join(root, name), wiki_dir)
                out.append(rel.replace(os.sep, "/"))
    return sorted(out)


def is_live(gold_path: str, pages: list[str]) -> bool:
    """Mirror recall-bench's substring hit rule."""
    return any(gold_path in p for p in pages)


def page_title(wiki_dir: str, rel: str) -> str:
    try:
        with open(os.path.join(wiki_dir, rel), encoding="utf-8", errors="ignore") as fh:
            head = fh.read(4096)
    except OSError:
        return ""
    m = FRONTMATTER.match(head)
    if not m:
        return ""
    t = TITLE_LINE.search(m.group(1))
    return t.group(1).strip() if t else ""


def normalize(text: str) -> str:
    return re.sub(r"[^0-9A-Za-z가-힣]+", "", text).lower()


def rename_map(wiki_dir: str) -> dict[str, str]:
    """old path → newest path, following rename chains through the wiki history.

    Built in one pass over `git log --diff-filter=R` rather than per-path
    `--follow`: a gold path points at a page that no longer exists, and
    `--follow` needs a path present at HEAD, so it finds nothing for exactly
    the cases this tool exists to fix. Cached per wiki dir — the history is a
    few hundred commits and the resolver asks about many paths.
    """
    cached = _RENAME_CACHE.get(wiki_dir)
    if cached is not None:
        return cached
    direct: dict[str, str] = {}
    try:
        proc = subprocess.run(
            ["git", "-C", wiki_dir, "log", "--all", "--reverse",
             "--diff-filter=R", "--name-status", "--format=", "-M"],
            capture_output=True, text=True, timeout=60, check=False,
        )
    except (OSError, subprocess.SubprocessError):
        _RENAME_CACHE[wiki_dir] = {}
        return {}
    for line in proc.stdout.split("\n"):
        parts = line.split("\t")
        if len(parts) == 3 and parts[0].startswith("R"):
            direct[unquote_git_path(parts[1])] = unquote_git_path(parts[2])
    # Follow chains (A→B→C) so an old gold path lands on the current name.
    resolved: dict[str, str] = {}
    for src in direct:
        seen = {src}
        dst = direct[src]
        while dst in direct and dst not in seen:
            seen.add(dst)
            dst = direct[dst]
        resolved[src] = dst
    _RENAME_CACHE[wiki_dir] = resolved
    return resolved


_RENAME_CACHE: dict[str, dict[str, str]] = {}


def unquote_git_path(raw: str) -> str:
    """Decode git's quoted path form.

    Every wiki path is Korean, and git quotes non-ASCII names as
    "\355\224\204…" octal escapes unless core.quotePath is off — so a rename
    map built from the raw output never matches a gold path.
    """
    raw = raw.strip()
    if not (raw.startswith('"') and raw.endswith('"')):
        return raw
    body = raw[1:-1]
    out = bytearray()
    i = 0
    while i < len(body):
        if body[i] == "\\" and i + 3 < len(body) and body[i + 1 : i + 4].isdigit():
            out.append(int(body[i + 1 : i + 4], 8))
            i += 4
            continue
        if body[i] == "\\" and i + 1 < len(body):  # \" \\ and friends
            out.extend(body[i + 1].encode())
            i += 2
            continue
        out.extend(body[i].encode())
        i += 1
    return out.decode("utf-8", errors="replace")


def git_renames(wiki_dir: str, dead_path: str) -> list[str]:
    """The current path of a renamed page, per the wiki's git history."""
    candidate = dead_path if dead_path.endswith(".md") else dead_path + ".md"
    dst = rename_map(wiki_dir).get(candidate)
    return [dst] if dst else []


def resolve(wiki_dir: str, dead_path: str, pages: list[str]) -> tuple[str, str]:
    """Return (new_path, reason) — new_path empty when unresolved."""
    stem = dead_path[:-3] if dead_path.endswith(".md") else dead_path
    base = os.path.basename(stem)

    renamed = [p for p in git_renames(wiki_dir, dead_path) if p in pages]
    if len(renamed) == 1:
        return renamed[0][:-3], "git-rename"
    if len(renamed) > 1:
        return "", f"ambiguous git renames: {', '.join(renamed)}"

    by_base = [p for p in pages if os.path.basename(p)[:-3] == base]
    if len(by_base) == 1:
        return by_base[0][:-3], "unique-basename"
    if len(by_base) > 1:
        return "", f"ambiguous basename ({len(by_base)} pages)"

    want = normalize(base)
    if want:
        by_title = [p for p in pages if normalize(page_title(wiki_dir, p)) == want]
        if len(by_title) == 1:
            return by_title[0][:-3], "unique-title"
        if len(by_title) > 1:
            return "", f"ambiguous title ({len(by_title)} pages)"

    if not dead_path.endswith(".md") and dead_path.startswith("프로젝트/"):
        if folder := resolve_project_folder(wiki_dir, want, pages):
            return folder, "project-folder-title"
    return "", "no candidate (page deleted, not moved?)"


def resolve_project_folder(wiki_dir: str, want: str, pages: list[str]) -> str:
    """The one project folder whose 대표.md title starts with `want`, else "".

    Anchored at the start so "영광-한빛-bess" reaches
    "영광 한빛 BESS (80MW/480MWh, 한빛에너지저장)" without a mid-string
    coincidence pulling in an unrelated project.
    """
    if not want:
        return ""
    hits = []
    for p in pages:
        if not p.startswith("프로젝트/") or not p.endswith("/대표.md"):
            continue
        if normalize(page_title(wiki_dir, p)).startswith(want):
            hits.append(p[: -len("/대표.md")])
    return hits[0] if len(hits) == 1 else ""


def gold_files(state_dir: str) -> list[str]:
    return sorted(
        os.path.join(state_dir, n)
        for n in os.listdir(state_dir)
        if n.startswith(GOLD_PREFIX) and n.endswith(".jsonl")
    )


def process(path: str, wiki_dir: str, pages: list[str], apply: bool) -> tuple[int, int]:
    """Repoint one gold file. Returns (repointed, unresolved)."""
    with open(path, encoding="utf-8") as fh:
        lines = fh.read().split("\n")
    repointed = unresolved = 0
    out_lines: list[str] = []
    label = os.path.basename(path)
    for line in lines:
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            out_lines.append(line)
            continue
        try:
            row = json.loads(stripped)
        except json.JSONDecodeError:
            out_lines.append(line)
            continue
        changed = False
        new_paths: list[str] = []
        for gp in row.get("gold_paths", []):
            if is_live(gp, pages):
                new_paths.append(gp)
                continue
            new, reason = resolve(wiki_dir, gp, pages)
            if new:
                print(f"  {label}  {row.get('id')}: {gp} → {new}  [{reason}]")
                new_paths.append(new)
                repointed += 1
                changed = True
            else:
                print(f"  {label}  {row.get('id')}: {gp} — UNRESOLVED ({reason})")
                new_paths.append(gp)
                unresolved += 1
        if changed:
            row["gold_paths"] = new_paths
            out_lines.append(json.dumps(row, ensure_ascii=False))
        else:
            out_lines.append(line)
    if apply and repointed:
        os.replace(path, f"{path}.bak-{int(time.time())}")
        with open(path, "w", encoding="utf-8") as fh:
            fh.write("\n".join(out_lines))
    return repointed, unresolved


def main() -> int:
    ap = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    ap.add_argument("--wiki", default=os.path.expanduser("~/.deneb/wiki"))
    ap.add_argument("--state-dir", default=os.path.expanduser("~/.deneb"))
    ap.add_argument("--apply", action="store_true",
                    help="rewrite the gold files (default: read-only preview)")
    args = ap.parse_args()

    pages = live_pages(args.wiki)
    if not pages:
        print(f"gold_repoint: no pages under {args.wiki}", file=sys.stderr)
        return 1

    totals: dict[str, int] = defaultdict(int)
    for gold in gold_files(args.state_dir):
        repointed, unresolved = process(gold, args.wiki, pages, args.apply)
        totals["repointed"] += repointed
        totals["unresolved"] += unresolved

    verb = "repointed" if args.apply else "would repoint"
    print(f"gold_repoint: {verb} {totals['repointed']}, unresolved {totals['unresolved']}")
    if not args.apply and totals["repointed"]:
        print("gold_repoint: re-run with --apply to write (a .bak-<epoch> copy is kept)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
