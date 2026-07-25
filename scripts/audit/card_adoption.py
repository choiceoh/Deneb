"""deneb-ui card adoption rate from the gateway journal.

The card authoring contract says a substantive answer — data, a comparison, a
list, a status summary — should ship as a ```deneb-ui card rather than plain
prose. `denebui.ReportCardHealth` already instruments both sides per turn:

- "deneb-ui card authored"  — the turn shipped a card (numerator)
- "deneb-ui adoption miss"  — the turn shipped a structured-LOOKING answer with
  no card (denominator's other half; heuristic — a markdown table, or >=5
  bullets in a >=400-rune answer)

Those two Info lines make adoption a real ratio, but reading it took an ad-hoc
journalctl grep, so the number was not reproducible between sessions. This
audit is that grep, fixed.

Read-only and advisory: it never gates and never writes. The miss signal is a
heuristic by construction, so treat the SPLIT between session classes as the
signal, not the absolute rate.

Session classes matter. Automated lanes (mail analysis, digests, letters) and
the operator's own conversation have very different baselines, and mixing them
hides the one that matters: measured 2026-07-25 over 14 days, the automated
mailpoll lane sat at 80.6% while the operator-facing session sat at 27.6%.
Dedicated log-like sessions (`client:main:dream`) are reported separately —
they are a background journal the operator opens deliberately, not a surface
that should be card-shaped.

Usage:
    python3 scripts/audit/card-adoption.py [--since "14 days ago"] [--json]
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from collections import defaultdict
from typing import TextIO

AUTHORED = "deneb-ui card authored"
MISS = "deneb-ui adoption miss"
REJECTED = "deneb-ui card rejected before delivery"

_ANSI = re.compile(r"\x1b\[[0-9;]*m")
_SESSION = re.compile(r"session=(\S+)")

# Sessions that are background journals rather than an answering surface.
_LOG_SESSION_SUFFIXES = (":dream",)


def _classify(session: str) -> str:
    """Bucket a session key into operator / automated / log."""
    if any(session.endswith(sfx) for sfx in _LOG_SESSION_SUFFIXES):
        return "log"
    if session.startswith("client:"):
        return "operator"
    return "automated"


def read_journal(since: str, unit: str, stderr: TextIO | None = None) -> list[str]:
    err = stderr if stderr is not None else sys.stderr
    try:
        out = subprocess.run(
            ["journalctl", "--user", "-u", unit, "--no-pager", "--output=cat", "--since", since],
            capture_output=True,
            text=True,
            check=False,
            timeout=180,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        print(f"card-adoption: journalctl failed: {exc}", file=err)
        return []
    if out.returncode != 0:
        print(f"card-adoption: journalctl exited {out.returncode}", file=err)
        return []
    return out.stdout.splitlines()


def tally(lines: list[str]) -> dict[str, dict[str, int]]:
    """Count authored / miss / rejected per session key."""
    per: dict[str, dict[str, int]] = defaultdict(lambda: {"authored": 0, "miss": 0, "rejected": 0})
    for raw in lines:
        line = _ANSI.sub("", raw)
        if AUTHORED in line:
            field = "authored"
        elif MISS in line:
            field = "miss"
        elif REJECTED in line:
            field = "rejected"
        else:
            continue
        m = _SESSION.search(line)
        per[m.group(1) if m else "(unknown)"][field] += 1
    return dict(per)


def summarize(per: dict[str, dict[str, int]]) -> dict:
    groups: dict[str, dict[str, int]] = defaultdict(lambda: {"authored": 0, "miss": 0, "rejected": 0})
    for session, counts in per.items():
        g = groups[_classify(session)]
        for k, v in counts.items():
            g[k] += v

    def rate(c: dict[str, int]) -> float | None:
        total = c["authored"] + c["miss"]
        return round(100.0 * c["authored"] / total, 1) if total else None

    return {
        "groups": {k: {**v, "adoption_pct": rate(v)} for k, v in sorted(groups.items())},
        "sessions": {
            k: {**v, "adoption_pct": rate(v)}
            for k, v in sorted(per.items(), key=lambda kv: -(kv[1]["authored"] + kv[1]["miss"]))
        },
    }


def render(summary: dict, since: str, top: int) -> str:
    out: list[str] = [f"deneb-ui card adoption  (journal since {since!r})", "─" * 64]
    groups = summary["groups"]
    if not groups:
        out.append("  no card-health records in the window")
        return "\n".join(out)
    for name in ("operator", "automated", "log"):
        g = groups.get(name)
        if not g:
            continue
        pct = "n/a" if g["adoption_pct"] is None else f"{g['adoption_pct']:.1f}%"
        rej = f" · rejected {g['rejected']}" if g["rejected"] else ""
        out.append(f"  {name:<10} authored {g['authored']:>4} · miss {g['miss']:>4} → {pct}{rej}")
    out.append("─" * 64)
    out.append("  per session (by volume)")
    for session, c in list(summary["sessions"].items())[:top]:
        pct = "n/a" if c["adoption_pct"] is None else f"{c['adoption_pct']:.1f}%"
        out.append(f"    {session:<34} {c['authored']:>4}/{c['authored'] + c['miss']:<5} {pct}")
    op = groups.get("operator", {}).get("adoption_pct")
    out.append("─" * 64)
    out.append(f"DENEB_CARD_ADOPTION operator={op if op is not None else 'n/a'}")
    return "\n".join(out)


def _parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument("--since", default="14 days ago")
    p.add_argument("--unit", default="deneb-gateway")
    p.add_argument("--top", type=int, default=10, help="per-session rows to show")
    p.add_argument("--json", action="store_true", help="emit JSON instead of text")
    p.add_argument("--stdin", action="store_true", help="read journal lines from stdin")
    return p


def main(argv: list[str] | None = None, stream: TextIO | None = None) -> int:
    args = _parser().parse_args(argv)
    out = stream if stream is not None else sys.stdout
    lines = sys.stdin.read().splitlines() if args.stdin else read_journal(args.since, args.unit)
    summary = summarize(tally(lines))
    if args.json:
        print(json.dumps(summary, ensure_ascii=False, indent=2), file=out)
    else:
        print(render(summary, args.since, args.top), file=out)
    return 0
