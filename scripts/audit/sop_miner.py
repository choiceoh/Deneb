"""SOP miner — recurring successful action flows into composite-tool candidates.

RSI 2026H2 addendum, second pass #3 (EvoSOP arXiv 2607.07321 + Tool-Making in
Low-Latency Systems 2607.08010 + Metis 2606.24151): repeated multi-step
procedures observed in real transcripts are worth promoting into a validated
composite tool — but ONLY when reuse frequency justifies the build cost
(Metis' promotion rule). This miner detects contiguous action-level n-grams
that recur across sessions above a frequency floor and files each as a
propose-only ``scope=code`` self-correction candidate over the miniapp RPC.
Tool results, user-turn boundaries, and bounded action fields keep failed
retries and unrelated work out of the promotion evidence.

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

# Tool-result messages use role=user too, so only a real user request creates
# this separator. It is not a valid normalized tool step.
TURN_BOUNDARY = "<turn>"

# Agent-control plumbing is not part of a reusable business procedure. Keeping
# it transparent lets a future no-fetch path match historical traces.
CONTROL_PLANE_TOOLS = frozenset({"fetch_tools", "process", "read_spillover"})
ACTION_KEYS = ("action", "op", "operation", "method")
COMMAND_ACTION_TOOLS = frozenset({"office"})
ACTION_VALUE_RE = re.compile(r"^[a-z0-9][a-z0-9_.-]{0,39}$")

_RISK_NOTE = (
    "제안 전용입니다. 합성 도구는 오류가 나면 개별 호출로 되돌아가야 하며, 어떤 "
    "도구의 권한 표면도 넓히면 안 됩니다."
)


def _input_object(block: dict[str, Any]) -> dict[str, Any]:
    raw = block.get("input")
    if isinstance(raw, dict):
        return raw
    if not isinstance(raw, str):
        return {}
    try:
        decoded = json.loads(raw)
    except (json.JSONDecodeError, TypeError, ValueError):
        return {}
    return decoded if isinstance(decoded, dict) else {}


def normalize_tool_step(block: dict[str, Any]) -> str:
    """Return ``tool.action`` when the invocation exposes a bounded action."""
    name = str(block.get("name") or "").strip().lower()
    if not name or name in CONTROL_PLANE_TOOLS:
        return ""
    payload = _input_object(block)
    action = ""
    for key in ACTION_KEYS:
        value = payload.get(key)
        if isinstance(value, str):
            candidate = value.strip().lower().replace(" ", "_")
            if ACTION_VALUE_RE.fullmatch(candidate):
                action = candidate
                break
    if not action and name in COMMAND_ACTION_TOOLS:
        value = payload.get("command")
        if isinstance(value, str):
            candidate = value.strip().lower().replace(" ", "_")
            if ACTION_VALUE_RE.fullmatch(candidate):
                action = candidate
    return f"{name}.{action}" if action else name


def _tool_result_succeeded(block: dict[str, Any]) -> bool:
    if block.get("is_error") is True:
        return False
    content = block.get("content")
    if not isinstance(content, str):
        return True
    text = content.lstrip().lower()
    return not text.startswith(("error:", "<error>", "tool error:", "failed:"))


def _is_real_user_message(content: Any) -> bool:
    if isinstance(content, str):
        return bool(content.strip())
    if not isinstance(content, list):
        return False
    return any(
        not isinstance(block, dict) or block.get("type") != "tool_result"
        for block in content
    )


def _collapse_step_runs(steps: list[str]) -> list[str]:
    """Represent repeated identical calls as one explicit batch opportunity."""
    out: list[str] = []
    i = 0
    while i < len(steps):
        j = i + 1
        while j < len(steps) and steps[j] == steps[i]:
            j += 1
        out.append(steps[i] + ("[]" if j - i > 1 else ""))
        i = j
    return out


def extract_tool_sequence(path: str, cutoff_ms: int) -> list[str]:
    """Successful normalized tool steps, separated by real user turns.

    Tolerates corrupt lines (same stance as the JSONL ledger readers). A
    transcript whose newest message predates cutoff yields [] so stale history
    does not manufacture demand. Failed or missing tool results split a flow;
    they are not evidence for a procedure worth compiling.
    """
    messages: list[dict[str, Any]] = []
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
                if isinstance(msg, dict):
                    messages.append(msg)
    except OSError:
        return []
    if newest and newest < cutoff_ms:
        return []

    seq: list[str] = []
    calls: list[tuple[str, str]] = []
    results: dict[str, bool] = {}

    def append_chunk(chunk: list[str]) -> None:
        collapsed = _collapse_step_runs([step for step in chunk if step])
        if not collapsed:
            return
        if seq and seq[-1] != TURN_BOUNDARY:
            seq.append(TURN_BOUNDARY)
        seq.extend(collapsed)

    def flush_calls() -> None:
        chunk: list[str] = []
        for call_id, step in calls:
            if results.get(call_id) is True:
                if step:
                    chunk.append(step)
                continue
            # Missing/failed results break continuity; do not join stable
            # steps on either side into a flow that never actually ran.
            append_chunk(chunk)
            chunk = []
        append_chunk(chunk)
        calls.clear()
        results.clear()

    for msg in messages:
        role = msg.get("role")
        content = msg.get("content")
        if role == "user":
            if isinstance(content, list):
                for block in content:
                    if not isinstance(block, dict) or block.get("type") != "tool_result":
                        continue
                    call_id = str(block.get("tool_use_id") or "").strip()
                    if call_id:
                        results[call_id] = _tool_result_succeeded(block)
            if _is_real_user_message(content):
                flush_calls()
            continue
        if role != "assistant" or not isinstance(content, list):
            continue
        for block in content:
            if not isinstance(block, dict) or block.get("type") != "tool_use":
                continue
            call_id = str(block.get("id") or "").strip()
            calls.append((call_id, normalize_tool_step(block)))
    flush_calls()
    while seq and seq[-1] == TURN_BOUNDARY:
        seq.pop()
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
    """Action n-grams recurring above the frequency gate, value-first.

    A gram already contained in a selected longer gram is suppressed — the
    longer procedure subsumes it (greedy by length desc, count desc, then
    lexicographic for determinism).
    """
    counts: dict[tuple[str, ...], int] = {}
    sessions: dict[tuple[str, ...], set[str]] = {}
    for key, seq in sequences.items():
        segments: list[list[str]] = [[]]
        for step in seq:
            if step == TURN_BOUNDARY:
                segments.append([])
            else:
                segments[-1].append(step)
        for segment in segments:
            for n in range(SOP_MIN_LEN, SOP_MAX_LEN + 1):
                for i in range(len(segment) - n + 1):
                    gram = tuple(segment[i : i + n])
                    tools = {step.split(".", 1)[0].removesuffix("[]") for step in gram}
                    semantic_steps = sum("." in step for step in gram)
                    if len(tools) < 2 or len(set(gram)) < 2 or semantic_steps < 2:
                        continue
                    counts[gram] = counts.get(gram, 0) + 1
                    sessions.setdefault(gram, set()).add(key)

    eligible = [
        g for g, c in counts.items()
        if c >= SOP_MIN_OCCURRENCES and len(sessions[g]) >= SOP_MIN_SESSIONS
    ]
    def minimum_calls_saved(gram: tuple[str, ...]) -> int:
        raw_calls = sum(2 if step.endswith("[]") else 1 for step in gram)
        return max(0, raw_calls - 1)

    # Select longest flows first so a high-frequency prefix cannot prevent its
    # complete procedure from suppressing the redundant shorter candidate.
    eligible.sort(key=lambda g: (-len(g), -counts[g], g))
    selected: list[tuple[str, ...]] = []
    for gram in eligible:
        joined = "\x00".join(gram)
        if any("\x00".join(s).find(joined) >= 0 for s in selected):
            continue  # subsumed by an already-selected longer procedure
        selected.append(gram)

    # Present the surviving procedures by conservative observed savings. This
    # affects prioritization only; subsumption above remains longest-first.
    selected.sort(key=lambda g: (
        -(len(sessions[g]) * minimum_calls_saved(g)),
        -len(sessions[g]),
        -counts[g],
        -len(g),
        g,
    ))

    out: list[dict[str, Any]] = []
    for gram in selected:
        flow = " > ".join(gram)
        digest = hashlib.sha256(flow.encode()).hexdigest()[:12]
        slug = re.sub(r"[^a-z0-9]+", "_", "_".join(gram).lower()).strip("_")[:40]
        # One replay per independent session is a defensible lower bound even
        # when repeated windows inside a session overlap.
        saved = len(sessions[gram]) * minimum_calls_saved(gram)
        out.append({
            "scope": "code",
            "skillName": "sop-mining",
            "title": f"절차 후보(SOP): {flow}",
            "candidate": (
                f"반복 도구 시퀀스 [{flow}]를 하나의 합성 도구로 승격 — 다단계 "
                "로직을 재사용해 턴 왕복과 실패율을 줄인다 (EvoSOP/Tool-Making 패턴)."
            ),
            "evidence": (
                f"observed {counts[gram]} successful action-normalized flows across "
                f"{len(sessions[gram])} sessions within {WINDOW_DAYS}d; compiling "
                f"would avoid at least {saved} tool calls over the observed uses "
                f"(deterministic transcript mining; gram sha {digest})"
            ),
            "reason": "recurring multi-step procedure above the Metis frequency "
                      "gate — proactive L4 supply (RSI 2026H2 second pass #3)",
            "targetFiles": [f"gateway-go/internal/pipeline/chat/tools/{slug}.go"],
            "proposedChange": (
                "gateway-go/CLAUDE.md 의 'Adding a New Agent Tool' 절차대로 이 시퀀스를 "
                "감싸는 합성 도구를 추가하고(스키마 + 핸들러 + toolreg 등록), 이 액션 "
                "흐름 그대로의 리플레이 픽스처도 함께 만든다. 승격 전에 프롬프트/컴플리션 "
                "토큰·도구 호출 수·지연·오류를 측정한다. 어느 단계든 오류가 나면 개별 "
                "호출로 되돌아가야 한다."
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
                        help="client token (reads ~/.deneb/client_token if unset)")
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

    # Token-file fallback + URL normalization mirror health_finding_miner so a
    # bare invocation authenticates the same way across the audit miners.
    token = args.token
    if not token:
        token_file = os.path.expanduser("~/.deneb/client_token")
        if os.path.exists(token_file):
            with open(token_file, encoding="utf-8") as handle:
                token = handle.read().strip()
    base_url = args.url.rstrip("/")

    now_ms = int(time.time() * 1000)
    cutoff_ms = now_ms - WINDOW_DAYS * 24 * 60 * 60 * 1000
    sequences = collect_sequences(args.transcripts, cutoff_ms)
    candidates = mine_sops(sequences)

    if args.dry_run:
        print(json.dumps({"sessions": len(sequences), "mined": candidates},
                         ensure_ascii=False, indent=1), file=out)
        return 0

    try:
        existing = fetch_existing(base_url, token)
        selected, skipped = select_candidates(candidates, existing, now_ms, args.max)
        filed = [
            {"id": record_candidate(base_url, token, cand), "source": cand["source"]}
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
