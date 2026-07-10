#!/usr/bin/env python3
"""runtime-health.py — production runtime-health score from the last N days of
gateway logs. Sibling to codebase-health.py: that scores STATIC structure, this
scores the LIVE runtime — crashes, operational error rate, LLM-serving health,
turn/tool reliability, and latency — parsed from the gateway's journald log
(`journalctl --user -u deneb-gateway`).

Each bar sits at what a HEALTHY runtime achieves (not tuned to a target), so a
well-behaving deployment lands high honestly and the score still discriminates
and points at the real operational gaps (e.g. glm-5.2 latency, 429s during a
serving cutover, prose-instead-of-JSON parse failures).

Unlike codebase-health this reads a ROLLING window of live logs, so it is not
deterministic and is NOT a ratchet CI gate — it is an on-host observability
report. Run it on the production host (srv4).

Stdlib only. Usage:
  scripts/audit/runtime-health.py                 # human summary + metric line
  scripts/audit/runtime-health.py --json          # full breakdown
  scripts/audit/runtime-health.py --since '3 days ago'
  journalctl --user -u deneb-gateway --since '7 days ago' | scripts/audit/runtime-health.py --stdin

Metric contract (last stdout lines):
  metric_value=71.3
  DENEB_RUNTIME_DETAIL stability=100.0 error-rate=58.1 … latency=46.1
"""
from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from dataclasses import dataclass, field

DEFAULT_UNIT = "deneb-gateway"
DEFAULT_SINCE = "7 days ago"

# --- honest bars (what a healthy runtime achieves) --------------------------- #
# Rates are per-run unless noted; graded(value, soft, hard): full credit at/under
# soft, zero at/over hard.
CRASH_PER_DAY_HARD = 1.0        # any crash/day is serious
ERR_PER_RUN_SOFT, ERR_PER_RUN_HARD = 0.05, 0.5        # non-LLM operational errors
# llm-serving scores on HARD faults only (work lost: prose-instead-of-JSON parse
# aborts, malformed stream chunks, retry-exhaustion). Transient 429 retries that
# recover are NOT faults — their only cost is delay, already paid in `latency`.
LLM_HARD_PER_RUN_SOFT, LLM_HARD_PER_RUN_HARD = 0.02, 0.25
TIMEOUT_FRAC_SOFT, TIMEOUT_FRAC_HARD = 0.01, 0.10     # runs ending in timeout
TOOLERR_FRAC_SOFT, TOOLERR_FRAC_HARD = 0.01, 0.05     # tool calls returning error
LAT_P95_SOFT, LAT_P95_HARD = 60.0, 240.0              # run agentMs p95, seconds
TOP_N = 12

# --- log signal patterns ----------------------------------------------------- #
RUN_RE = re.compile(r"agent loop complete")
AGENTMS_RE = re.compile(r"\bagentMs=(\d+)")
TURNS_RE = re.compile(r"\bturns=(\d+)")
TOOLCALLS_RE = re.compile(r"\btotalToolCalls=(\d+)")
TOOLERR_RE = re.compile(r"tool complete .*isError=true")
CRASH_RE = re.compile(r"panic:|fatal error|SIGSEGV|runtime\.error")
# LLM-serving HARD faults — the LLM returned unusable output and work was lost:
# prose instead of JSON (parse aborts), malformed SSE chunks, retry-exhaustion.
LLM_HARD_RE = re.compile(
    r"unmarshal LLM|invalid JSON|unparseable|parse verdicts|"
    r"unexpected end of JSON|verdict call failed|"
    r"giving up|request failed after|all retries exhausted")
# LLM-serving transient pressure — a request was retried (usually a 429 under
# burst load) and later recovered. Informational; not a fault, cost is in latency.
LLM_RETRY_RE = re.compile(r"retrying LLM request|API error 429")
# Non-LLM operational error line. `error=<something real>` (not <nil>/null/empty)
# or a bare "failed" status word — but NOT a "failed=0" counter field in a
# summary line, and not the "isError=false" tool-complete lines.
ERR_LINE_RE = re.compile(r"""error=(?!<nil>|null|"")|\bfailed\b(?!=)""")


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
    turns: list[int] = field(default_factory=list)
    tool_calls: int = 0
    tool_errors: int = 0
    crashes: int = 0
    llm_hard: int = 0
    llm_retries: int = 0
    other_errors: int = 0
    days: float = 7.0
    err_examples: dict = field(default_factory=dict)
    llm_examples: dict = field(default_factory=dict)


def parse(lines) -> Signals:
    s = Signals()
    for raw in lines:
        line = raw.rstrip("\n")
        if RUN_RE.search(line):
            s.runs += 1
            m = AGENTMS_RE.search(line)
            if m:
                s.agent_ms.append(int(m.group(1)))
            m = TURNS_RE.search(line)
            if m:
                s.turns.append(int(m.group(1)))
            m = TOOLCALLS_RE.search(line)
            if m:
                s.tool_calls += int(m.group(1))
            if "stopReason=timeout" in line:
                s.timeout_runs += 1
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
        if LLM_RETRY_RE.search(line):        # transient, recovers — not a fault
            s.llm_retries += 1
            continue
        if ERR_LINE_RE.search(line) and "isError=false" not in line and "toolErrors=" not in line:
            s.other_errors += 1
            key = _err_key(line)
            s.err_examples[key] = s.err_examples.get(key, 0) + 1
    return s


def _err_key(line: str) -> str:
    # collapse a log line to its human message: take the text after the "│"
    # column and drop the structured key=val tail (error=, dest=, provider=…).
    body = line.split("│", 1)[-1].strip()
    body = re.split(r"\s+[A-Za-z_][\w.]*=", body, maxsplit=1)[0].strip()
    return (body or line.strip())[:56]


def _p(values: list[int], q: float) -> float:
    if not values:
        return 0.0
    v = sorted(values)
    return v[min(len(v) - 1, int(len(v) * q))]


def compute(s: Signals) -> tuple[float, list[DimResult]]:
    runs = max(s.runs, 1)
    days = max(s.days, 0.5)

    crashes_per_day = s.crashes / days
    err_per_run = s.other_errors / runs
    llm_hard_per_run = s.llm_hard / runs
    timeout_frac = s.timeout_runs / runs
    toolerr_frac = s.tool_errors / max(s.tool_calls, 1)
    p95 = _p(s.agent_ms, 0.95) / 1000.0

    def top(examples):
        return [f"{n:>3}x  {k}" for k, n in sorted(examples.items(), key=lambda kv: -kv[1])[:TOP_N]]

    dims = [
        DimResult("stability", 18, 100.0 * graded(crashes_per_day, 0.0, CRASH_PER_DAY_HARD),
                  [f"{s.crashes} crash/panic in {days:.1f}d = {crashes_per_day:.2f}/day"],
                  {"crashes": s.crashes}),
        DimResult("error-rate", 16, 100.0 * graded(err_per_run, ERR_PER_RUN_SOFT, ERR_PER_RUN_HARD),
                  [f"{s.other_errors} operational errors / {runs} runs = {err_per_run:.2f}/run"]
                  + top(s.err_examples),
                  {"errors": s.other_errors, "per_run": round(err_per_run, 3)}),
        DimResult("llm-serving", 16, 100.0 * graded(llm_hard_per_run, LLM_HARD_PER_RUN_SOFT, LLM_HARD_PER_RUN_HARD),
                  [f"{s.llm_hard} hard faults (work lost) / {runs} runs = {llm_hard_per_run:.2f}/run",
                   f"{s.llm_retries} transient retries (recovered — cost is in latency, not scored)"]
                  + top(s.llm_examples),
                  {"llm_hard": s.llm_hard, "llm_retries": s.llm_retries, "per_run": round(llm_hard_per_run, 3)}),
        DimResult("turn-reliability", 16, 100.0 * graded(timeout_frac, TIMEOUT_FRAC_SOFT, TIMEOUT_FRAC_HARD),
                  [f"{s.timeout_runs}/{runs} runs timed out = {timeout_frac * 100:.1f}%"],
                  {"timeout_runs": s.timeout_runs, "frac": round(timeout_frac, 4)}),
        DimResult("tool-reliability", 14, 100.0 * graded(toolerr_frac, TOOLERR_FRAC_SOFT, TOOLERR_FRAC_HARD),
                  [f"{s.tool_errors}/{s.tool_calls} tool calls errored = {toolerr_frac * 100:.1f}%"],
                  {"tool_errors": s.tool_errors, "tool_calls": s.tool_calls}),
        DimResult("latency", 20, 100.0 * graded(p95, LAT_P95_SOFT, LAT_P95_HARD),
                  [f"run agentMs p95 = {p95:.0f}s (p50 {_p(s.agent_ms, 0.5) / 1000:.0f}s, "
                   f"max {_p(s.agent_ms, 1.0) / 1000:.0f}s) over {len(s.agent_ms)} runs"],
                  {"p95_s": round(p95, 1)}),
    ]
    den = sum(d.weight for d in dims)
    composite = sum(d.score * d.weight for d in dims) / den if den else 0.0
    return round(composite, 1), dims


def _span_days(lines: list[str]) -> float:
    # crude: count distinct YYYY-MM-DD-ish day stamps ("MM DD" Korean journal prefix).
    days = set()
    for ln in lines:
        m = re.match(r"\s*\d{1,2}\S*\s+(\d{1,2})\S*\s+\d{2}:\d{2}:\d{2}", ln) \
            or re.match(r"\s*(\d{1,2})\S*\s+\d{1,2}\S*", ln)
        if m:
            days.add(m.group(0)[:6])
    return max(len(days), 1)


def _read_journal(since: str, unit: str) -> list[str]:
    try:
        out = subprocess.run(
            ["journalctl", "--user", "-u", unit, "--no-pager", "--since", since],
            capture_output=True, text=True, timeout=120)
    except (OSError, subprocess.TimeoutExpired) as exc:
        print(f"runtime-health: journalctl failed: {exc}", file=sys.stderr)
        return []
    return out.stdout.splitlines()


def print_summary(composite: float, dims: list[DimResult], meta: dict) -> None:
    print(f"\nruntime-health  composite = {composite:.1f}/100  "
          f"({meta['runs']} runs over ~{meta['days']:.1f}d)", file=sys.stderr)
    print("─" * 64, file=sys.stderr)
    for d in dims:
        print(f"  {d.name:<17} {d.score:5.1f}  (w={d.weight})", file=sys.stderr)
        for line in d.detail[:6]:
            print(f"      · {line}", file=sys.stderr)
        if len(d.detail) > 6:
            print(f"      … +{len(d.detail) - 6} more", file=sys.stderr)
    print("─" * 64, file=sys.stderr)


def main() -> int:
    ap = argparse.ArgumentParser(description="Deneb runtime-health score from gateway logs.")
    ap.add_argument("--json", action="store_true")
    ap.add_argument("--stdin", action="store_true", help="read journal lines from stdin")
    ap.add_argument("--since", default=DEFAULT_SINCE)
    ap.add_argument("--unit", default=DEFAULT_UNIT)
    args = ap.parse_args()

    lines = sys.stdin.read().splitlines() if args.stdin else _read_journal(args.since, args.unit)
    if not lines:
        print("runtime-health: no log lines (empty journal / not on the host?)", file=sys.stderr)
        return 2

    sig = parse(lines)
    sig.days = _span_days(lines)
    composite, dims = compute(sig)
    meta = {"runs": sig.runs, "days": sig.days, "since": args.since}

    if args.json:
        print(json.dumps({
            "composite": composite,
            "meta": meta,
            "dims": {d.name: round(d.score, 1) for d in dims},
            "weights": {d.name: d.weight for d in dims},
            "detail": {d.name: d.detail for d in dims},
            "extra": {d.name: d.extra for d in dims},
        }, ensure_ascii=False, indent=1))
    else:
        print_summary(composite, dims, meta)

    print(f"metric_value={composite}")
    print("DENEB_RUNTIME_DETAIL " + " ".join(f"{d.name}={d.score:.1f}" for d in dims))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
