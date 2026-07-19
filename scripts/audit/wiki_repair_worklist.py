#!/usr/bin/env python3
"""Turn recall-bench misses into a wiki repair worklist.

The 2026-07-19 analysis-path bench showed that residual recall misses are
mostly WIKI problems, not retrieval problems: thin project folders, sibling
folders splitting one site's identity, or pages whose vocabulary never
mentions the words real mail subjects use. This script classifies each missed
gold case into those repair categories so the fix list falls out of the bench
run instead of a manual audit.

Inputs: a wiki COPY, the gold set, and the per-case verbose output of
cmd/recall-bench (-v; grep the ✗ lines into a file). Output: a per-project
worklist, most-missed first, with the dominant diagnosis and suggested action.

Usage:
  gateway-go: go run ./cmd/recall-bench -wiki <copy> -gold <gold> -v | grep ✗ > misses.txt
  scripts/audit/wiki_repair_worklist.py --wiki <copy> --gold <gold> --misses misses.txt
"""

import argparse
import collections
import glob
import json
import os
import re


def load_gold(path):
    cases = {}
    for line in open(path, encoding="utf-8"):
        if line.startswith("#"):
            continue
        d = json.loads(line)
        cases[d["id"]] = d
    return cases


def miss_ids(path):
    ids = []
    for line in open(path, encoding="utf-8"):
        m = re.match(r"\s*✗\s+(\S+)", line)
        if m:
            ids.append(m.group(1))
    return ids


def folder_profile(wiki, folder):
    """Page count and total prose bytes of one project folder."""
    base = os.path.join(wiki, folder)
    pages = glob.glob(os.path.join(base, "**", "*.md"), recursive=True)
    size = sum(os.path.getsize(p) for p in pages)
    return len(pages), size


def content_blob(wiki, folder, cap=200_000):
    blob = []
    total = 0
    for p in glob.glob(os.path.join(wiki, folder, "**", "*.md"), recursive=True):
        try:
            t = open(p, encoding="utf-8", errors="ignore").read()
        except OSError:
            continue
        blob.append(t)
        total += len(t)
        if total > cap:
            break
    return "\n".join(blob).lower().replace(" ", "")


GENERIC_TOKENS = {
    "태양광", "발전소", "프로젝트", "케이블", "모듈", "공장", "에너지", "물류센터",
    "리스사업", "임대사업", "루프탑", "커넥터", "협력", "인버터", "구조물", "및", "솔라",
}


def tokens_overlap(a, b):
    """Two folder-token sets overlap when any pair matches by containment —
    Korean site names inflect (비금 ↔ 비금도), so exact equality misses the
    very siblings this diagnosis exists for."""
    for x in a:
        for y in b:
            if len(x) >= 2 and len(y) >= 2 and (x in y or y in x):
                return True
    return False


def name_tokens(folder):
    toks = re.split(r"[-_(),/]", folder.lower())
    return {t for t in toks if len(t) >= 2 and t not in GENERIC_TOKENS and not t.isdigit()}


def subject_terms(question):
    """Distinctive Korean/latin terms of a mail subject (stopword-light)."""
    q = re.sub(r"^\[[^\]]{1,30}\]\s*", "", question)
    words = re.findall(r"[가-힣]{2,8}|[a-zA-Z]{4,}", q)
    drop = {"송부", "요청", "검토", "관련", "내용", "회신", "자료", "송부의", "드립니다", "부탁드립니다"}
    return [w for w in words if w not in drop][:12]


def classify(wiki, folder, questions, all_folders):
    pages, size = folder_profile(wiki, folder)
    diagnoses = []
    if not os.path.isdir(os.path.join(wiki, folder)):
        diagnoses.append(("missing-folder", "gold 경로가 위키에 없음 — 폴더명 확인/생성"))
    # thin folder: little material for any retrieval signal to grab onto
    elif pages <= 2 or size < 4_000:
        diagnoses.append(("thin-folder", f"{pages}p/{size // 1000}KB"))
    # sibling overlap: other folders share this folder's distinctive tokens
    mine = name_tokens(folder)
    sibs = [f for f in all_folders if f != folder and tokens_overlap(name_tokens(f), mine)]
    if sibs:
        diagnoses.append(("sibling-overlap", ", ".join(s.split("/")[-1] for s in sibs[:3])))
    # vocab gap: the subjects' distinctive terms barely appear in the folder
    blob = content_blob(wiki, folder)
    missing = collections.Counter()
    for q in questions:
        for t in subject_terms(q):
            if t.lower().replace(" ", "") not in blob:
                missing[t] += 1
    gap = [t for t, n in missing.most_common(6) if n >= max(2, len(questions) // 3)]
    if gap:
        diagnoses.append(("vocab-gap", " ".join(gap)))
    return pages, size, diagnoses


ACTION = {
    "missing-folder": "gold 라벨 폴더명 대조 (개명/통합 흔적)",
    "thin-folder": "대표.md 보강 (메일분석 요약 반영)",
    "sibling-overlap": "형제 폴더 병합/parent 링크 검토",
    "vocab-gap": "cues/본문에 실사용 어휘 추가",
}


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--wiki", required=True, help="wiki directory (a COPY of prod)")
    ap.add_argument("--gold", required=True)
    ap.add_argument("--misses", required=True, help="file of ✗ lines from recall-bench -v")
    args = ap.parse_args()

    gold = load_gold(args.gold)
    missed = [gold[i] for i in miss_ids(args.misses) if i in gold]
    by_folder = collections.defaultdict(list)
    totals = collections.Counter(c["gold_paths"][0] for c in gold.values())
    for c in missed:
        by_folder[c["gold_paths"][0]].append(c["question"])

    # sibling candidates come from the WHOLE project roster, not just the
    # missed set — the overlap signal is precisely about folders the miss list
    # does not contain.
    all_folders = sorted(
        "프로젝트/" + d
        for d in os.listdir(os.path.join(args.wiki, "프로젝트"))
        if os.path.isdir(os.path.join(args.wiki, "프로젝트", d))
    )
    print(f"misses: {len(missed)} cases over {len(by_folder)} projects\n")
    for folder, qs in sorted(by_folder.items(), key=lambda kv: -len(kv[1])):
        pages, size, diagnoses = classify(args.wiki, folder, qs, all_folders)
        print(f"== {folder}  ({len(qs)}/{totals[folder]} missed · {pages}p {size // 1000}KB)")
        if not diagnoses:
            print("   진단 없음 — 케이스 라벨 재검토 후보")
        for kind, detail in diagnoses:
            print(f"   [{kind}] {detail}")
            print(f"     → {ACTION[kind]}")


if __name__ == "__main__":
    main()
