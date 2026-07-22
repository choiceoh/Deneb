"""Advisory token-economics readout for RSI Bench (arXiv:2607.06906, The Harness Effect).

The paper's claim: a self-improvement loop judged on quality ALONE will *token-max*
— buy each quality gain with monotonically rising token intensity — because tokens
are someone else's line item, invisible in the score and visible only in the bill.
The escape is a measurement change, not a gate: put CPM (task-completions per million
tokens) next to quality, the way performance-per-watt sits next to performance in
chip design.

This module surfaces that number for Deneb's RSI loop, computed from the runtime's
own per-turn trace (``~/.deneb/agent-logs/<session>.jsonl`` — the ``turn.llm`` token
counts and ``run.end`` completions written by ``core/agentlog``):

- tau  = work tokens (input+output) per completed task (run)   — lower is leaner
- CPM  = completed tasks per million work tokens                — higher is leaner
- cacheHit = fraction of presented input served from cache (the paper's h)

It is deliberately **advisory**: not a scored Metric, not part of the ratchet, and
outside the Report scoring model entirely (main injects it as a payload sidecar). It
exists to make token-maxing observable — "what is unobservable is unmanaged". Promote
to a ratcheted term only once a trustworthy baseline exists (phase 2).
"""

from __future__ import annotations

import json
import os
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

# agentlog wire (gateway-go/internal/core/agentlog/agentlog.go): one JSON object per
# line, {ts, type, runId, session, data}. run.end marks one completed task; turn.llm
# carries the token counts in data.
_TYPE_RUN_END = "run.end"
_TYPE_TURN_LLM = "turn.llm"


def agent_logs_dir() -> Path:
    """Resolve the agent-logs directory, mirroring ledgers.data_dir() overrides.

    DENEB_AGENT_LOGS_DIR wins (tests, explicit control); else the sibling of a
    DENEB_DATA_DIR override (dev state is /tmp-isolated, agent-logs beside data);
    else the production default ~/.deneb/agent-logs.
    """
    override = os.environ.get("DENEB_AGENT_LOGS_DIR", "").strip()
    if override:
        return Path(override)
    data_override = os.environ.get("DENEB_DATA_DIR", "").strip()
    if data_override:
        return Path(data_override).parent / "agent-logs"
    return Path.home() / ".deneb" / "agent-logs"


def _now_ms() -> int:
    return int(datetime.now(timezone.utc).timestamp() * 1000)


@dataclass
class TokenEconomics:
    window_days: int
    completed_tasks: int  # run.end count in window
    input_tokens: int
    output_tokens: int
    cache_read_tokens: int
    cache_creation_tokens: int
    measured: bool  # any in-window log row seen at all

    @property
    def work_tokens(self) -> int:
        """Input+output — the tokens that buy the work (cache read/creation reported apart)."""
        return self.input_tokens + self.output_tokens

    @property
    def tau(self) -> float | None:
        """Token intensity: work tokens per completed task."""
        if self.completed_tasks <= 0:
            return None
        return self.work_tokens / self.completed_tasks

    @property
    def cpm(self) -> float | None:
        """Task-completions per million work tokens — the anti-token-maxing number."""
        if self.work_tokens <= 0:
            return None
        return self.completed_tasks * 1_000_000 / self.work_tokens

    @property
    def cache_hit(self) -> float | None:
        """Fraction of presented input served from cache (paper's h; input excludes cache)."""
        presented = self.input_tokens + self.cache_read_tokens + self.cache_creation_tokens
        if presented <= 0:
            return None
        return self.cache_read_tokens / presented

    def to_dict(self) -> dict[str, Any]:
        return {
            "window_days": self.window_days,
            "measured": self.measured,
            "completed_tasks": self.completed_tasks,
            "input_tokens": self.input_tokens,
            "output_tokens": self.output_tokens,
            "cache_read_tokens": self.cache_read_tokens,
            "cache_creation_tokens": self.cache_creation_tokens,
            "tokens_per_task": round(self.tau, 1) if self.tau is not None else None,
            "cpm": round(self.cpm, 1) if self.cpm is not None else None,
            "cache_hit": round(self.cache_hit, 3) if self.cache_hit is not None else None,
            "advisory": True,
            "note": "not scored / not ratcheted — CPM/tau KPI per arXiv:2607.06906",
        }

    def summary(self) -> str:
        if not self.measured:
            return (
                f"unmeasured (no agent-logs in {self.window_days}d window under {agent_logs_dir()})"
            )
        if self.completed_tasks <= 0 or self.tau is None or self.cpm is None:
            return (
                f"tasks=0 (no run.end in {self.window_days}d; "
                f"work_tokens={self.work_tokens}) window={self.window_days}d"
            )
        hit = f"{self.cache_hit * 100:.1f}%" if self.cache_hit is not None else "—"
        return (
            f"tasks={self.completed_tasks} tau={self.tau:.0f}tok/task "
            f"cpm={self.cpm:.1f}/Mtok cacheHit={hit} window={self.window_days}d"
        )


def load_token_economics(logs: Path | None = None, *, days: int = 7) -> TokenEconomics:
    """Aggregate per-turn tokens and run completions across the agent-logs window."""
    root = logs or agent_logs_dir()
    cutoff = _now_ms() - days * 86_400 * 1000
    completed = inp = out = cread = ccreate = 0
    seen = False
    if root.is_dir():
        for path in sorted(root.glob("*.jsonl")):
            # Skip mock-native-client live-test sessions (agentlog.liveTestSessionPrefix):
            # each smoke is a fresh session with no cache continuity, so counting them
            # craters cacheHit and skews τ/CPM away from real usage (the Go aggregators
            # skip this same prefix). See aggregate_failed.go.
            if path.name.startswith("client:lt-"):
                continue
            try:
                text = path.read_text(encoding="utf-8")
            except OSError:
                continue
            for line in text.splitlines():
                line = line.strip()
                if not line:
                    continue
                try:
                    row = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if not isinstance(row, dict):
                    continue
                ts = row.get("ts")
                if not isinstance(ts, (int, float)) or ts < cutoff:
                    continue
                seen = True
                kind = row.get("type")
                if kind == _TYPE_RUN_END:
                    completed += 1
                elif kind == _TYPE_TURN_LLM:
                    data = row.get("data")
                    if isinstance(data, dict):
                        inp += int(data.get("inputTokens") or 0)
                        out += int(data.get("outputTokens") or 0)
                        cread += int(data.get("cacheReadTokens") or 0)
                        ccreate += int(data.get("cacheCreationTokens") or 0)
    return TokenEconomics(
        window_days=days,
        completed_tasks=completed,
        input_tokens=inp,
        output_tokens=out,
        cache_read_tokens=cread,
        cache_creation_tokens=ccreate,
        measured=seen,
    )
