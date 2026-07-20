#!/usr/bin/env python3
"""Mine use-confirmed recall gold from the recall-utility ledger.

The recall ledger (#4050 retrieval context, #4055 observed use) records every
inject with its real user query and every read/cite that proves the page was
actually USED. A (query → page) pair whose page the answer then cited is a
real-traffic gold case at zero annotation cost — the compounding gold source
the hand-written sets lack. Young today (the ledger started 2026-07-20);
rerun weekly and the set grows with real usage.

Join: inject(query, path, session) ← cite/read(path, session) within
--use-window-min. Dedupe by normalized query; skip queries under 8 runes.

Usage:
  scripts/audit/mine_traffic_gold.py [--ledger ~/.deneb/wiki/.recall-hits.jsonl]
      [--out ~/.deneb/wiki-qa-gold-traffic.jsonl]
"""

from __future__ import annotations

import argparse
import collections
import json
import pathlib
import re


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    home = pathlib.Path.home()
    ap.add_argument("--ledger", default=str(home / ".deneb/wiki/.recall-hits.jsonl"))
    ap.add_argument("--out", default=str(home / ".deneb/wiki-qa-gold-traffic.jsonl"))
    ap.add_argument("--use-window-min", type=int, default=30)
    args = ap.parse_args()

    injects, uses = [], []
    ledger = pathlib.Path(args.ledger).expanduser()
    if ledger.is_file():
        for line in ledger.read_text(encoding="utf-8").splitlines():
            line = line.strip()
            if not line:
                continue
            try:
                row = json.loads(line)
            except json.JSONDecodeError:
                continue
            kind = row.get("event", "inject") or "inject"
            (injects if kind == "inject" else uses).append(row)

    window_ms = args.use_window_min * 60 * 1000
    used_keys = {(u.get("session", ""), u.get("path", "")): int(u.get("at", 0)) for u in uses}

    cases, seen = [], set()
    for row in injects:
        query = (row.get("query") or "").strip()
        path = row.get("path", "")
        # Anchor sentinels (project-anchor 등) are not user phrasing.
        if len(query) < 8 or not re.search(r"[가-힣]", query) or query.endswith("-anchor"):
            continue
        use_at = used_keys.get((row.get("session", ""), path))
        if use_at is None or not 0 <= use_at - int(row.get("at", 0)) <= window_ms:
            continue
        key = query.lower().replace(" ", "")
        if key in seen:
            continue
        seen.add(key)
        cases.append(
            {
                "id": f"tr-{len(cases)}",
                "category": "traffic-use",
                "question": query,
                "gold_paths": [path],
                "must_contain": [],
            }
        )

    out = pathlib.Path(args.out).expanduser()
    with out.open("w", encoding="utf-8") as f:
        f.write("# Use-confirmed real-traffic gold — mined by scripts/audit/mine_traffic_gold.py\n")
        for c in cases:
            f.write(json.dumps(c, ensure_ascii=False) + "\n")
    by_kind = collections.Counter(c["category"] for c in cases)
    print(f"{len(cases)} cases -> {out} {dict(by_kind)} (inject={len(injects)} use={len(uses)})")


if __name__ == "__main__":
    main()
