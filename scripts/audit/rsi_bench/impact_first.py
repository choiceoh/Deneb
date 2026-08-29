"""Impact-first discipline: did a coding run check its blast radius before editing?

The defect this measures is not "the agent used the wrong tool" — it is the agent
reading whole files into a finite window, evicting the context it needed, and then
editing whatever symbol was still loud enough to see. A dependency-graph lookup
(``codegraph impact``) before the first edit turns a whole-file read into a bounded
assembly of exactly the symbols the change reaches. The effective capability gain
comes from shrinking the problem, so the ORDER is the thing worth scoring: a graph
lookup after the edit is context for a review, not for the decision.

Prompt discipline was tried first and does not hold — it degrades silently and
leaves no trace. This module makes the order observable from artifacts the lanes
already write, so it can be scored instead of asserted.

Two lanes, both read deterministically, no LLM and no external binary:

- **Runtime lane** — ``~/.deneb/agent-logs/<session>.jsonl``. ``turn.tool`` rows carry
  the tool ``name`` and sanitized ``targets`` in call order, so a run that mutated a
  source file is visible, and so is whether a ``codegraph_*`` call preceded it.
- **L4 dispatch lane** — the archived Codex rollouts under
  ``~/.deneb/data/coding_dispatch_sessions/.codex/sessions/``. ``exec_command`` calls
  carry the shell command (the dispatch lane uses the ``codegraph`` CLI, not MCP) and
  ``patch_apply_end`` marks a file edit.

A run with no source mutation is not counted at all — the denominator is
"runs that edited source", not "runs".
"""

from __future__ import annotations

import json
import os
import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Iterator

from .ledgers import data_dir, now_ms

# Tools that mutate a file from the runtime agent. exec is excluded on purpose:
# its targets are workdirs, so "ran a command in a repo" would be indistinguishable
# from "edited source" and would inflate the denominator with build and test runs.
MUTATING_TOOLS = frozenset({"edit", "write"})

# A dependency-graph consultation, in either lane's vocabulary.
CODEGRAPH_TOOL_PREFIX = "codegraph_"
_CLI_CODEGRAPH = re.compile(r"(?:^|[|&;(\s])codegraph\s+(impact|callers|callees|node|explore|affected)\b")

# Suffixes the CodeGraph index parses — mirrors impact_brief.SOURCE_SUFFIXES.
# A run that only wrote Markdown or JSON has no blast radius to check.
SOURCE_SUFFIXES = (
    ".go", ".kt", ".kts", ".java", ".swift",
    ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
    ".py", ".rs", ".c", ".h", ".cc", ".cpp", ".hpp", ".m", ".mm",
)

DEFAULT_WINDOW_DAYS = 28
DISPATCH_ROLLOUT_SUBDIR = Path("coding_dispatch_sessions") / ".codex" / "sessions"


def agent_logs_dir() -> Path:
    """Resolve the agent-logs directory (same override order as token_economics)."""
    override = os.environ.get("DENEB_AGENT_LOGS_DIR", "").strip()
    if override:
        return Path(override)
    data_override = os.environ.get("DENEB_DATA_DIR", "").strip()
    if data_override:
        return Path(data_override).parent / "agent-logs"
    return Path.home() / ".deneb" / "agent-logs"


@dataclass
class ImpactFirstWindow:
    """Runs that edited source, split by whether they checked impact first."""

    window_days: int = DEFAULT_WINDOW_DAYS
    runtime_edit_runs: int = 0
    runtime_impact_first: int = 0
    runtime_impact_late: int = 0
    dispatch_edit_runs: int = 0
    dispatch_impact_first: int = 0
    dispatch_impact_late: int = 0
    lanes_seen: set[str] = field(default_factory=set)

    @property
    def edit_runs(self) -> int:
        return self.runtime_edit_runs + self.dispatch_edit_runs

    @property
    def impact_first(self) -> int:
        return self.runtime_impact_first + self.dispatch_impact_first

    @property
    def impact_late(self) -> int:
        """Consulted the graph, but only after the first edit — order missed."""
        return self.runtime_impact_late + self.dispatch_impact_late

    @property
    def rate(self) -> float | None:
        return self.impact_first / self.edit_runs if self.edit_runs else None

    def to_dict(self) -> dict[str, Any]:
        return {
            "windowDays": self.window_days,
            "editRuns": self.edit_runs,
            "impactFirst": self.impact_first,
            "impactLate": self.impact_late,
            "rate": round(self.rate, 4) if self.rate is not None else None,
            "runtime": {
                "editRuns": self.runtime_edit_runs,
                "impactFirst": self.runtime_impact_first,
                "impactLate": self.runtime_impact_late,
            },
            "dispatch": {
                "editRuns": self.dispatch_edit_runs,
                "impactFirst": self.dispatch_impact_first,
                "impactLate": self.dispatch_impact_late,
            },
            "lanesSeen": sorted(self.lanes_seen),
        }


def _is_source(target: str) -> bool:
    return target.endswith(SOURCE_SUFFIXES)


def _iter_lines(path: Path) -> Iterator[dict[str, Any]]:
    try:
        with path.open(encoding="utf-8", errors="replace") as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                try:
                    row = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if isinstance(row, dict):
                    yield row
    except OSError:
        return


def classify_order(graph_positions: list[int], edit_positions: list[int]) -> str | None:
    """'first' | 'late' | None — None when the run never edited source.

    "late" covers both "consulted the graph only after editing" and "never
    consulted it": neither informed the edit, which is what the metric is about.
    The two are still told apart in evidence via impact_late vs edit_runs.
    """
    if not edit_positions:
        return None
    first_edit = min(edit_positions)
    return "first" if any(pos < first_edit for pos in graph_positions) else "late"


def scan_agent_log(path: Path, cutoff_ms: int) -> list[str]:
    """Per-run verdicts ('first'/'late') for one agent-log file."""
    graph: dict[str, list[int]] = {}
    edits: dict[str, list[int]] = {}
    seq = 0
    for row in _iter_lines(path):
        if row.get("type") != "turn.tool":
            continue
        ts = row.get("ts")
        if not isinstance(ts, (int, float)) or ts < cutoff_ms:
            continue
        run = str(row.get("runId") or "")
        if not run:
            continue
        seq += 1
        data = row.get("data")
        if not isinstance(data, dict):
            continue
        name = str(data.get("name") or "")
        # A blocked or hallucinated call never ran — it is not a consultation.
        if data.get("blocked") or data.get("unknownTool") or data.get("isError"):
            continue
        if name.startswith(CODEGRAPH_TOOL_PREFIX):
            graph.setdefault(run, []).append(seq)
            continue
        if name in MUTATING_TOOLS:
            targets = data.get("targets")
            if isinstance(targets, list) and any(
                isinstance(t, str) and _is_source(t) for t in targets
            ):
                edits.setdefault(run, []).append(seq)
    verdicts: list[str] = []
    for run, positions in edits.items():
        verdict = classify_order(graph.get(run, []), positions)
        if verdict:
            verdicts.append(verdict)
    return verdicts


def _rollout_cmd(payload: dict[str, Any]) -> str:
    """Shell command text from a Codex function_call payload, or ""."""
    raw = payload.get("arguments")
    if not isinstance(raw, str):
        return ""
    try:
        args = json.loads(raw)
    except json.JSONDecodeError:
        return raw
    if isinstance(args, dict):
        cmd = args.get("cmd") or args.get("command")
        if isinstance(cmd, list):
            return " ".join(str(part) for part in cmd)
        if isinstance(cmd, str):
            return cmd
    return ""


def scan_rollout(path: Path) -> str | None:
    """'first' | 'late' for one Codex dispatch rollout, or None when it never edited."""
    graph_positions: list[int] = []
    edit_positions: list[int] = []
    for seq, row in enumerate(_iter_lines(path)):
        payload = row.get("payload")
        if not isinstance(payload, dict):
            continue
        ptype = payload.get("type")
        if ptype in ("function_call", "custom_tool_call"):
            if _CLI_CODEGRAPH.search(_rollout_cmd(payload)):
                graph_positions.append(seq)
        elif ptype in ("patch_apply_end", "patch_apply_begin"):
            edit_positions.append(seq)
    return classify_order(graph_positions, edit_positions)


def _mtime_ms(path: Path) -> int:
    try:
        return int(path.stat().st_mtime * 1000)
    except OSError:
        return 0


def load_impact_first_window(
    *,
    days: int = DEFAULT_WINDOW_DAYS,
    logs: Path | None = None,
    data: Path | None = None,
    now: int | None = None,
) -> ImpactFirstWindow:
    """Aggregate both coding lanes over the window."""
    window = ImpactFirstWindow(window_days=days)
    cutoff = (now if now is not None else now_ms()) - days * 86_400_000

    # An explicit data dir means an isolated world (tests, a replay): agent-logs
    # is its sibling there, never the production one. Reading live logs under an
    # isolated data dir is the contamination class devgw-workspace-contamination
    # names — a "no evidence" fixture that quietly scores real traffic.
    if logs is not None:
        log_dir = logs
    elif data is not None:
        log_dir = data.parent / "agent-logs"
    else:
        log_dir = agent_logs_dir()
    if log_dir.is_dir():
        for path in sorted(log_dir.glob("*.jsonl")):
            if _mtime_ms(path) < cutoff:
                continue
            verdicts = scan_agent_log(path, cutoff)
            if verdicts:
                window.lanes_seen.add("runtime")
            for verdict in verdicts:
                window.runtime_edit_runs += 1
                if verdict == "first":
                    window.runtime_impact_first += 1
                else:
                    window.runtime_impact_late += 1

    rollout_dir = (data or data_dir()) / DISPATCH_ROLLOUT_SUBDIR
    if rollout_dir.is_dir():
        for path in sorted(rollout_dir.glob("rollout-*.jsonl")):
            if _mtime_ms(path) < cutoff:
                continue
            verdict = scan_rollout(path)
            if verdict is None:
                continue
            window.lanes_seen.add("dispatch")
            window.dispatch_edit_runs += 1
            if verdict == "first":
                window.dispatch_impact_first += 1
            else:
                window.dispatch_impact_late += 1

    return window
