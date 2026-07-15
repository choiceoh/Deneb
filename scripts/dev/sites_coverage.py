#!/usr/bin/env python3
"""Measure 현장(Sites) coverage across wiki project 대표페이지.

Read-only. Answers the go/no-go question for a 현장 지도: how many ACTIVE
projects actually carry a Site, and how do those Sites distribute across
시도/시군구 — i.e. would a map be densely populated or mostly empty?

Sites live in the YAML frontmatter of project 대표페이지 as a flow array
`sites: [광역약칭 시/군 읍/면/동, ...]`. This script mirrors the gateway's own
rules so the numbers match what production would actually render:

  - 대표페이지 detection mirrors wiki/project_layout.go IsProjectRepPage + ListPages:
    folder form 프로젝트/<name>/대표.md AND legacy flat 프로젝트/<name>.md, excluding
    reserved buckets (거래/메일분석/자료/회의록/mail-analyses), backup/hidden dirs
    (isNonPageDir), and index/log basenames. Folder form wins over legacy flat.
  - archived pages (frontmatter archived: true) are skipped, exactly as
    Store.knownProjects drops them from the active project surface.
  - sites are read from the flow form `sites: [..]` ONLY — production's
    parseFlowArray ignores YAML block form, so we do too (no false positives).
  - 시도 bucketing mirrors normalizeSiteName's provinceAbbrev.

Usage:
    python3 scripts/dev/sites_coverage.py <wiki-root>          # e.g. ~/.deneb/wiki
    python3 scripts/dev/sites_coverage.py <wiki-root> --json
"""
import sys
import os
import json
import re

# from wiki/page.go provinceAbbrev — full 광역 name -> fixed abbreviation
PROVINCE_ABBREV = {
    "전라북도": "전북", "전북특별자치도": "전북",
    "전라남도": "전남", "경상북도": "경북", "경상남도": "경남",
    "충청북도": "충북", "충청남도": "충남",
    "강원도": "강원", "강원특별자치도": "강원",
    "경기도": "경기", "제주도": "제주", "제주특별자치도": "제주",
    "서울특별시": "서울", "부산광역시": "부산", "대구광역시": "대구",
    "인천광역시": "인천", "광주광역시": "광주", "대전광역시": "대전",
    "울산광역시": "울산", "세종특별자치시": "세종",
}
SIDO = {"서울", "부산", "대구", "인천", "광주", "대전", "울산", "세종",
        "경기", "강원", "충북", "충남", "전북", "전남", "경북", "경남", "제주"}
# from wiki/project_layout.go reservedProjectDirs — 프로젝트/ children that are
# category buckets, not projects.
RESERVED = {"거래", "메일분석", "mail-analyses", "자료", "회의록"}
REP_PAGE_FILE = "대표.md"
# from wiki/store.go ListPages — derived files that are never project pages.
SKIP_BASENAMES = {"index.md", "_index.md", "log.md"}


def normalize_site(s):
    """Mirror wiki/page.go normalizeSiteName: trim, collapse ws, drop trailing
    period, abbreviate the leading province."""
    s = " ".join(s.strip().rstrip(".").split())
    if not s:
        return ""
    parts = s.split(" ", 1)
    first = parts[0]
    if first in PROVINCE_ABBREV:
        return PROVINCE_ABBREV[first] + (" " + parts[1] if len(parts) > 1 else "")
    return s


def parse_frontmatter(text):
    """Return the frontmatter block or ''. Line-anchored, mirroring
    wiki/page.go splitFrontmatter: the file must open with a '---' line, and the
    block ends at the next line that is exactly '---' (a bare '---' in the body
    must not be mistaken for a delimiter)."""
    lines = text.splitlines()
    if not lines or lines[0].strip() != "---":
        return ""
    for i in range(1, len(lines)):
        if lines[i].strip() == "---":
            return "\n".join(lines[1:i])
    return ""  # unclosed frontmatter — treat as none


def extract_sites(fm):
    """Extract sites from a frontmatter block — flow form `sites: [a, b]` ONLY,
    mirroring wiki/page.go: parseFlowArray receives just the same-line scalar, so
    YAML block form (`sites:` then `- a`) yields NO sites in production. We match
    that (block form → empty) so the verdict never counts sites the gateway
    would silently ignore."""
    m = re.search(r"^sites:\s*\[(.*?)\]\s*$", fm, re.M)
    if not m:
        return []
    inner = m.group(1).strip()
    return [x.strip() for x in inner.split(",") if x.strip()] if inner else []


def extract_scalar(fm, key):
    m = re.search(r"^%s:\s*(.+)$" % re.escape(key), fm, re.M)
    return m.group(1).strip() if m else ""


def is_non_page_dir(name):
    """Mirror wiki/store_index.go isNonPageDir: hidden or backup directories hold
    derived/backup state, never live pages — ListPages prunes them entirely."""
    if name.startswith("."):
        return True
    lower = name.lower()
    return "backup" in lower or ".bak" in lower or "백업" in name or lower == "bak"


def collect_rep_pages(proj_dir):
    """Return {project_name: abspath} for every 대표페이지, mirroring the gateway's
    active project surface: IsProjectRepPage (folder form 프로젝트/<name>/대표.md +
    legacy flat 프로젝트/<name>.md) filtered through ListPages — reserved buckets,
    backup/hidden dirs (isNonPageDir), and index/log basenames excluded. On a
    name collision the folder form wins (knownProjects)."""
    flat, folder = {}, {}
    for entry in sorted(os.listdir(proj_dir)):
        path = os.path.join(proj_dir, entry)
        if os.path.isfile(path) and entry.endswith(".md"):
            if entry in SKIP_BASENAMES:
                continue
            name = entry[:-3]
            if name and name not in RESERVED:
                flat[name] = path
        elif os.path.isdir(path) and entry not in RESERVED and not is_non_page_dir(entry):
            candidate = os.path.join(path, REP_PAGE_FILE)
            if os.path.isfile(candidate):
                folder[entry] = candidate
    return {**flat, **folder}  # folder form wins on name collision


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    as_json = "--json" in sys.argv
    if not args:
        print("usage: sites_coverage.py <wiki-root> [--json]", file=sys.stderr)
        sys.exit(2)
    root = os.path.expanduser(args[0])
    proj_dir = os.path.join(root, "프로젝트")
    if not os.path.isdir(proj_dir):
        print("no 프로젝트/ dir under %s" % root, file=sys.stderr)
        sys.exit(1)

    reps = collect_rep_pages(proj_dir)
    total = 0                # active (non-archived, readable) 대표페이지
    with_sites = 0
    no_sites = []            # project names with zero sites
    site_total = 0
    by_sido = {}             # 시도 -> count of site entries
    sigungu = set()          # (시도, 시군구)
    unmatchable = []         # (project, raw site) whose 시도 token isn't a known 광역
    renderable = 0           # projects with >=1 matchable site
    archived_skipped = 0
    read_errors = []         # (project, error) — excluded from total, not silent

    for name in sorted(reps):
        try:
            text = open(reps[name], encoding="utf-8").read()
        except OSError as e:
            read_errors.append((name, str(e)))
            continue
        fm = parse_frontmatter(text)
        if extract_scalar(fm, "archived") == "true":
            archived_skipped += 1
            continue
        total += 1
        sites = [s for s in (normalize_site(x) for x in extract_sites(fm)) if s]
        if not sites:
            no_sites.append(name)
            continue
        with_sites += 1
        has_match = False
        for s in sites:
            site_total += 1
            toks = s.split(" ")
            if toks[0] in SIDO:
                by_sido[toks[0]] = by_sido.get(toks[0], 0) + 1
                if len(toks) > 1:
                    sigungu.add((toks[0], toks[1]))
                has_match = True
            else:
                unmatchable.append((name, s))
        if has_match:
            renderable += 1

    cov = (with_sites / total * 100) if total else 0
    rcov = (renderable / total * 100) if total else 0

    if as_json:
        print(json.dumps({
            "projects_total": total, "projects_with_sites": with_sites,
            "coverage_pct": round(cov, 1), "renderable_projects": renderable,
            "renderable_pct": round(rcov, 1), "site_entries": site_total,
            "sido_histogram": by_sido, "sigungu_unique": len(sigungu),
            "unmatchable": unmatchable, "projects_without_sites": no_sites,
            "archived_skipped": archived_skipped, "read_errors": read_errors,
        }, ensure_ascii=False, indent=2))
        return

    print("현장(Sites) 커버리지 — %s" % proj_dir)
    print("─" * 52)
    print("활성 대표페이지 총계     : %d  (대표.md 폴더형 + 레거시 flat, archived 제외)" % total)
    print("Sites ≥1 보유            : %d  (%.0f%%)" % (with_sites, cov))
    print("지도 표시 가능(매칭됨)   : %d  (%.0f%%)" % (renderable, rcov))
    print("현장(site) 엔트리 총계    : %d" % site_total)
    print("고유 시군구              : %d" % len(sigungu))
    print("매칭 실패(시도 불명)     : %d" % len(unmatchable))
    print("archived 제외            : %d" % archived_skipped)
    if read_errors:
        print("읽기 실패(집계 제외)     : %d" % len(read_errors))
    if by_sido:
        print("\n시도 분포 (현장 수):")
        mx = max(by_sido.values())
        for k in sorted(by_sido, key=lambda k: -by_sido[k]):
            bar = "█" * max(1, round(by_sido[k] / mx * 24))
            print("  %-4s %3d  %s" % (k, by_sido[k], bar))
    if unmatchable:
        print("\n⚠ 매칭 실패 현장 (미배치로 표시됨):")
        for proj, s in unmatchable[:20]:
            print("  · [%s] %s" % (proj, s))
    if read_errors:
        print("\n⚠ 읽기 실패 대표페이지:")
        for proj, e in read_errors[:20]:
            print("  · [%s] %s" % (proj, e))
    empty = len(no_sites)
    print("\nSites 미기입 프로젝트: %d (%.0f%%)" % (empty, empty / total * 100 if total else 0))
    print("\n판정:", end=" ")
    if total == 0:
        print("활성 프로젝트 없음 — 측정 불가")
    elif renderable >= 8 and rcov >= 40:
        print("밀도 충분 — 지도 구현 가치 있음")
    elif renderable >= 3:
        print("경계선 — 소수만 표시됨, Sites 보강 후 재측정 권장")
    else:
        print("희소 — 지금 만들면 빈 지도. Sites 입력 습관부터")


if __name__ == "__main__":
    main()
