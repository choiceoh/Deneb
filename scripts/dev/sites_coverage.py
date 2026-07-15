#!/usr/bin/env python3
"""Measure 현장(Sites) coverage across wiki project 대표페이지.

Read-only. Answers the go/no-go question for a 현장 지도: how many projects
actually carry a Site, and how do those Sites distribute across 시도/시군구 —
i.e. would a map be densely populated or mostly empty?

Sites live in the YAML frontmatter of top-level 프로젝트/*.md pages as a flow
array `sites: [광역약칭 시/군 읍/면/동, ...]` (see wiki/page.go). This script
mirrors the Go normalizeSiteName province-abbreviation so its 시도 bucketing
matches the gateway's own matching keys.

Usage:
    python3 scripts/dev/sites_coverage.py <wiki-root>      # e.g. ~/.deneb/wiki
    python3 scripts/dev/sites_coverage.py <wiki-root> --json
"""
import sys, os, glob, json, re

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
    """Return the frontmatter block (between the first two '---' lines) or ''."""
    if not text.startswith("---\n") and not text.startswith("---\r\n"):
        return ""
    rest = text.split("---", 2)
    return rest[1] if len(rest) >= 3 else ""


def extract_sites(fm):
    """Extract the sites list from a frontmatter block. Handles the flow form
    `sites: [a, b]` (what Render writes) and a legacy block form."""
    # flow: sites: [a, b, c]
    m = re.search(r"^sites:\s*\[(.*?)\]\s*$", fm, re.M)
    if m:
        inner = m.group(1).strip()
        return [x.strip() for x in inner.split(",") if x.strip()] if inner else []
    # block: sites:\n  - a\n  - b
    m = re.search(r"^sites:\s*$", fm, re.M)
    if m:
        out = []
        for line in fm[m.end():].splitlines():
            b = re.match(r"\s*-\s*(.+)$", line)
            if b:
                out.append(b.group(1).strip())
            elif line.strip() and not line.startswith((" ", "\t")):
                break
        return out
    return []


def extract_scalar(fm, key):
    m = re.search(r"^%s:\s*(.+)$" % re.escape(key), fm, re.M)
    return m.group(1).strip() if m else ""


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

    # top-level 대표페이지 only (sub-pages live in subfolders)
    files = sorted(glob.glob(os.path.join(proj_dir, "*.md")))
    total = len(files)
    with_sites = 0
    no_sites = []            # project names with zero sites
    site_total = 0
    by_sido = {}             # 시도 -> count of site entries
    sigungu = set()          # (시도, 시군구)
    unmatchable = []         # (project, raw site) whose 시도 token isn't a known 광역
    renderable = 0           # projects with >=1 matchable site

    for f in files:
        name = os.path.splitext(os.path.basename(f))[0]
        try:
            text = open(f, encoding="utf-8").read()
        except Exception:
            continue
        fm = parse_frontmatter(text)
        sites = [normalize_site(s) for s in extract_sites(fm)]
        sites = [s for s in sites if s]
        if not sites:
            no_sites.append(name)
            continue
        with_sites += 1
        has_match = False
        for s in sites:
            site_total += 1
            toks = s.split(" ")
            sido = toks[0]
            if sido in SIDO:
                by_sido[sido] = by_sido.get(sido, 0) + 1
                if len(toks) > 1:
                    sigungu.add((sido, toks[1]))
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
        }, ensure_ascii=False, indent=2))
        return

    print("현장(Sites) 커버리지 — %s" % proj_dir)
    print("─" * 52)
    print("프로젝트 대표페이지 총계 : %d" % total)
    print("Sites ≥1 보유            : %d  (%.0f%%)" % (with_sites, cov))
    print("지도 표시 가능(매칭됨)   : %d  (%.0f%%)" % (renderable, rcov))
    print("현장(site) 엔트리 총계    : %d" % site_total)
    print("고유 시군구              : %d" % len(sigungu))
    print("매칭 실패(시도 불명)     : %d" % len(unmatchable))
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
    empty = len(no_sites)
    print("\nSites 미기입 프로젝트: %d (%.0f%%)" % (empty, empty / total * 100 if total else 0))
    # verdict
    print("\n판정:", end=" ")
    if total == 0:
        print("프로젝트 없음 — 측정 불가")
    elif renderable >= 8 and rcov >= 40:
        print("밀도 충분 — 지도 구현 가치 있음")
    elif renderable >= 3:
        print("경계선 — 소수만 표시됨, Sites 보강 후 재측정 권장")
    else:
        print("희소 — 지금 만들면 빈 지도. Sites 입력 습관부터")


if __name__ == "__main__":
    main()
