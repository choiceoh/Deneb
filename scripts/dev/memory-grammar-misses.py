#!/usr/bin/env python3
"""Turn the direct-memory miss ledger into candidate axis work.

The gateway records every turn that was shaped like a memory command
("기억해줘 …", "앞으로 …", "remember …") but bound to no axis in
`gateway-go/internal/domain/memory/direct_grammar.json`. Those rows are the only
evidence of what the agent silently failed to remember — but a ledger nobody
reads is just a log. This groups them so a person (or the improvement loop) can
see which phrasings recur and decide what the catalog should learn.

Read-only: it opens the ledger and the catalog and writes nothing.

Entrypoints: `normalize`, `nearest_axes`, `group_rows`, `main`.
Tests: `scripts/dev/test_memory_grammar_misses.py` (`make python-test`).

Usage:
    python3 scripts/dev/memory-grammar-misses.py            # default ledger
    python3 scripts/dev/memory-grammar-misses.py --path P   # another ledger
    python3 scripts/dev/memory-grammar-misses.py --json     # machine-readable
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import unicodedata
from collections import Counter
from dataclasses import dataclass, field
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
CATALOG = REPO_ROOT / "gateway-go/internal/domain/memory/direct_grammar.json"

# Openings the gateway strips off when it looks for the payload. Removing them
# here too makes "기억해줘. 회의는 화요일" and "앞으로 회의는 화요일" group
# together — the axis they are asking for is the same one.
LEAD_PATTERN = re.compile(
    r"^\s*(?:기억\s*(?:해줘요|해줘|해\s*둬|해)|앞으로(?:는)?|다음부터|정정(?:할게(?:요)?)?|"
    r"수정(?:할게(?:요)?)?|아니(?:야|요)?|please\s+remember|remember|from\s+now\s+on)"
    r"[\s,.:;!?，、。！？-]*",
    re.IGNORECASE,
)
TRAILING_REMEMBER = re.compile(r"\s*기억\s*(?:해줘요|해줘|해\s*둬|해)\s*[.!?。！？]*\s*$")
# Values vary per utterance; the shape does not. Numbers and quoted payloads are
# what differ between "9시" and "10시", so blanking them clusters the request.
DIGITS = re.compile(r"\d+")
QUOTED = re.compile(r"[\"“'‘][^\"”'’]{1,40}[\"”'’]")


def default_ledger_path() -> Path:
    state = os.environ.get("DENEB_STATE_DIR") or os.path.expanduser("~/.deneb")
    return Path(state) / "data" / "memory_grammar_misses.jsonl"


def load_axes(catalog: Path | None = None) -> list[dict]:
    path = catalog or CATALOG
    if not path.exists():
        return []
    return json.loads(path.read_text(encoding="utf-8")).get("axes", [])


def normalize(message: str) -> str:
    text = unicodedata.normalize("NFKC", message).strip()
    text = LEAD_PATTERN.sub("", text)
    text = TRAILING_REMEMBER.sub("", text)
    text = QUOTED.sub("〈값〉", text)
    text = DIGITS.sub("N", text)
    return " ".join(text.split())


def nearest_axes(message: str, axes: list[dict], limit: int = 2) -> list[str]:
    """Name axes whose published vocabulary already appears in the message.

    A hit means the catalog is close — usually the axis needs one more phrasing,
    not a new axis. No hit means the request is about something the fact plane
    does not model yet, which is a different (and larger) decision.
    """
    lowered = message.lower()
    scored: list[tuple[int, str]] = []
    for axis in axes:
        hits = sum(1 for alias in axis.get("queryAliases", []) if alias.lower() in lowered)
        if hits:
            scored.append((hits, axis.get("key", "")))
    scored.sort(key=lambda item: (-item[0], item[1]))
    return [key for _, key in scored[:limit] if key]


@dataclass
class Group:
    shape: str
    count: int = 0
    leads: Counter = field(default_factory=Counter)
    targets: Counter = field(default_factory=Counter)
    samples: list[str] = field(default_factory=list)
    near: list[str] = field(default_factory=list)


def group_rows(rows: list[dict], axes: list[dict]) -> list[Group]:
    groups: dict[str, Group] = {}
    for row in rows:
        message = str(row.get("message", "")).strip()
        if not message:
            continue
        shape = normalize(message) or message
        group = groups.setdefault(shape, Group(shape=shape))
        group.count += 1
        group.leads[str(row.get("lead", "")) or "?"] += 1
        group.targets[str(row.get("target", "")) or "?"] += 1
        if len(group.samples) < 3 and message not in group.samples:
            group.samples.append(message)
        if not group.near:
            group.near = nearest_axes(message, axes)
    return sorted(groups.values(), key=lambda g: (-g.count, g.shape))


def read_ledger(path: Path) -> tuple[list[dict], int]:
    rows: list[dict] = []
    skipped = 0
    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            parsed = json.loads(line)
        except json.JSONDecodeError:
            skipped += 1
            continue
        if isinstance(parsed, dict):
            rows.append(parsed)
        else:
            skipped += 1
    return rows, skipped


def render(groups: list[Group], rows: int, skipped: int, path: Path, limit: int) -> list[str]:
    lines = [f"직접 기억 미탐 {rows}건 · 묶음 {len(groups)}개 · {path}"]
    if skipped:
        lines.append(f"  (읽을 수 없는 줄 {skipped}개 건너뜀)")
    if not groups:
        lines.append("  기록된 미탐 없음.")
        return lines
    for index, group in enumerate(groups[:limit], start=1):
        leads = ", ".join(f"{lead}×{count}" for lead, count in group.leads.most_common())
        lines.append("")
        lines.append(f"{index}. ({group.count}회) {group.shape}")
        lines.append(f"   lead: {leads}")
        if group.near:
            lines.append(
                f"   가까운 축: {', '.join(group.near)} → 그 축에 표현을 추가하면 잡힐 가능성"
            )
        else:
            lines.append("   가까운 축 없음 → 새 축이 필요한지부터 판단할 것")
        for sample in group.samples:
            lines.append(f"   · {sample}")
    if len(groups) > limit:
        lines.append("")
        lines.append(f"… {len(groups) - limit}개 묶음 생략 (--limit 로 조정)")
    return lines


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="direct-memory miss ledger report")
    parser.add_argument("--path", type=Path, default=None, help="ledger path")
    parser.add_argument("--limit", type=int, default=20, help="groups to print")
    parser.add_argument("--json", action="store_true", help="machine-readable output")
    args = parser.parse_args(sys.argv[1:] if argv is None else argv)

    path = args.path or default_ledger_path()
    if not path.exists():
        # An absent ledger is the healthy default, not an error: it means no
        # command-shaped turn has gone unbound since the file was last cleared.
        if args.json:
            print(json.dumps({"path": str(path), "rows": 0, "groups": []}, ensure_ascii=False))
        else:
            print(f"미탐 장부 없음: {path} (직접 기억 명령이 전부 축에 묶였다는 뜻)")
        return 0

    rows, skipped = read_ledger(path)
    groups = group_rows(rows, load_axes())

    if args.json:
        print(json.dumps({
            "path": str(path),
            "rows": len(rows),
            "skipped": skipped,
            "groups": [
                {
                    "shape": group.shape,
                    "count": group.count,
                    "leads": dict(group.leads),
                    "targets": dict(group.targets),
                    "nearestAxes": group.near,
                    "samples": group.samples,
                }
                for group in groups[: args.limit]
            ],
        }, ensure_ascii=False, indent=2))
        return 0

    print("\n".join(render(groups, len(rows), skipped, path, args.limit)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
