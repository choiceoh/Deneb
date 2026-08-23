"""Production runtime-health scoring from Deneb gateway journald logs.

This module is stdlib-only and importable for deterministic parser/scoring
tests. The user-facing CLI remains ``scripts/audit/runtime-health.py``.
"""

from __future__ import annotations

import argparse
import json
import math
import re
import subprocess
import sys
from dataclasses import dataclass, field
from datetime import date
from typing import Iterable, TextIO

DEFAULT_UNIT = "deneb-gateway"
DEFAULT_SINCE = "7 days ago"

# --- honest bars (what a healthy runtime achieves) --------------------------- #
# Rates are per-run unless noted; graded(value, soft, hard): full credit at/under
# soft, zero at/over hard.
CRASH_PER_DAY_HARD = 1.0
ERR_PER_RUN_SOFT, ERR_PER_RUN_HARD = 0.05, 0.5
# llm-serving scores on HARD faults only (work lost: prose-instead-of-JSON parse
# aborts, malformed stream chunks, retry-exhaustion). Transient 429 retries that
# recover are NOT faults — their only cost is delay, already paid in `latency`.
LLM_HARD_PER_RUN_SOFT, LLM_HARD_PER_RUN_HARD = 0.02, 0.25
TIMEOUT_FRAC_SOFT, TIMEOUT_FRAC_HARD = 0.01, 0.10
TOOLERR_FRAC_SOFT, TOOLERR_FRAC_HARD = 0.01, 0.05
LAT_P95_SOFT, LAT_P95_HARD = 60.0, 240.0
TOP_N = 12

# --- log signal patterns ----------------------------------------------------- #
RUN_RE = re.compile(r"agent loop complete")
AGENTMS_RE = re.compile(r"\bagentMs=(\d+)")
LLMMS_RE = re.compile(r"\bllmMs=(\d+)")
TOOLMS_RE = re.compile(r"\btoolMs=(\d+)")
TURNS_RE = re.compile(r"\bturns=(\d+)")
TOOLCALLS_RE = re.compile(r"\btotalToolCalls=(\d+)")
TOOLERR_RE = re.compile(r"tool complete .*isError=true")
CRASH_RE = re.compile(r"panic:|fatal error|SIGSEGV|runtime\.error")
LLM_HARD_RE = re.compile(
    r"unmarshal LLM|invalid JSON|unparseable|parse verdicts|"
    r"unexpected end of JSON|verdict call failed|"
    r"giving up|request failed after|all retries exhausted"
)
LLM_RETRY_RE = re.compile(r"retrying LLM request|API error 429")
# MCP child processes log their own stderr through the gateway verbatim — pino
# JSON, npm notices, multi-line node stack traces, even third-party telemetry
# beacons (posthog DNS failures). One child event becomes dozens of journal
# lines, so counting them as gateway operational errors both double-counts and
# blames Deneb for another process's logging. MCP failures that actually cost
# work still land in tool-reliability as `tool complete … isError=true`.
MCP_STDERR_RE = re.compile(r"mcp server stderr\b")
ERR_LINE_RE = re.compile(r'''error=(?!<nil>|null|"")|\bfailed\b(?!=)''')

ISO_DAY_RE = re.compile(r"^\s*(\d{4})-(\d{2})-(\d{2})(?:[T\s]|$)")
ENGLISH_DAY_RE = re.compile(
    r"^\s*(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+(\d{1,2})\s+\d{2}:\d{2}:\d{2}\b"
)
KOREAN_DAY_RE = re.compile(r"^\s*(\d{1,2})\uc6d4\s+(\d{1,2})(?:\uc77c)?\s+\d{2}:\d{2}:\d{2}\b")
MONTHS = {
    month: index
    for index, month in enumerate(
        ("Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"),
        start=1,
    )
}


class InsufficientDataError(ValueError):
    """Raised when the minimum completed-run signals do not exist to score."""


def graded(value: float, soft: float, hard: float) -> float:
    if value <= soft:
        return 1.0
    if value >= hard:
        return 0.0
    return (hard - value) / (hard - soft)


@dataclass
class DimResult:
    name: str
    weight: int
    score: float
    detail: list[str] = field(default_factory=list)
    extra: dict = field(default_factory=dict)


@dataclass
class Signals:
    runs: int = 0
    timeout_runs: int = 0
    agent_ms: list[int] = field(default_factory=list)
    # Stage decomposition of slow runs (llmMs/toolMs ride the same run-complete
    # record; older journals lack them, so coverage is tracked separately).
    stage_samples: list[tuple[int, int, int]] = field(default_factory=list)
    turns: list[int] = field(default_factory=list)
    tool_calls: int = 0
    tool_call_reports: int = 0
    tool_errors: int = 0
    crashes: int = 0
    llm_hard: int = 0
    llm_retries: int = 0
    other_errors: int = 0
    mcp_stderr_lines: int = 0
    days: float = 7.0
    err_examples: dict = field(default_factory=dict)
    llm_examples: dict = field(default_factory=dict)


def journal_data_lines(lines: Iterable[str]) -> list[str]:
    """Drop journalctl boilerplate that is not a gateway log record."""

    data: list[str] = []
    for raw in lines:
        line = raw.rstrip("\n")
        stripped = line.strip()
        if not stripped:
            continue
        if stripped.startswith("-- ") and stripped.endswith(" --"):
            continue
        if stripped.startswith("Hint: ") or stripped.startswith("No journal files were found"):
            continue
        data.append(line)
    return data



def latency_stage_detail(samples: list[tuple[int, int, int]]) -> list[str]:
    """Human lines attributing where slow-run time goes (advisory — the latency
    SCORE stays a pure agentMs p95 grade so baselines do not churn).

    Stage fields (llmMs/toolMs) ride the run-complete record since 2026-07-20;
    older journal windows simply have no samples and the breakdown is omitted.
    The slow cohort is the top decile by agentMs so the attribution describes
    the p95 problem, not the healthy median.
    """
    if not samples:
        return ["stage breakdown: n/a (no llmMs/toolMs fields in window)"]
    ranked = sorted(samples, key=lambda t: -t[0])
    slow = ranked[: max(1, len(ranked) // 10)]
    agent = sum(t[0] for t in slow)
    llm = sum(t[1] for t in slow)
    tool = sum(t[2] for t in slow)
    other = max(0, agent - llm - tool)
    if agent <= 0:
        return ["stage breakdown: n/a (zero agentMs in slow cohort)"]
    return [
        "slow-cohort stage breakdown (top 10% by agentMs, "
        f"{len(slow)}/{len(samples)} sampled runs): "
        f"llm {100.0 * llm / agent:.0f}% · tools {100.0 * tool / agent:.0f}% · "
        f"other {100.0 * other / agent:.0f}%"
    ]


def latency_stage_extra(samples: list[tuple[int, int, int]]) -> dict:
    """Machine fields for the latency dimension extra dict (same cohort as
    latency_stage_detail; empty when the window has no stage samples)."""
    if not samples:
        return {}
    ranked = sorted(samples, key=lambda t: -t[0])
    slow = ranked[: max(1, len(ranked) // 10)]
    agent = sum(t[0] for t in slow)
    if agent <= 0:
        return {}
    llm = sum(t[1] for t in slow)
    tool = sum(t[2] for t in slow)
    return {
        "stage_samples": len(samples),
        "slow_cohort": len(slow),
        "slow_llm_share": round(llm / agent, 3),
        "slow_tool_share": round(tool / agent, 3),
        "slow_other_share": round(max(0.0, 1.0 - (llm + tool) / agent), 3),
    }


def parse(lines: Iterable[str]) -> Signals:
    s = Signals()
    for raw in lines:
        line = raw.rstrip("\n")
        if RUN_RE.search(line):
            s.runs += 1
            match = AGENTMS_RE.search(line)
            if match:
                agent_ms = int(match.group(1))
                s.agent_ms.append(agent_ms)
                llm = LLMMS_RE.search(line)
                tool = TOOLMS_RE.search(line)
                if llm and tool:
                    s.stage_samples.append((agent_ms, int(llm.group(1)), int(tool.group(1))))
            match = TURNS_RE.search(line)
            if match:
                s.turns.append(int(match.group(1)))
            match = TOOLCALLS_RE.search(line)
            if match:
                s.tool_calls += int(match.group(1))
                s.tool_call_reports += 1
            if "stopReason=timeout" in line:
                s.timeout_runs += 1
            continue
        if MCP_STDERR_RE.search(line):
            s.mcp_stderr_lines += 1
            continue
        if TOOLERR_RE.search(line):
            s.tool_errors += 1
            continue
        if CRASH_RE.search(line):
            s.crashes += 1
            continue
        if LLM_HARD_RE.search(line):
            s.llm_hard += 1
            key = _err_key(line)
            s.llm_examples[key] = s.llm_examples.get(key, 0) + 1
            continue
        if LLM_RETRY_RE.search(line):
            s.llm_retries += 1
            continue
        if ERR_LINE_RE.search(line) and "isError=false" not in line and "toolErrors=" not in line:
            s.other_errors += 1
            key = _err_key(line)
            s.err_examples[key] = s.err_examples.get(key, 0) + 1
    return s


def _err_key(line: str) -> str:
    body = line.split("│", 1)[-1].strip()
    body = re.split(r"\s+[A-Za-z_][\w.]*=", body, maxsplit=1)[0].strip()
    return (body or line.strip())[:56]


def percentile(values: list[int], q: float) -> float:
    """Nearest-rank percentile (q=0.95 selects rank ceil(0.95*n))."""

    if not values:
        return 0.0
    ordered = sorted(values)
    if q <= 0:
        return ordered[0]
    if q >= 1:
        return ordered[-1]
    rank = max(1, math.ceil(len(ordered) * q))
    return ordered[rank - 1]


# Backward-compatible private name for callers that used the original script.
_p = percentile


def compute(s: Signals) -> tuple[float, list[DimResult]]:
    if s.runs <= 0:
        raise InsufficientDataError("0 completed runs")
    if not s.agent_ms:
        raise InsufficientDataError(f"0 agentMs samples for {s.runs} completed runs")

    runs = s.runs
    days = max(s.days, 0.5)

    crashes_per_day = s.crashes / days
    err_per_run = s.other_errors / runs
    llm_hard_per_run = s.llm_hard / runs
    timeout_frac = s.timeout_runs / runs
    toolerr_frac = s.tool_errors / max(s.tool_calls, 1)
    p95 = percentile(s.agent_ms, 0.95) / 1000.0
    tool_coverage = min(1.0, s.tool_call_reports / runs)
    latency_coverage = min(1.0, len(s.agent_ms) / runs)

    def top(examples):
        return [f"{n:>3}x  {key}" for key, n in sorted(examples.items(), key=lambda item: -item[1])[:TOP_N]]

    dims = [
        DimResult(
            "stability",
            18,
            100.0 * graded(crashes_per_day, 0.0, CRASH_PER_DAY_HARD),
            [f"{s.crashes} crash/panic in {days:.1f}d = {crashes_per_day:.2f}/day"],
            {"crashes": s.crashes},
        ),
        DimResult(
            "error-rate",
            16,
            100.0 * graded(err_per_run, ERR_PER_RUN_SOFT, ERR_PER_RUN_HARD),
            [f"{s.other_errors} operational errors / {runs} runs = {err_per_run:.2f}/run"]
            + top(s.err_examples)
            + (
                [
                    f"{s.mcp_stderr_lines} mcp child stderr lines passed through "
                    "(not scored — MCP failures that cost work are tool errors)"
                ]
                if s.mcp_stderr_lines
                else []
            ),
            {
                "errors": s.other_errors,
                "per_run": round(err_per_run, 3),
                "mcp_stderr_lines": s.mcp_stderr_lines,
            },
        ),
        DimResult(
            "llm-serving",
            16,
            100.0 * graded(llm_hard_per_run, LLM_HARD_PER_RUN_SOFT, LLM_HARD_PER_RUN_HARD),
            [
                f"{s.llm_hard} hard faults (work lost) / {runs} runs = {llm_hard_per_run:.2f}/run",
                f"{s.llm_retries} transient retries (recovered — cost is in latency, not scored)",
            ]
            + top(s.llm_examples),
            {"llm_hard": s.llm_hard, "llm_retries": s.llm_retries, "per_run": round(llm_hard_per_run, 3)},
        ),
        DimResult(
            "turn-reliability",
            16,
            100.0 * graded(timeout_frac, TIMEOUT_FRAC_SOFT, TIMEOUT_FRAC_HARD),
            [f"{s.timeout_runs}/{runs} runs timed out = {timeout_frac * 100:.1f}%"],
            {"timeout_runs": s.timeout_runs, "frac": round(timeout_frac, 4)},
        ),
        DimResult(
            "tool-reliability",
            14,
            100.0 * graded(toolerr_frac, TOOLERR_FRAC_SOFT, TOOLERR_FRAC_HARD) * tool_coverage,
            [
                f"{s.tool_errors}/{s.tool_calls} tool calls errored = {toolerr_frac * 100:.1f}%",
                f"totalToolCalls reported by {s.tool_call_reports}/{runs} runs = {tool_coverage * 100:.1f}% coverage",
            ],
            {
                "tool_errors": s.tool_errors,
                "tool_calls": s.tool_calls,
                "reported_runs": s.tool_call_reports,
                "coverage": round(tool_coverage, 4),
            },
        ),
        DimResult(
            "latency",
            20,
            100.0 * graded(p95, LAT_P95_SOFT, LAT_P95_HARD) * latency_coverage,
            [
                f"run agentMs p95 = {p95:.0f}s (p50 {percentile(s.agent_ms, 0.5) / 1000:.0f}s, "
                f"max {percentile(s.agent_ms, 1.0) / 1000:.0f}s) over {len(s.agent_ms)}/{runs} runs "
                f"({latency_coverage * 100:.1f}% coverage)"
            ]
            + latency_stage_detail(s.stage_samples),
            {
                "p95_s": round(p95, 1),
                "sampled_runs": len(s.agent_ms),
                "coverage": round(latency_coverage, 4),
                **latency_stage_extra(s.stage_samples),
            },
        ),
    ]
    denominator = sum(dim.weight for dim in dims)
    composite = sum(dim.score * dim.weight for dim in dims) / denominator if denominator else 0.0
    return round(composite, 1), dims


def _valid_month_day(month: int, day: int) -> tuple[int, int] | None:
    try:
        date(2000, month, day)
    except ValueError:
        return None
    return month, day


def _parse_journal_day(line: str) -> date | tuple[int, int] | None:
    match = ISO_DAY_RE.match(line)
    if match:
        try:
            return date(int(match.group(1)), int(match.group(2)), int(match.group(3)))
        except ValueError:
            return None
    match = ENGLISH_DAY_RE.match(line)
    if match:
        return _valid_month_day(MONTHS[match.group(1)], int(match.group(2)))
    match = KOREAN_DAY_RE.match(line)
    if match:
        return _valid_month_day(int(match.group(1)), int(match.group(2)))
    return None


def _nearby_day(month: int, day: int, reference: date, forward: bool) -> date:
    candidates: list[date] = []
    for year in range(reference.year - 4, reference.year + 5):
        try:
            candidates.append(date(year, month, day))
        except ValueError:
            continue
    if forward:
        return min(candidates, key=lambda candidate: (candidate < reference, abs((candidate - reference).days)))
    return min(candidates, key=lambda candidate: (candidate > reference, abs((candidate - reference).days)))


def _journal_days(lines: Iterable[str], reference: date | None = None) -> list[date]:
    tokens = [token for line in lines if (token := _parse_journal_day(line)) is not None]
    if not tokens:
        return []

    explicit_anchor = next(
        ((index, token) for index, token in enumerate(tokens) if isinstance(token, date)),
        None,
    )
    if explicit_anchor is None:
        anchor_index = 0
        month, day = tokens[anchor_index]
        anchor = _nearby_day(month, day, reference or date.today(), forward=False)
    else:
        anchor_index, anchor = explicit_anchor

    resolved: list[date | None] = [None] * len(tokens)
    resolved[anchor_index] = anchor

    previous = anchor
    for index in range(anchor_index + 1, len(tokens)):
        token = tokens[index]
        current = token if isinstance(token, date) else _nearby_day(token[0], token[1], previous, forward=True)
        resolved[index] = current
        previous = current

    following = anchor
    for index in range(anchor_index - 1, -1, -1):
        token = tokens[index]
        current = token if isinstance(token, date) else _nearby_day(token[0], token[1], following, forward=False)
        resolved[index] = current
        following = current

    return [day for day in resolved if day is not None]


def span_days(lines: Iterable[str], reference: date | None = None) -> float:
    """Inclusive calendar-day span for ISO, English, or Korean journal prefixes.

    ``reference`` resolves the year omitted by localized journal timestamps. It
    defaults to today and is injectable so boundary tests remain deterministic.
    """

    days = _journal_days(lines, reference)
    if not days:
        return 1.0
    return float(max(1, (max(days) - min(days)).days + 1))


_span_days = span_days


def _read_journal(since: str, unit: str, stderr: TextIO | None = None) -> list[str]:
    err_stream = stderr if stderr is not None else sys.stderr
    try:
        result = subprocess.run(
            ["journalctl", "--user", "-u", unit, "--no-pager", "--output=short-iso", "--since", since],
            capture_output=True,
            text=True,
            timeout=120,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        print(f"runtime-health: journalctl failed: {exc}", file=err_stream)
        return []
    if result.returncode != 0:
        detail = result.stderr.strip() or f"exit {result.returncode}"
        print(f"runtime-health: journalctl failed: {detail}", file=err_stream)
        return []
    return result.stdout.splitlines()


def print_summary(composite: float, dims: list[DimResult], meta: dict, stream: TextIO | None = None) -> None:
    output = stream if stream is not None else sys.stderr
    print(
        f"\nruntime-health  composite = {composite:.1f}/100  "
        f"({meta['runs']} runs over ~{meta['days']:.1f}d)",
        file=output,
    )
    print("─" * 64, file=output)
    for dim in dims:
        print(f"  {dim.name:<17} {dim.score:5.1f}  (w={dim.weight})", file=output)
        for line in dim.detail[:6]:
            print(f"      · {line}", file=output)
        if len(dim.detail) > 6:
            print(f"      … +{len(dim.detail) - 6} more", file=output)
    print("─" * 64, file=output)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Deneb runtime-health score from gateway logs.")
    parser.add_argument("--json", action="store_true")
    parser.add_argument("--stdin", action="store_true", help="read journal lines from stdin")
    parser.add_argument("--since", default=DEFAULT_SINCE)
    parser.add_argument("--unit", default=DEFAULT_UNIT)
    return parser


def main(
    argv: list[str] | None = None,
    *,
    stdin: TextIO | None = None,
    stdout: TextIO | None = None,
    stderr: TextIO | None = None,
) -> int:
    args = _parser().parse_args(argv)
    input_stream = stdin if stdin is not None else sys.stdin
    output_stream = stdout if stdout is not None else sys.stdout
    error_stream = stderr if stderr is not None else sys.stderr

    raw_lines = input_stream.read().splitlines() if args.stdin else _read_journal(args.since, args.unit, error_stream)
    lines = journal_data_lines(raw_lines)
    signals = parse(lines)
    if signals.runs == 0:
        print(
            f"runtime-health: insufficient data: 0 completed runs in {len(lines)} journal lines",
            file=error_stream,
        )
        return 2

    signals.days = span_days(lines)
    try:
        composite, dims = compute(signals)
    except InsufficientDataError as exc:
        print(
            f"runtime-health: insufficient data: {exc} in {len(lines)} journal lines",
            file=error_stream,
        )
        return 2
    meta = {"runs": signals.runs, "days": signals.days, "since": args.since}

    if args.json:
        print(
            json.dumps(
                {
                    "composite": composite,
                    "meta": meta,
                    "dims": {dim.name: round(dim.score, 1) for dim in dims},
                    "weights": {dim.name: dim.weight for dim in dims},
                    "detail": {dim.name: dim.detail for dim in dims},
                    "extra": {dim.name: dim.extra for dim in dims},
                },
                ensure_ascii=False,
                indent=1,
            ),
            file=output_stream,
        )
    else:
        print_summary(composite, dims, meta, error_stream)

    print(f"metric_value={composite}", file=output_stream)
    print("DENEB_RUNTIME_DETAIL " + " ".join(f"{dim.name}={dim.score:.1f}" for dim in dims), file=output_stream)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
