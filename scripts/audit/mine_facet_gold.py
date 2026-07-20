#!/usr/bin/env python3
"""Mine a facet-path recall gold set from wiki frontmatter identity metadata.

Builds ~/.deneb/wiki-qa-gold-facet.jsonl: each case asks about a project by its
counterparty (client:), site (sites:/address: leaf), or venture group
(program:) — the structured frontmatter identity vocabulary that the hidden
facet field (wiki search.go facetText, DENEB_WIKI_FACET_BOOST) makes lexically
searchable. This probes the query class the analysis-xl miner is structurally
blind to: that miner labels subjects by project FOLDER-NAME anchor tokens, so
a query using vocabulary absent from the folder name (and often from the whole
page prose — measured 2026-07-20: client 27/89, sites 27/56, address 25/30
values carry tokens found nowhere in title/summary/tags/cues/body) can never
enter its gold. Consumed by gateway-go/cmd/recall-bench via -gold; A/B against
the facet field with DENEB_WIKI_FACET_BOOST=0 vs default.

Precision guards (mirroring mine_analysis_gold.py):
  * generic-token stoplist + shape filters (no digits, no short latin);
  * single-target constraint — a site/address/program anchor must resolve to
    exactly ONE project across the whole corpus or it is skipped; client
    anchors may resolve to several projects and the gold lists them all
    (recall-bench accepts any listed path as a hit).

Usage:
  scripts/audit/mine_facet_gold.py --wiki <wiki-copy-dir> \
      [--out ~/.deneb/wiki-qa-gold-facet.jsonl]
"""

import argparse
import collections
import json
import pathlib
import re

GENERIC = {
    "태양광", "발전소", "프로젝트", "에너지", "산업", "현장", "공장", "사업",
    "전북", "전남", "경북", "경남", "충북", "충남", "강원", "경기", "제주",
    "탑솔라", "topsolar",
}

QUESTION = {
    "client": "{a} 쪽 프로젝트 요즘 어떻게 되고 있어?",
    "sites": "{a} 현장 근황 알려줘",
    "address": "{a} 현장 근황 알려줘",
    "program": "{a} 전체 그림 정리해줘",
}


def ok_token(t: str) -> bool:
    if t in GENERIC or len(t) < 2 or t.isdigit():
        return False
    if re.fullmatch(r"\d+(mw|kw|km|개소|차|호)?", t):
        return False
    if re.fullmatch(r"[a-z0-9]+", t) and len(t) < 5:
        return False
    return True


def parse_frontmatter(text: str) -> dict[str, str]:
    m = re.match(r"^---\n(.*?)\n---\n?", text, re.S)
    if not m:
        return {}
    fm: dict[str, str] = {}
    for line in m.group(1).splitlines():
        km = re.match(r"^([a-z_]+):\s*(.*)$", line)
        if km:
            fm[km.group(1)] = km.group(2).strip()
    return fm


def flow_list(v: str) -> list[str]:
    v = v.strip()
    if v.startswith("[") and v.endswith("]"):
        return [x.strip().strip("\"'") for x in v[1:-1].split(",") if x.strip()]
    return [v] if v else []


def project_folder(rel: pathlib.PurePath) -> str | None:
    parts = rel.parts
    if len(parts) >= 2 and parts[0] == "프로젝트":
        return "/".join(parts[:2])
    return None


def site_leaf(site: str) -> str:
    toks = [t for t in site.split() if t]
    return toks[-1] if toks else ""


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--wiki", required=True, help="wiki directory (a COPY of prod)")
    ap.add_argument(
        "--out", default=str(pathlib.Path.home() / ".deneb/wiki-qa-gold-facet.jsonl")
    )
    args = ap.parse_args()
    root = pathlib.Path(args.wiki)

    # anchor -> kind -> set of gold target paths
    targets: dict[tuple[str, str], set[str]] = collections.defaultdict(set)
    for fp in root.rglob("*.md"):
        rel = fp.relative_to(root)
        if fp.name in ("index.md", "_index.md", "log.md") or ".bak" in str(rel):
            continue
        fm = parse_frontmatter(fp.read_text(errors="ignore"))
        if not fm:
            continue
        folder = project_folder(rel)
        gold = folder or str(rel)
        for c in flow_list(fm.get("client", "")):
            if ok_token(c.lower()):
                targets[("client", c)].add(gold)
        for s in flow_list(fm.get("sites", "")):
            leaf = site_leaf(s)
            if ok_token(leaf.lower()):
                targets[("sites", leaf)].add(gold)
        if addr := fm.get("address", ""):
            leaf = site_leaf(addr)
            if ok_token(leaf.lower()):
                targets[("address", leaf)].add(gold)
        if prog := fm.get("program", ""):
            if ok_token(prog.lower()):
                targets[("program", prog)].add(gold)

    # Site/address/program anchors must be unambiguous; merge address into an
    # existing sites anchor of the same name (same place, two field spellings).
    cases = []
    seen_anchor: set[str] = set()
    for (kind, anchor), golds in sorted(targets.items()):
        if anchor in seen_anchor:
            continue
        if kind != "client" and len(golds) > 1:
            continue
        seen_anchor.add(anchor)
        slug = re.sub(r"[^0-9a-z가-힣]+", "-", anchor.lower()).strip("-")
        cases.append(
            {
                "id": f"facet-{kind[:2]}-{slug}",
                "category": f"facet-{kind}",
                "question": QUESTION[kind].format(a=anchor),
                "gold_paths": sorted(golds),
            }
        )

    out = pathlib.Path(args.out).expanduser()
    with out.open("w", encoding="utf-8") as f:
        f.write("# facet-path gold — mined by scripts/audit/mine_facet_gold.py\n")
        for c in cases:
            f.write(json.dumps(c, ensure_ascii=False) + "\n")
    by_kind = collections.Counter(c["category"] for c in cases)
    print(f"wrote {len(cases)} cases -> {out} ({dict(by_kind)})")


if __name__ == "__main__":
    main()
