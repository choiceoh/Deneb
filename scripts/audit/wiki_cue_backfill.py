#!/usr/bin/env python3
"""Backfill 대표페이지 recall cues from the folder's real usage vocabulary.

Cues are the retrieval entry-point field (Memora-style) — measured 2026-07-21
only 8/66 대표페이지 carried any, leaving the designed vocab-gap lever unused.
This backfills them DETERMINISTICALLY (no LLM, idempotent): for each
프로젝트/*/대표.md, candidate tokens come from the frontmatter titles of the
folder's own 메일분석/회의록 subpages — the vocabulary real mail and meetings
actually used for this project — minus tokens the 대표 page already carries
anywhere (cue doctrine: cues are words NOT already on the page), minus the
GENERIC stoplist shared with mine_analysis_gold. Top tokens by frequency are
merged into cues (total capped at 10, matching the store's write-time cap).

Usage:
  scripts/audit/wiki_cue_backfill.py --wiki <wiki-dir> [--apply] [--max-add 5]

Dry-run by default; --apply writes. Point --wiki at a scratch copy first,
bench, then at production.
"""

import argparse
import collections
import pathlib
import re

from mine_analysis_gold import ok_token

# Mail-boilerplate tokens measured in the first dry-run (2026-07-21) — words
# every subject line uses that name no project. Suffix classes (…관련/…사항)
# are rejected structurally below.
CUE_BOILER = {
    "첨부", "요청", "질의", "문의", "외부메일", "견적서", "날인", "재요청",
    "따른", "정리", "회신", "미선적", "태양광발전사업", "간선등",
}
CUE_BAD_SUFFIX = ("관련", "사항", "여부", "안내")
CUE_PARTICLES = ("의", "건", "에", "은", "는", "을", "를", "과", "와")


def normalize_cue(t: str) -> str:
    for suf in CUE_PARTICLES:
        if len(t) > 2 and t.endswith(suf):
            return t[: -len(suf)]
    return t


def frontmatter(text: str) -> tuple[str, str]:
    m = re.match(r"^---\n(.*?)\n---\n?(.*)$", text, re.S)
    return (m.group(1), m.group(2)) if m else ("", text)


def subpage_titles(folder: pathlib.Path) -> list[str]:
    titles = []
    for sub in folder.rglob("*.md"):
        if sub.name in ("대표.md", "로그.md") or ".bak" in str(sub):
            continue
        head = sub.read_text(errors="ignore")[:1500]
        tm = re.search(r"^title:\s*(.+)$", head, re.M)
        if tm:
            titles.append(tm.group(1).strip().strip("\"'"))
    return titles


def identity_tokens(rep_text: str) -> set[str]:
    fm, _ = frontmatter(rep_text)
    out: set[str] = set()
    for key in ("title", "client", "sites"):
        m = re.search(rf"^{key}:\s*(.+)$", fm, re.M)
        if not m:
            continue
        for t in re.split(r"[\s\-_(),:·—\[\]\"'/.]+", m.group(1).lower()):
            t = normalize_cue(t)
            if len(t) >= 2:
                out.add(t)
    return out


def candidate_cues(
    rep_text: str, titles: list[str], max_add: int, foreign: set[str] = frozenset()
) -> list[str]:
    fm, body = frontmatter(rep_text)
    page_text = (fm + " " + body).lower()
    freq: collections.Counter = collections.Counter()
    for title in titles:
        for t in re.split(r"[\s\-_(),:·—\[\]\"'/.]+", title.lower()):
            t = normalize_cue(t)
            if len(t) < 2 or not re.search(r"[가-힣0-9]", t):
                continue
            if t in CUE_BOILER or t.endswith(CUE_BAD_SUFFIX):
                continue
            if not ok_token(t):
                continue
            if t in page_text:
                continue
            # Another project's identity claims this token — the folder holds
            # cross-filed mail (measured: 완도 folder carrying 당진/강진 mail);
            # adopting it as a cue would attract that project's queries here.
            if t in foreign:
                continue
            freq[t] += 1
    # Frequency ≥2: a word one mail used once is noise; a word several real
    # subjects share is how people actually name this project's work.
    return [t for t, n in freq.most_common(max_add * 3) if n >= 2][:max_add]


def merge_cues(fm: str, adds: list[str]) -> str | None:
    m = re.search(r"^cues:\s*\[(.*?)\]\s*$", fm, re.M)
    existing = []
    if m:
        existing = [c.strip().strip("\"'") for c in m.group(1).split(",") if c.strip()]
    merged = existing + [a for a in adds if a not in existing]
    merged = merged[:10]
    if merged == existing:
        return None
    line = "cues: [" + ", ".join(merged) + "]"
    if m:
        return fm[: m.start()] + line + fm[m.end():]
    return fm + "\n" + line


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--wiki", required=True)
    ap.add_argument("--apply", action="store_true")
    ap.add_argument("--max-add", type=int, default=5)
    args = ap.parse_args()
    root = pathlib.Path(args.wiki)

    reps = sorted(root.glob("프로젝트/*/대표.md"))
    ident_by_rep = {rep: identity_tokens(rep.read_text(errors="ignore")) for rep in reps}

    touched = added = 0
    for rep in reps:
        text = rep.read_text(errors="ignore")
        fm, body = frontmatter(text)
        if not fm:
            continue
        foreign = set().union(*(v for k, v in ident_by_rep.items() if k != rep)) - ident_by_rep[rep]
        adds = candidate_cues(text, subpage_titles(rep.parent), args.max_add, foreign)
        if not adds:
            continue
        new_fm = merge_cues(fm, adds)
        if new_fm is None:
            continue
        touched += 1
        added += len(adds)
        print(f"{rep.parent.name}: +{adds}")
        if args.apply:
            rep.write_text("---\n" + new_fm + "\n---\n" + body, encoding="utf-8")
    mode = "APPLIED" if args.apply else "dry-run"
    print(f"{mode}: {touched} pages, {added} cues")


if __name__ == "__main__":
    main()
