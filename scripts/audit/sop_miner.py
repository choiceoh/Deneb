"""SOP miner — recurring tool-call sequences into composite-tool L4 candidates.

RSI 2026H2 addendum, second pass #3 (EvoSOP arXiv 2607.07321 + Tool-Making in
Low-Latency Systems 2607.08010 + Metis 2606.24151): repeated multi-step
procedures observed in real transcripts are worth promoting into a validated
composite tool — but ONLY when reuse frequency justifies the build cost
(Metis' promotion rule). This miner detects contiguous tool-call n-grams that
recur across sessions above a frequency floor and files each as a propose-only
``scope=code`` self-correction candidate over the miniapp RPC.

Design decision: SCRIPTS-SIDE miner (same rationale recorded in
``health_finding_miner.py``) — the input (transcript files) lives outside the
serving process and the tracker stays the queue's single writer via the RPC.
The RPC edge, reopen semantics, and per-run cap are imported from that module
so the two miners cannot drift.

The ``sop-mining`` source is NOT in the coding-dispatch allowlist: candidates
stage for review per the graduation ladder, exactly like runtime-error did.
Deterministic; no LLM anywhere.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import sys
import time
from typing import Any, TextIO

from health_finding_miner import (
    DEFAULT_GATEWAY_URL,
    GatewayError,
    fetch_existing,
    record_candidate,
    select_candidates,
)

DEFAULT_TRANSCRIPT_DIR = os.path.expanduser("~/.deneb/transcripts")
SOURCE_PREFIX = "sop-mining"

# Frequency gate (Metis promotion rule): a sequence must recur enough, across
# enough distinct sessions, before the build cost of a composite tool is
# justified. A gram must also span >=2 distinct tools — a single tool called
# repeatedly is a loop, not a procedure.
SOP_MIN_LEN = 3
SOP_MAX_LEN = 6
SOP_MIN_OCCURRENCES = 5
SOP_MIN_SESSIONS = 3
MAX_PER_RUN = 2
WINDOW_DAYS = 30
MAX_TRANSCRIPTS = 400

_RISK_NOTE = (
    "propose-only; composite tool must fall back to the individual calls on "
    "error and never widen any tool's permission surface"
)


def extract_tool_sequence(path: str, cutoff_ms: int) -> list[str]:
    """Ordered tool names from one transcript's assistant tool_use blocks.

    Tolerates corrupt lines (same stance as the JSONL ledger readers). A
    transcript whose newest message predates cutoff yields [] so stale history
    does not manufacture demand.
    """
    seq: list[str] = []
    newest = 0
    try:
        with open(path, encoding="utf-8", errors="replace") as handle:
            for line in handle:
                line = line.strip()
                if not line:
                    continue
                try:
                    msg = json.loads(line)
                except json.JSONDecodeError:
                    continue
                ts = msg.get("timestamp")
                if isinstance(ts, (int, float)):
                    newest = max(newest, int(ts))
                if msg.get("role") != "assistant":
                    continue
                content = msg.get("content")
                if not isinstance(content, list):
                    continue
                for block in content:
                    if isinstance(block, dict) and block.get("type") == "tool_use":
                        name = str(block.get("name") or "").strip()
                        if name:
                            seq.append(name)
    except OSError:
        return []
    if newest and newest < cutoff_ms:
        return []
    return seq


def collect_sequences(transcript_dir: str, cutoff_ms: int) -> dict[str, list[str]]:
    """session key -> tool sequence, newest transcripts first, bounded."""
    try:
        names = [n for n in os.listdir(transcript_dir) if n.endswith(".jsonl")]
    except OSError:
        return {}
    paths = [os.path.join(transcript_dir, n) for n in names]
    paths.sort(key=lambda p: os.path.getmtime(p) if os.path.exists(p) else 0, reverse=True)
    out: dict[str, list[str]] = {}
    for path in paths[:MAX_TRANSCRIPTS]:
        seq = extract_tool_sequence(path, cutoff_ms)
        if len(seq) >= SOP_MIN_LEN:
            out[os.path.basename(path)[: -len(".jsonl")]] = seq
    return out


def mine_sops(sequences: dict[str, list[str]]) -> list[dict[str, Any]]:
    """Contiguous n-grams recurring above the frequency gate, longest-first.

    A gram already contained in a selected longer gram is suppressed — the
    longer procedure subsumes it (greedy by length desc, count desc, then
    lexicographic for determinism).
    """
    counts: dict[tuple[str, ...], int] = {}
    sessions: dict[tuple[str, ...], set[str]] = {}
    for key, seq in sequences.items():
        for n in range(SOP_MIN_LEN, SOP_MAX_LEN + 1):
            for i in range(len(seq) - n + 1):
                gram = tuple(seq[i : i + n])
                if len(set(gram)) < 2:
                    continue  # a repeated single tool is a loop, not a procedure
                counts[gram] = counts.get(gram, 0) + 1
                sessions.setdefault(gram, set()).add(key)

    eligible = [
        g for g, c in counts.items()
        if c >= SOP_MIN_OCCURRENCES and len(sessions[g]) >= SOP_MIN_SESSIONS
    ]
    eligible.sort(key=lambda g: (-len(g), -counts[g], g))
    selected: list[tuple[str, ...]] = []
    for gram in eligible:
        joined = "\x00".join(gram)
        if any("\x00".join(s).find(joined) >= 0 for s in selected):
            continue  # subsumed by an already-selected longer procedure
        selected.append(gram)

    out: list[dict[str, Any]] = []
    for gram in selected:
        flow = " > ".join(gram)
        digest = hashlib.sha256(flow.encode()).hexdigest()[:12]
        slug = re.sub(r"[^a-z0-9]+", "_", "_".join(gram).lower()).strip("_")[:40]
        out.append({
            "scope": "code",
            "skillName": "sop-mining",
            "title": f"SOP candidate: {flow}",
            "candidate": (
                f"반복 도구 시퀀스 [{flow}]를 하나의 합성 도구로 승격 — 다단계 "
                "로직을 재사용해 턴 왕복과 실패율을 줄인다 (EvoSOP/Tool-Making 패턴)."
            ),
            "evidence": (
                f"observed {counts[gram]}x across {len(sessions[gram])} sessions "
                f"within {WINDOW_DAYS}d (deterministic transcript n-gram mining; "
                f"gram sha {digest})"
            ),
            "reason": "recurring multi-step procedure above the Metis frequency "
                      "gate — proactive L4 supply (RSI 2026H2 second pass #3)",
            "targetFiles": [f"gateway-go/internal/pipeline/chat/tools/{slug}.go"],
            "proposedChange": (
                "Add a composite tool wrapping the sequence per gateway-go/CLAUDE.md "
                "'Adding a New Agent Tool' (schema + handler + toolreg registration); "
                "it must fall back to the individual calls on any step error."
            ),
            "risk": _RISK_NOTE,
            "source": f"{SOURCE_PREFIX}:{digest}",
        })
    return out


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Deneb SOP miner (RSI L4 supply).")
    parser.add_argument("--transcripts", default=DEFAULT_TRANSCRIPT_DIR,
                        help="transcript directory (default ~/.deneb/transcripts)")
    parser.add_argument("--url", default=os.environ.get("DENEB_GATEWAY_URL", DEFAULT_GATEWAY_URL),
                        help="gateway base URL (env DENEB_GATEWAY_URL)")
    parser.add_argument("--token", default=os.environ.get("DENEB_CLIENT_TOKEN", ""),
                        help="client token (env DENEB_CLIENT_TOKEN)")
    parser.add_argument("--max", type=int, default=MAX_PER_RUN,
                        help=f"per-run filing cap (default {MAX_PER_RUN})")
    parser.add_argument("--dry-run", action="store_true",
                        help="mine and print; file nothing")
    parser.add_argument("--json", action="store_true", help="machine-readable summary")
    return parser


def main(argv: list[str] | None = None, stdout: TextIO | None = None,
         stderr: TextIO | None = None) -> int:
    args = _parser().parse_args(argv)
    out = stdout if stdout is not None else sys.stdout
    err = stderr if stderr is not None else sys.stderr

    now_ms = int(time.time() * 1000)
    cutoff_ms = now_ms - WINDOW_DAYS * 24 * 60 * 60 * 1000
    sequences = collect_sequences(args.transcripts, cutoff_ms)
    candidates = mine_sops(sequences)

    if args.dry_run:
        print(json.dumps({"sessions": len(sequences), "mined": candidates},
                         ensure_ascii=False, indent=1), file=out)
        return 0

    try:
        existing = fetch_existing(args.url, args.token)
        selected, skipped = select_candidates(candidates, existing, now_ms, args.max)
        filed = [
            {"id": record_candidate(args.url, args.token, cand), "source": cand["source"]}
            for cand in selected
        ]
    except GatewayError as exc:
        print(f"sop-miner: {exc}", file=err)
        return 1

    summary = {
        "sessions": len(sequences),
        "mined": len(candidates),
        "filed": filed,
        "skipped": [{"source": c["source"], "reason": r} for c, r in skipped],
    }
    if args.json:
        print(json.dumps(summary, ensure_ascii=False, indent=1), file=out)
    else:
        print(f"sop-miner: {len(filed)} filed, {len(summary['skipped'])} skipped "
              f"({len(candidates)} mined from {len(sequences)} sessions)", file=out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
