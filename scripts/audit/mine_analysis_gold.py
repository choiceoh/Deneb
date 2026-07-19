#!/usr/bin/env python3
"""Mine an analysis-path recall gold set from real mail/approval subjects.

Builds ~/.deneb/wiki-qa-gold-analysis-xl.jsonl: each case is a REAL mail
subject (mailstore + mail_analysis/approval_analysis caches) labeled with the
wiki project folder the analysis pipeline should recall for it. This probes
the dominant production recall path — mail/approval analysis — at scale,
where the hand-written chat-style gold sets (wiki-qa-gold*.jsonl) do not
reach. Consumed by gateway-go/cmd/recall-bench via -gold.

Labeling is deterministic anchor-token matching against project folder names,
with two precision guards learned the hard way (v1-v4, 2026-07-19):

  * corpus-frequency validation — a folder token may anchor a subject alone
    only if it is unique to one project AND rare across the whole subject
    corpus (generic compounds like "태양광발전설비"/"가배치"/"울산" appear in
    hundreds of unrelated subjects and poison every label they touch);
  * single-project constraint — subjects matching >1 project are skipped
    outright rather than guessed.

Residual noise after these guards is sibling-folder ambiguity (one site,
several folders, e.g. 비금 cable vs 비금 solar): spot-check a sample and
either drop the family or list the siblings together in gold_paths (the
bench accepts any listed path as a hit).

Usage:
  scripts/audit/mine_analysis_gold.py --wiki <wiki-copy-dir> \
      [--out ~/.deneb/wiki-qa-gold-analysis-xl.jsonl] [--per-project 25]
"""

import argparse
import collections
import glob
import json
import pathlib
import re

GENERIC = {
    "태양광", "발전소", "프로젝트", "리스사업", "대표", "로그", "회의록", "현장", "공장",
    "에너지", "산업", "주식회사", "자료", "검토", "계약", "공급", "설치", "모듈", "수정",
    "발주", "공사", "구매", "견적", "도면", "보고", "송부", "인버터", "구조물", "전기",
    "연계", "사업", "신규", "관련", "요청", "건", "및", "안전", "점검", "월간", "주간",
    "임대사업", "루프탑", "루프탑태양광", "케이블", "커넥터", "협의", "협력", "조사", "통합",
    "작성", "공유", "배치", "가배치", "주차장", "옥외", "자가소비", "태양광발전설비",
    "육상풍력", "수배전반", "리파워링", "energy",
}


def ok_token(t: str) -> bool:
    if t in GENERIC or len(t) < 2 or t.isdigit():
        return False
    if t in "탑솔라" or t in "topsolar":  # company-name substrings ("솔라"…)
        return False
    if re.fullmatch(r"\d+(mw|kw|km|개소|차|호)?", t):
        return False
    if re.fullmatch(r"[a-z]+", t) and len(t) < 5:
        return False
    return True


def norm(subj: str) -> str:
    s = re.sub(r"^((re|fw|fwd|답장|전달)\s*:\s*)+", "", subj.strip(), flags=re.I)
    return re.sub(r"^\[[^\]]{1,30}\]\s*", "", s).strip()


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--wiki", required=True, help="wiki directory (a COPY of prod)")
    ap.add_argument(
        "--out", default=str(pathlib.Path.home() / ".deneb/wiki-qa-gold-analysis-xl.jsonl")
    )
    ap.add_argument("--per-project", type=int, default=25)
    args = ap.parse_args()

    home = pathlib.Path.home()
    projects: dict[str, list[str]] = {}
    for p in (pathlib.Path(args.wiki) / "프로젝트").iterdir():
        if not p.is_dir():
            continue
        toks = [t for t in re.split(r"[-_(),]", p.name.lower()) if ok_token(t)]
        if toks:
            projects[f"프로젝트/{p.name}"] = toks
    tok_owners: dict[str, set[str]] = collections.defaultdict(set)
    for proj, toks in projects.items():
        for t in toks:
            tok_owners[t].add(proj)

    subjects: list[tuple[str, str, str]] = []
    seen: set[str] = set()

    def collect(src: str, title: str) -> None:
        t = norm(title)
        if not t or len(t) < 8 or not re.search(r"[가-힣]", t):
            return
        key = t.lower()
        if key in seen:
            return
        seen.add(key)
        subjects.append((src, title.strip(), key.replace(" ", "")))

    for f in sorted(glob.glob(str(home / ".deneb/mailstore/messages/*.jsonl"))):
        for line in open(f, encoding="utf-8", errors="ignore"):
            try:
                collect("mail", json.loads(line).get("subject") or "")
            except (json.JSONDecodeError, AttributeError):
                continue
    for cache_dir, src, field in (
        ("mail_analysis", "manl", "subject"),
        ("approval_analysis", "appr", "title"),
    ):
        for f in glob.glob(str(home / f".deneb/cache/{cache_dir}/*.json")):
            if f.endswith(".lock"):
                continue
            try:
                collect(src, json.load(open(f)).get(field) or "")
            except (json.JSONDecodeError, OSError, AttributeError):
                continue

    freq: collections.Counter = collections.Counter()
    for _, _, s in subjects:
        for t in tok_owners:
            if t in s:
                freq[t] += 1

    def anchor_ok(t: str) -> bool:
        return len(tok_owners[t]) == 1 and len(t) >= 3 and freq[t] <= 40

    def match(s: str) -> str | None:
        hits = []
        for proj, toks in projects.items():
            found = [t for t in toks if t in s]
            if not found:
                continue
            if any(anchor_ok(t) for t in found) or (
                len(found) >= 2 and all(freq[t] <= 150 for t in found)
            ):
                hits.append(proj)
        return hits[0] if len(hits) == 1 else None

    per_proj: dict[str, int] = {}
    cases = []
    for src, title, s in subjects:
        proj = match(s)
        if not proj or per_proj.get(proj, 0) >= args.per_project:
            continue
        per_proj[proj] = per_proj.get(proj, 0) + 1
        cases.append(
            {
                "id": f"an-{src}-{len(cases)}",
                "category": f"analysis-{src}",
                "question": title,
                "gold_paths": [proj],
                "must_contain": [],
                "must_not": [],
            }
        )

    with open(args.out, "w", encoding="utf-8") as out:
        out.write(
            "# Analysis-path XL gold — mined from real mail/approval subjects;"
            " spot-check before trusting.\n"
        )
        for c in cases:
            out.write(json.dumps(c, ensure_ascii=False) + "\n")
    print(f"{len(cases)} cases over {len(per_proj)} projects -> {args.out}")


if __name__ == "__main__":
    main()
