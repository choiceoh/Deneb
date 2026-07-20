#!/usr/bin/env python3
"""Mine meeting/log-provenance recall gold from the wiki's own filing.

The hand-written meeting gold (42 cases) is variance-bound and the raw
material (Plaud transcripts) accrues slowly — but the wiki already files two
meeting-shaped provenance sources per project: 회의록/ pages (one per
recording) and 로그.md dated sections ('## [YYYY-MM-DD] <op> | <주제>').
Query = the topic the meeting/log entry itself used; gold = the containing
project (plus program-mates, plus any OTHER project whose unique identity
anchor the topic names — multigold for multi-topic meetings). Rerun as
material accrues; today's corpus yields ~50 cases and grows with every
harvested recording.

Usage:
  scripts/audit/mine_meeting_gold.py --wiki <wiki-copy> \
      [--out ~/.deneb/wiki-qa-gold-meeting-xl.jsonl]
"""

from __future__ import annotations

import argparse
import json
import pathlib
import re

from mine_analysis_gold import ok_token
from wiki_cue_backfill import frontmatter, identity_tokens, normalize_cue

TOPIC_MIN_RUNES = 8

# System/housekeeping log entries are filing artifacts, not meeting content —
# nobody will ever query them (dreamer maintenance, transcript plumbing).
TOPIC_SYSTEM = re.compile(r"(자율 리서치|페이지 갱신|원본 녹음|전사 자료|^안내\b)")


def project_dirs(root: pathlib.Path) -> list[pathlib.Path]:
    return sorted(
        p for p in (root / "프로젝트").iterdir() if (p / "대표.md").is_file()
    )


def topic_tokens(topic: str) -> set[str]:
    out = set()
    for t in re.split(r"[\s\-_(),:·—\[\]\"'/.|]+", topic.lower()):
        t = normalize_cue(t)
        if len(t) >= 2 and ok_token(t):
            out.add(t)
    return out


def collect_topics(proj: pathlib.Path) -> list[str]:
    topics = []
    for sub in proj.glob("회의록/*.md"):
        fm, _ = frontmatter(sub.read_text(errors="ignore"))
        tm = re.search(r"^title:\s*(.+)$", fm, re.M)
        if tm:
            topics.append(tm.group(1).strip().strip("\"'"))
    log = proj / "로그.md"
    if log.is_file():
        for m in re.finditer(r"^## \[\d{4}-\d{2}-\d{2}\]\s*(?:[^|\n]*\|)?\s*(.+)$", log.read_text(errors="ignore"), re.M):
            topics.append(m.group(1).strip())
    kept = []
    for t in topics:
        if len(t) < TOPIC_MIN_RUNES or not re.search(r"[가-힣]", t):
            continue
        if TOPIC_SYSTEM.search(t):
            continue
        # An all-generic topic ("가배치 수정 요청") is a valid log line but an
        # unanswerable query — demand at least one contentful token.
        if not topic_tokens(t):
            continue
        kept.append(t)
    return kept


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--wiki", required=True)
    ap.add_argument(
        "--out", default=str(pathlib.Path.home() / ".deneb/wiki-qa-gold-meeting-xl.jsonl")
    )
    args = ap.parse_args()
    root = pathlib.Path(args.wiki)

    projs = project_dirs(root)
    ident: dict[str, set[str]] = {}
    program: dict[str, str] = {}
    for p in projs:
        rel = f"프로젝트/{p.name}"
        text = (p / "대표.md").read_text(errors="ignore")
        ident[rel] = identity_tokens(text)
        pm = re.search(r"^program:\s*(\S+)", frontmatter(text)[0], re.M)
        program[rel] = pm.group(1).strip().strip("\"'") if pm else ""
    # A token is a cross-reference anchor only when exactly ONE project's
    # identity claims it (same single-owner discipline as the analysis miner).
    owners: dict[str, list[str]] = {}
    for rel, toks in ident.items():
        for t in toks:
            owners.setdefault(t, []).append(rel)

    cases, seen = [], set()
    for p in projs:
        rel = f"프로젝트/{p.name}"
        for topic in collect_topics(p):
            key = topic.lower().replace(" ", "")
            if key in seen:
                continue
            seen.add(key)
            gold = {rel}
            gold.update(q for q, pg in program.items() if pg and pg == program[rel])
            for t in topic_tokens(topic):
                own = owners.get(t, [])
                if len(own) == 1 and own[0] != rel:
                    gold.add(own[0])  # multi-topic meeting names another project
            cases.append(
                {
                    "id": f"mt-{len(cases)}",
                    "category": "meeting-prov",
                    "question": topic,
                    "gold_paths": sorted(gold, key=lambda g: (g != rel, g)),
                    "must_contain": [],
                }
            )

    out = pathlib.Path(args.out).expanduser()
    with out.open("w", encoding="utf-8") as f:
        f.write("# Meeting/log-provenance gold — mined by scripts/audit/mine_meeting_gold.py\n")
        for c in cases:
            f.write(json.dumps(c, ensure_ascii=False) + "\n")
    multi = sum(1 for c in cases if len(c["gold_paths"]) > 1)
    print(f"{len(cases)} cases ({multi} multigold) -> {out}")


if __name__ == "__main__":
    main()
