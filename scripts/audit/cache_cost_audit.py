"""Cache-aware cost decomposition audit over agent-log JSONL files.

arXiv:2607.12161 ("Token Reduction Is Not Cost Reduction") showed that in
cache-billed agent workloads the prompt cache dominates spend, so nominal
token counts mislead: a removed tool-output token mostly saves cache-read
price (~0.1x), while any prefix break re-bills whole context spans at the
cache-write premium (~1.25x). This audit reconstructs the paper's
four-component decomposition (uncached input 1.0x / cache creation 1.25x /
cache read 0.1x / output separate) from the per-turn usage agentlog already
records, and derives the trajectory-level signals the paper says actually
move cost:

- per-model cost decomposition and the effective price ratio of a nominal
  input token (how much less "token count" is worth than it looks);
- turn-over-turn cache-read growth inside a run (a stalled read means the
  tail of the context is re-billed at full price every turn);
- run-boundary prefix survival (where per-run context mutation — cheap
  pruning cutoff advance, dynamic-block drift — lands);
- repeated identical tool calls, split at the compaction cutoff (a repeat
  beyond the cutoff is a candidate "result was pruned, model re-fetched"
  trajectory cost).

Reads ~/.deneb/agent-logs/*.jsonl (append-only, schema owned by
gateway-go/internal/core/agentlog). Advisory and read-only; never mutates
logs. Cache-price multipliers are the Anthropic-published ratios used as a
neutral convention — subscription providers (kimi/glm) don't bill per token,
but the weighted volume still tracks quota burn and provider-side compute.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Iterable, TextIO

# Mirrors of the gateway's compaction constants (compaction/polaris.go).
# A repeat tool call further apart than CUTOFF_TURNS assistant turns is in the
# window where Tier 2/2b pruning has already stubbed the earlier result.
CUTOFF_TURNS = 4
STUB_MIN_CHARS = 256

# Anthropic prompt-caching price multipliers relative to the uncached input
# price (docs.anthropic.com prompt-caching). Used as a provider-neutral
# weighting convention, not a claim about any one provider's billing.
WEIGHT_UNCACHED = 1.0
WEIGHT_CREATION = 1.25
WEIGHT_READ = 0.1

# A turn-over-turn transition is "stalled" when the next turn re-reads less
# than this share of the previous turn's total prompt: the un-reused tail was
# re-billed at full price.
STALL_REUSE_SHARE = 0.8

RUN_GAP_BUCKETS = ((5 * 60_000, "<5m"), (30 * 60_000, "5-30m"), (None, ">30m"))


@dataclass
class Turn:
    turn: int
    uncached: int
    output: int
    read: int
    creation: int

    @property
    def prompt(self) -> int:
        return self.uncached + self.read + self.creation

    @property
    def has_cache_telemetry(self) -> bool:
        return self.read > 0 or self.creation > 0


@dataclass
class Run:
    run_id: str
    model_key: str
    start_ts: int
    turns: list[Turn] = field(default_factory=list)


@dataclass
class ToolCall:
    llm_turns_before: int
    name: str
    input_hash: str
    output_len: int


@dataclass
class ModelCost:
    runs: int = 0
    turns: int = 0
    uncached: int = 0
    output: int = 0
    read: int = 0
    creation: int = 0
    # Within-run turn-over-turn reuse, attributed to this model.
    transitions: int = 0
    stalled_transitions: int = 0
    unreused_tokens: int = 0

    def fold(self, turn: Turn) -> None:
        self.turns += 1
        self.uncached += turn.uncached
        self.output += turn.output
        self.read += turn.read
        self.creation += turn.creation

    @property
    def nominal_input(self) -> int:
        return self.uncached + self.read + self.creation

    @property
    def weighted_input(self) -> float:
        return (
            WEIGHT_UNCACHED * self.uncached
            + WEIGHT_CREATION * self.creation
            + WEIGHT_READ * self.read
        )

    @property
    def price_ratio(self) -> float:
        """Effective cost of one nominal input token, in uncached-price units."""
        return self.weighted_input / self.nominal_input if self.nominal_input else 1.0


@dataclass
class ToolStat:
    calls: int = 0
    repeats_within_cutoff: int = 0
    repeats_beyond_cutoff: int = 0
    stub_sized_beyond_cutoff: int = 0  # earlier result >= STUB_MIN_CHARS


@dataclass
class Report:
    models: dict[str, ModelCost] = field(default_factory=dict)
    # Turn-over-turn transitions inside runs with cache telemetry.
    transitions: int = 0
    stalled_transitions: int = 0
    unreused_tokens: int = 0  # prompt tokens the next turn re-billed uncached
    # Run-boundary turn-1 reuse ratios, keyed by (model key, gap bucket label).
    boundary_reuse: dict[tuple[str, str], list[float]] = field(default_factory=dict)
    # Session rewrite amplification (creation-reporting models only).
    amplification: list[float] = field(default_factory=list)
    tools: dict[str, ToolStat] = field(default_factory=dict)
    files: int = 0
    skipped_lines: int = 0


def iter_entries(path: Path) -> Iterable[dict]:
    try:
        raw = path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return
    for line in raw.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            entry = json.loads(line)
        except ValueError:
            yield {"__malformed__": True}
            continue
        if isinstance(entry, dict):
            yield entry


def _turn_from(data: dict) -> Turn:
    return Turn(
        turn=int(data.get("turn") or 0),
        uncached=int(data.get("inputTokens") or 0),
        output=int(data.get("outputTokens") or 0),
        read=int(data.get("cacheReadTokens") or 0),
        creation=int(data.get("cacheCreationTokens") or 0),
    )


def fold_session(path: Path, since_ms: int, report: Report) -> None:
    """Single chronological pass over one session file (mirrors the Go
    aggregator's per-file runId scoping: correlation never crosses files)."""
    runs: dict[str, Run] = {}
    ordered_runs: list[Run] = []
    llm_turns_seen = 0
    tool_first_seen: dict[tuple[str, str], ToolCall] = {}
    had_entries = False

    for entry in iter_entries(path):
        if entry.get("__malformed__"):
            report.skipped_lines += 1
            continue
        ts = int(entry.get("ts") or 0)
        if since_ms > 0 and ts < since_ms:
            continue
        etype = entry.get("type")
        run_id = str(entry.get("runId") or "")
        data = entry.get("data") or {}
        if not isinstance(data, dict):
            continue
        had_entries = True

        if etype == "run.start":
            model = str(data.get("model") or "")
            if not model:
                continue
            run = Run(
                run_id=run_id,
                model_key=str(data.get("provider") or "") + "/" + model,
                start_ts=ts,
            )
            runs[run_id] = run
            ordered_runs.append(run)
            report.models.setdefault(run.model_key, ModelCost()).runs += 1
        elif etype == "turn.llm":
            llm_turns_seen += 1
            run = runs.get(run_id)
            if run is None:
                continue
            turn = _turn_from(data)
            run.turns.append(turn)
            report.models[run.model_key].fold(turn)
        elif etype == "turn.tool":
            name = str(data.get("name") or "")
            input_hash = str(data.get("inputHash") or "")
            if not name:
                continue
            stat = report.tools.setdefault(name, ToolStat())
            stat.calls += 1
            if not input_hash:
                continue
            key = (name, input_hash)
            prior = tool_first_seen.get(key)
            call = ToolCall(
                llm_turns_before=llm_turns_seen,
                name=name,
                input_hash=input_hash,
                output_len=int(data.get("outputLen") or 0),
            )
            if prior is None:
                tool_first_seen[key] = call
                continue
            gap = call.llm_turns_before - prior.llm_turns_before
            if gap > CUTOFF_TURNS:
                stat.repeats_beyond_cutoff += 1
                if prior.output_len >= STUB_MIN_CHARS:
                    stat.stub_sized_beyond_cutoff += 1
            else:
                stat.repeats_within_cutoff += 1
            # Keep the earliest sighting so a chain of repeats measures
            # distance from the original result, which is what got pruned.

    if had_entries:
        report.files += 1
    _fold_trajectory(ordered_runs, report)


def _fold_trajectory(ordered_runs: list[Run], report: Report) -> None:
    prev_run: Run | None = None
    session_creation = 0
    max_prompt = 0
    creation_reported = False

    for run in ordered_runs:
        for turn in run.turns:
            session_creation += turn.creation
            creation_reported = creation_reported or turn.creation > 0
            max_prompt = max(max_prompt, turn.prompt)

        # Within-run turn-over-turn reuse growth.
        model_cost = report.models.get(run.model_key)
        for prev, cur in zip(run.turns, run.turns[1:]):
            if not (prev.has_cache_telemetry or cur.has_cache_telemetry):
                continue
            if prev.prompt <= 0:
                continue
            report.transitions += 1
            reused = cur.read + cur.creation
            stalled = reused < STALL_REUSE_SHARE * prev.prompt
            unreused = max(0, prev.prompt - reused)
            if stalled:
                report.stalled_transitions += 1
            report.unreused_tokens += unreused
            if model_cost is not None:
                model_cost.transitions += 1
                model_cost.stalled_transitions += 1 if stalled else 0
                model_cost.unreused_tokens += unreused

        # Run-boundary turn-1 reuse against the previous run of the same model.
        if (
            prev_run is not None
            and run.turns
            and prev_run.turns
            and run.model_key == prev_run.model_key
            and run.turns[0].has_cache_telemetry
        ):
            first = run.turns[0]
            if first.prompt > 0:
                ratio = first.read / first.prompt
                gap_ms = run.start_ts - prev_run.start_ts
                key = (run.model_key, _gap_bucket(gap_ms))
                report.boundary_reuse.setdefault(key, []).append(ratio)
        if run.turns:
            prev_run = run

    if creation_reported and max_prompt > 0 and len(ordered_runs) >= 3:
        report.amplification.append(session_creation / max_prompt)


def _gap_bucket(gap_ms: int) -> str:
    for limit, label in RUN_GAP_BUCKETS:
        if limit is None or gap_ms < limit:
            return label
    return RUN_GAP_BUCKETS[-1][1]


def build_report(log_dir: Path, since_days: float, session_prefix: str = "") -> Report:
    since_ms = int((time.time() - since_days * 86_400) * 1000) if since_days > 0 else 0
    report = Report()
    for path in sorted(log_dir.glob("*.jsonl")):
        if session_prefix and not path.name.startswith(session_prefix):
            continue
        fold_session(path, since_ms, report)
    return report


def _pct(part: float, whole: float) -> float:
    return 100.0 * part / whole if whole else 0.0


def _mean(values: list[float]) -> float:
    return sum(values) / len(values) if values else 0.0


def _median(values: list[float]) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    return ordered[len(ordered) // 2]


def render(report: Report, top: int, stream: TextIO) -> None:
    w = stream.write
    w(f"cache-cost-audit: {report.files} session files")
    if report.skipped_lines:
        w(f" ({report.skipped_lines} malformed lines skipped)")
    w("\n\n== Model cost decomposition (input-side, uncached-price units) ==\n")
    w(
        f"{'model':<38}{'runs':>6}{'turns':>7}{'uncached':>11}{'cacheRead':>11}"
        f"{'cacheWrite':>11}{'output':>10}{'readShare':>10}{'price/tok':>10}\n"
    )
    models = sorted(
        report.models.items(), key=lambda kv: kv[1].nominal_input, reverse=True
    )
    for key, cost in models:
        if cost.turns == 0:
            continue
        w(
            f"{key:<38}{cost.runs:>6}{cost.turns:>7}{cost.uncached:>11,}"
            f"{cost.read:>11,}{cost.creation:>11,}{cost.output:>10,}"
            f"{_pct(cost.read, cost.nominal_input):>9.1f}%{cost.price_ratio:>10.3f}\n"
        )
    w(
        "\nprice/tok = (1.0*uncached + 1.25*write + 0.1*read) / nominal input —\n"
        "the effective per-token multiplier a naive token count assumes to be 1.0.\n"
        "Models with readShare 0% report no cache telemetry (weights collapse to 1.0).\n"
    )

    w("\n== Within-run cache reuse (turn-over-turn) ==\n")
    if report.transitions:
        w(
            f"transitions={report.transitions}  stalled={report.stalled_transitions}"
            f" ({_pct(report.stalled_transitions, report.transitions):.1f}%)"
            f"  unreused-tokens={report.unreused_tokens:,}\n"
            f"A stalled transition re-billed >{int((1 - STALL_REUSE_SHARE) * 100)}% of the previous"
            " turn's prompt at full price.\n\n"
        )
        w(f"{'model':<38}{'transitions':>12}{'stalled':>9}{'stall%':>8}{'unreused':>12}\n")
        for key, cost in models:
            if cost.transitions == 0:
                continue
            w(
                f"{key:<38}{cost.transitions:>12}{cost.stalled_transitions:>9}"
                f"{_pct(cost.stalled_transitions, cost.transitions):>7.1f}%"
                f"{cost.unreused_tokens:>12,}\n"
            )
    else:
        w("no multi-turn runs with cache telemetry in window\n")

    w("\n== Run-boundary prefix survival (turn-1 read share, same model) ==\n")
    if report.boundary_reuse:
        boundary_models = sorted({model for model, _ in report.boundary_reuse})
        for model in boundary_models:
            for _, label in RUN_GAP_BUCKETS:
                ratios = report.boundary_reuse.get((model, label))
                if not ratios:
                    continue
                w(
                    f"{model:<38}gap {label:<6} n={len(ratios):>5}"
                    f"  mean={_mean(ratios):.2f}  median={_median(ratios):.2f}\n"
                )
        w(
            "Low survival inside the provider cache TTL bucket means the per-run\n"
            "context mutation (pruning cutoff advance, dynamic drift) broke the prefix.\n"
        )
    else:
        w("no consecutive same-model runs with cache telemetry\n")

    w("\n== Session rewrite amplification (creation-reporting models) ==\n")
    if report.amplification:
        w(
            f"sessions={len(report.amplification)}  mean={_mean(report.amplification):.2f}x"
            f"  median={_median(report.amplification):.2f}x  (1.0x = every context byte"
            " written to cache once)\n"
        )
    else:
        w("no sessions with cache-creation telemetry (subscription/OpenAI-path providers)\n")

    w(f"\n== Repeated identical tool calls (top {top} by beyond-cutoff repeats) ==\n")
    tools = sorted(
        ((name, s) for name, s in report.tools.items() if s.repeats_beyond_cutoff),
        key=lambda kv: kv[1].repeats_beyond_cutoff,
        reverse=True,
    )[:top]
    if tools:
        w(
            f"{'tool':<28}{'calls':>7}{'rep<=4t':>9}{'rep>4t':>8}{'stub-sized':>11}"
            f"{'>4t rate':>9}\n"
        )
        for name, s in tools:
            w(
                f"{name:<28}{s.calls:>7}{s.repeats_within_cutoff:>9}"
                f"{s.repeats_beyond_cutoff:>8}{s.stub_sized_beyond_cutoff:>11}"
                f"{_pct(s.repeats_beyond_cutoff, s.calls):>8.1f}%\n"
            )
        w(
            "rep>4t = identical (name, inputHash) re-executed more than "
            f"{CUTOFF_TURNS} assistant turns\nafter the first call — inside the window "
            "where cheap pruning already stubbed the\nearlier result. stub-sized = first "
            f"result was >={STUB_MIN_CHARS} chars (Tier 2b eligible).\n"
        )
    else:
        w("no beyond-cutoff repeated tool calls in window\n")


def to_json(report: Report) -> dict:
    return {
        "files": report.files,
        "skippedLines": report.skipped_lines,
        "weights": {
            "uncached": WEIGHT_UNCACHED,
            "creation": WEIGHT_CREATION,
            "read": WEIGHT_READ,
        },
        "models": {
            key: {
                "runs": cost.runs,
                "turns": cost.turns,
                "uncached": cost.uncached,
                "cacheRead": cost.read,
                "cacheCreation": cost.creation,
                "output": cost.output,
                "nominalInput": cost.nominal_input,
                "weightedInput": round(cost.weighted_input, 1),
                "priceRatio": round(cost.price_ratio, 4),
                "transitions": cost.transitions,
                "stalledTransitions": cost.stalled_transitions,
                "unreusedTokens": cost.unreused_tokens,
            }
            for key, cost in report.models.items()
            if cost.turns
        },
        "withinRun": {
            "transitions": report.transitions,
            "stalledTransitions": report.stalled_transitions,
            "unreusedTokens": report.unreused_tokens,
        },
        "runBoundary": {
            f"{model} {label}": {
                "n": len(ratios),
                "mean": round(_mean(ratios), 4),
                "median": round(_median(ratios), 4),
            }
            for (model, label), ratios in report.boundary_reuse.items()
        },
        "rewriteAmplification": {
            "sessions": len(report.amplification),
            "mean": round(_mean(report.amplification), 4),
            "median": round(_median(report.amplification), 4),
        },
        "tools": {
            name: {
                "calls": s.calls,
                "repeatsWithinCutoff": s.repeats_within_cutoff,
                "repeatsBeyondCutoff": s.repeats_beyond_cutoff,
                "stubSizedBeyondCutoff": s.stub_sized_beyond_cutoff,
            }
            for name, s in report.tools.items()
            if s.repeats_within_cutoff or s.repeats_beyond_cutoff
        },
    }


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "--log-dir",
        default=str(Path.home() / ".deneb" / "agent-logs"),
        help="agent-log directory (default: ~/.deneb/agent-logs)",
    )
    parser.add_argument(
        "--since-days", type=float, default=7.0, help="lookback window (0 = all)"
    )
    parser.add_argument(
        "--session-prefix",
        default="",
        help="only session files whose name starts with this prefix",
    )
    parser.add_argument("--top", type=int, default=12, help="tool table row cap")
    parser.add_argument("--json", action="store_true", help="emit JSON instead of text")
    return parser


def main(argv: list[str] | None = None, stream: TextIO | None = None) -> int:
    args = _parser().parse_args(argv)
    out = stream or sys.stdout
    log_dir = Path(args.log_dir).expanduser()
    if not log_dir.is_dir():
        print(f"cache-cost-audit: no such directory: {log_dir}", file=sys.stderr)
        return 2
    report = build_report(log_dir, args.since_days, args.session_prefix)
    if args.json:
        json.dump(to_json(report), out, indent=2, sort_keys=True)
        out.write("\n")
    else:
        render(report, args.top, out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
