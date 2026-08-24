"""Where do our long-horizon runs actually fail?

Three August-2026 papers (StateM arXiv:2608.15089, LongHorizon-Harness
arXiv:2608.01964, OneDayAgent arXiv:2608.05013) independently converged on the
same diagnosis: long-horizon agents fail because task state lives inside the
growing context — and the same prescription (externalized state, an
environment-verified auditor, fresh-context subtasks). Adopting that here means
touching the chat pipeline next to the untouchable prompt-cache path, so the
review verdict was: PROVE THE DEMAND FIRST. This audit is that proof — a
deterministic tagging of every long run in the agent-log window with the
papers' failure taxonomy, so "do we have this disease?" is a number instead of
a hunch.

Failure modes and their log proxies (all deterministic; the honest limits of
each are part of the contract):

- premature-stop — the run was cut off, not finished: stopReason timeout /
  max_tokens / max_turns. `aborted` is tagged separately as user-abort — an
  operator interrupting a run is not the harness failing.
- context-pressure — state demonstrably no longer fits: the run compacted
  mid-flight or truncated tool results. This is the DIRECT symptom of the
  papers' "state lives in context" diagnosis.
- error-churn — ≥5 tool errors in one run: the agent thrashing against the
  environment rather than progressing.
- repeat-churn — one tool called ≥8 times WITH ≥3 of those calls erroring.
  Plain call-count is deliberately not enough: a coding session runs exec
  dozens of times productively, and tagging that as a loop (measured: 61% of
  long runs on the naive proxy) would manufacture demand. Errors on the
  repeats are what separate a loop from a workhorse.

What the log CANNOT see, stated so nobody reads absence as health: goal drift
and false self-assessment (the run "finishing" with the wrong outcome) leave no
deterministic trace in run.end rows — end_turn+clean here means "no mechanical
failure", not "correct". Those two need transcript-level judgment and are out
of scope for this audit.

Read-only and advisory: it never gates and never writes.

Entrypoints: `analyze`, `render`, `main`.
Test: `scripts/audit/test_longrun_failure_taxonomy.py`.
Verify: `make python-test`, `python3 scripts/audit/longrun-failure-taxonomy.py --json`.
"""

from __future__ import annotations

import argparse
import json
import os
import time
from collections import Counter, defaultdict
from dataclasses import dataclass, field
from pathlib import Path

# A run is long-horizon when it spans this many turns or tool calls.
LONG_TURNS = 8
LONG_TOOL_CALLS = 10

# Mode thresholds (see module docstring for why each is shaped this way).
ERROR_CHURN_MIN = 5
REPEAT_CHURN_CALLS = 8
REPEAT_CHURN_ERRORS = 3

PREMATURE_STOPS = frozenset({"timeout", "max_tokens", "max_turns"})

MODE_ORDER = (
    "context-pressure",
    "premature-stop",
    "repeat-churn",
    "error-churn",
    "user-abort",
    "clean",
)


def agent_logs_dir() -> Path:
    override = os.environ.get("DENEB_AGENT_LOGS_DIR", "").strip()
    if override:
        return Path(override)
    return Path.home() / ".deneb" / "agent-logs"


@dataclass
class RunTags:
    session: str
    run_id: str
    turns: int
    stop: str
    modes: list[str] = field(default_factory=list)
    top_tool: str = ""


@dataclass
class Taxonomy:
    window_days: int
    total_runs: int = 0
    long_runs: int = 0
    mode_counts: Counter = field(default_factory=Counter)
    examples: dict[str, list[RunTags]] = field(default_factory=dict)

    def rate(self, mode: str) -> float:
        if self.long_runs <= 0:
            return 0.0
        return self.mode_counts.get(mode, 0) / self.long_runs


def _truncated_count(value: object) -> int:
    """run.end truncatedToolCalls is an int on old rows, a per-tool map on new."""
    if isinstance(value, dict):
        return sum(int(v or 0) for v in value.values())
    try:
        return int(value or 0)
    except (TypeError, ValueError):
        return 0


def _iter_rows(path: Path, cutoff_ms: float):
    try:
        text = path.read_text(encoding="utf-8")
    except OSError:
        return
    for line in text.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            row = json.loads(line)
        except json.JSONDecodeError:
            continue
        if row.get("ts", 0) < cutoff_ms:
            continue
        yield row


def tag_run(end: dict, tool_calls: Counter, tool_errors: Counter) -> list[str]:
    """The taxonomy proper — one run's modes, order-stable, [] never returned."""
    modes: list[str] = []
    stop = str(end.get("stopReason") or "")
    if end.get("compacted") or _truncated_count(end.get("truncatedToolCalls")) > 0:
        modes.append("context-pressure")
    if stop in PREMATURE_STOPS:
        modes.append("premature-stop")
    for tool, calls in tool_calls.items():
        if calls >= REPEAT_CHURN_CALLS and tool_errors.get(tool, 0) >= REPEAT_CHURN_ERRORS:
            modes.append("repeat-churn")
            break
    if sum(tool_errors.values()) >= ERROR_CHURN_MIN:
        modes.append("error-churn")
    if stop == "aborted":
        modes.append("user-abort")
    if not modes:
        modes.append("clean")
    return modes


def analyze(logs: Path | None = None, *, days: int = 28, now_ms: float | None = None) -> Taxonomy:
    root = logs or agent_logs_dir()
    now = now_ms if now_ms is not None else time.time() * 1000
    cutoff = now - days * 86400 * 1000

    out = Taxonomy(window_days=days)
    if not root.is_dir():
        return out
    for path in sorted(root.glob("*.jsonl")):
        session = path.stem
        tool_calls: dict[str, Counter] = defaultdict(Counter)
        tool_errors: dict[str, Counter] = defaultdict(Counter)
        ends: dict[str, dict] = {}
        for row in _iter_rows(path, cutoff):
            kind = row.get("type")
            run_id = str(row.get("runId") or "")
            if kind == "turn.tool":
                data = row.get("data", {})
                name = data.get("name")
                if isinstance(name, str) and name:
                    tool_calls[run_id][name] += 1
                    if data.get("isError"):
                        tool_errors[run_id][name] += 1
            elif kind == "run.end":
                ends[run_id] = row.get("data", {})
        for run_id, end in ends.items():
            out.total_runs += 1
            turns = int(end.get("turns") or 0)
            calls = int(end.get("toolCalls") or 0)
            if turns < LONG_TURNS and calls < LONG_TOOL_CALLS:
                continue
            out.long_runs += 1
            modes = tag_run(end, tool_calls.get(run_id, Counter()), tool_errors.get(run_id, Counter()))
            tc = tool_calls.get(run_id, Counter())
            top_tool = tc.most_common(1)[0][0] if tc else ""
            tags = RunTags(
                session=session,
                run_id=run_id,
                turns=turns,
                stop=str(end.get("stopReason") or ""),
                modes=modes,
                top_tool=top_tool,
            )
            for mode in modes:
                out.mode_counts[mode] += 1
                bucket = out.examples.setdefault(mode, [])
                if len(bucket) < 5:
                    bucket.append(tags)
    return out


def render(tax: Taxonomy) -> str:
    lines = [
        f"DENEB_LONGRUN_FAILURE_TAXONOMY window={tax.window_days}d "
        f"runs={tax.total_runs} long={tax.long_runs} [advisory]"
    ]
    if tax.long_runs <= 0:
        lines.append("  no long-horizon runs in window")
        return "\n".join(lines)
    for mode in MODE_ORDER:
        n = tax.mode_counts.get(mode, 0)
        if n == 0:
            continue
        lines.append(f"  {mode:<17} {n:>4} ({tax.rate(mode):.0%})")
        for ex in tax.examples.get(mode, [])[:3]:
            detail = f"turns={ex.turns} stop={ex.stop}"
            if ex.top_tool:
                detail += f" topTool={ex.top_tool}"
            lines.append(f"    - {ex.session} {detail}")
    lines.append(
        "  NOTE: goal drift / false self-assessment are invisible to run.end "
        "rows — 'clean' means no mechanical failure, not correct"
    )
    return "\n".join(lines)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="long-horizon failure taxonomy (advisory)")
    parser.add_argument("--logs", type=Path, default=None, help="agent-logs dir (default ~/.deneb/agent-logs)")
    parser.add_argument("--days", type=int, default=28)
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args(argv)

    tax = analyze(args.logs, days=args.days)
    if args.json:
        print(
            json.dumps(
                {
                    "windowDays": tax.window_days,
                    "runs": tax.total_runs,
                    "longRuns": tax.long_runs,
                    "modes": {m: tax.mode_counts.get(m, 0) for m in MODE_ORDER if tax.mode_counts.get(m)},
                },
                ensure_ascii=False,
                indent=2,
            )
        )
    else:
        print(render(tax))
    return 0
