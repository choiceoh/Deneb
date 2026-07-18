"""JSONL ledger aggregation for RSI Bench (7d / 28d windows)."""

from __future__ import annotations

import json
import os
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterator


def data_dir() -> Path:
    override = os.environ.get("DENEB_DATA_DIR", "").strip()
    if override:
        return Path(override)
    return Path.home() / ".deneb" / "data"


def now_ms() -> int:
    return int(datetime.now(timezone.utc).timestamp() * 1000)


def _created_at_ms(row: dict[str, Any]) -> int | None:
    raw = row.get("createdAt")
    if isinstance(raw, (int, float)):
        return int(raw)
    if isinstance(raw, str) and raw.strip():
        try:
            return int(raw)
        except ValueError:
            try:
                return int(datetime.fromisoformat(raw.replace("Z", "+00:00")).timestamp() * 1000)
            except ValueError:
                return None
    return None


def iter_jsonl(path: Path) -> Iterator[dict[str, Any]]:
    if not path.is_file():
        return
    try:
        text = path.read_text(encoding="utf-8")
    except OSError:
        return
    for line in text.splitlines():
        if not line.strip():
            continue
        try:
            row = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(row, dict):
            yield row


@dataclass
class GenesisWindow:
    evolves: int = 0
    rejected: int = 0
    confirmed: int = 0
    rolled_back: int = 0
    genesis: int = 0
    proposals: int = 0
    per_skill: dict[str, int] = field(default_factory=dict)

    @property
    def resolved(self) -> int:
        return self.confirmed + self.rolled_back

    @property
    def false_accept_rate(self) -> float | None:
        if self.resolved <= 0:
            return None
        return self.rolled_back / self.resolved

    @property
    def confirm_rate(self) -> float | None:
        if self.resolved <= 0:
            return None
        return self.confirmed / self.resolved

    @property
    def thrash(self) -> bool:
        if self.evolves <= 0 or not self.per_skill:
            return False
        top = max(self.per_skill.values())
        return top >= 3 and top * 100 >= self.evolves * 60

    @property
    def top_skill(self) -> tuple[str, int]:
        if not self.per_skill:
            return ("", 0)
        name = max(self.per_skill, key=self.per_skill.get)
        return name, self.per_skill[name]


@dataclass
class JudgeWindow:
    runs: int = 0
    pairs: int = 0
    correct: int = 0
    operator_verdicts: int = 0
    misses: int = 0
    false_rejects: int = 0
    by_category: dict[str, list[int]] = field(default_factory=dict)
    by_class: dict[str, list[int]] = field(default_factory=dict)
    # Per-run byClass accuracy maps (oldest→newest) for swap-consistency.
    class_rate_runs: list[dict[str, float]] = field(default_factory=list)

    @property
    def accuracy(self) -> float | None:
        if self.pairs <= 0:
            return None
        return self.correct / self.pairs


@dataclass
class TransferWindow:
    """EvoAgentBench-style transfer: evolve/genesis covered by validation cases."""

    evolved_skills: int = 0
    evolved_with_cases: int = 0
    genesis_skills: int = 0
    genesis_with_cases: int = 0
    validation_skills: int = 0
    opportunities: int = 0
    opportunity_actionable: int = 0
    evolves7d: int = 0
    distinct_evolved7d: int = 0


@dataclass
class WatchWindow:
    """Soft resolution from skill_evolve_watch.json (postUses ≥ threshold)."""

    soft_confirmed: int = 0
    soft_open: int = 0
    watches: int = 0


# RHAE-style land efficiency (adopted from the ARC-AGI-3 harness scoring form
# min(115, 100*(h/a)^2)): each landed dispatch is weighted by squared attempt
# efficiency against the 1-attempt baseline, capped so a future sub-baseline
# cannot run away. A first-attempt land scores 1.0 (identical to the old flat
# count); a 2nd-attempt land 0.25, a 3rd 0.11 — retry-heavy landing stops
# reading as full utility. Attempts come from the marker attemptId's trailing
# ordinal (`<cid>-<ts>-<pid>-<n>`, coding-dispatch.sh); unparseable → 1 (no
# penalty on unknown).
LAND_EFFICIENCY_CAP = 1.15


def _attempt_ordinal(attempt_id: Any) -> int:
    raw = str(attempt_id or "")
    _, _, tail = raw.rpartition("-")
    try:
        ordinal = int(tail)
    except ValueError:
        return 1
    return ordinal if ordinal >= 1 else 1


def land_efficiency(attempts: int) -> float:
    return min(LAND_EFFICIENCY_CAP, (1.0 / max(attempts, 1)) ** 2)


@dataclass
class DispatchWindow:
    files: int = 0
    accepted: int = 0
    landed: int = 0
    failed: int = 0
    rolled_back: int = 0
    # Σ land_efficiency over landed markers (== landed when every land was
    # first-attempt). Utility's dispatch-land scores this instead of the count.
    land_eff: float = 0.0


@dataclass
class MetaWindow:
    revisions: int = 0
    proposed: int = 0
    adopted: int = 0
    rejected: int = 0
    reverted: int = 0
    structural: int = 0
    parametric: int = 0
    consecutive_parametric_adoptions: int = 0


@dataclass
class ClosureWindow:
    proposed: int = 0
    dispatched: int = 0
    landed: int = 0
    reverted: int = 0


def load_genesis_window(data: Path | None = None, *, days: int = 7) -> GenesisWindow:
    root = data or data_dir()
    cutoff = now_ms() - days * 86400 * 1000
    out = GenesisWindow()
    for row in iter_jsonl(root / "skill_genesis_log.jsonl"):
        ts = _created_at_ms(row)
        if ts is None or ts < cutoff:
            continue
        kind = str(row.get("type") or "")
        skill = str(row.get("skillName") or "")
        if kind == "evolved":
            out.evolves += 1
            if skill:
                out.per_skill[skill] = out.per_skill.get(skill, 0) + 1
        elif kind == "evolve_rejected":
            out.rejected += 1
        elif kind == "evolve_confirmed":
            out.confirmed += 1
        elif kind == "evolve_rolled_back":
            out.rolled_back += 1
        elif kind in {"genesis", ""}:
            out.genesis += 1
        elif kind == "evolution_proposal":
            out.proposals += 1
    return out


def _merge_pair_map(dst: dict[str, list[int]], src: object) -> None:
    if not isinstance(src, dict):
        return
    for key, raw in src.items():
        if isinstance(raw, (list, tuple)) and len(raw) >= 2:
            correct, total = int(raw[0]), int(raw[1])
        elif isinstance(raw, dict):
            correct = int(raw.get("correct") or 0)
            total = int(raw.get("pairs") or raw.get("total") or 0)
        else:
            continue
        bucket = dst.setdefault(str(key), [0, 0])
        bucket[0] += correct
        bucket[1] += total


def load_judge_window(data: Path | None = None, *, days: int = 28) -> JudgeWindow:
    root = data or data_dir()
    cutoff = now_ms() - days * 86400 * 1000
    out = JudgeWindow()
    timed: list[tuple[int, dict[str, Any]]] = []
    for row in iter_jsonl(root / "judge_accuracy_log.jsonl"):
        ts = _created_at_ms(row)
        if ts is None or ts < cutoff:
            continue
        pairs = int(row.get("pairs") or 0)
        correct = int(row.get("correct") or 0)
        if pairs <= 0 and not (row.get("byClass") or row.get("by_class")):
            continue
        timed.append((ts, row))
    timed.sort(key=lambda item: item[0])
    for _ts, row in timed:
        pairs = int(row.get("pairs") or 0)
        correct = int(row.get("correct") or 0)
        if pairs > 0:
            out.runs += 1
            out.pairs += pairs
            out.correct += correct
        misses = row.get("misses") or []
        if isinstance(misses, list):
            out.misses += len(misses)
        fr = row.get("falseRejects") or row.get("false_rejects") or []
        if isinstance(fr, list):
            out.false_rejects += len(fr)
        verdicts = row.get("operatorVerdicts") or row.get("operator_verdicts") or []
        if isinstance(verdicts, list):
            out.operator_verdicts += len(verdicts)
        elif isinstance(verdicts, (int, float)):
            out.operator_verdicts += int(verdicts)
        _merge_pair_map(out.by_category, row.get("byCategory") or row.get("by_category"))
        _merge_pair_map(out.by_class, row.get("byClass") or row.get("by_class"))
        rate_map: dict[str, float] = {}
        raw_class = row.get("byClass") or row.get("by_class") or {}
        if isinstance(raw_class, dict):
            for key, raw in raw_class.items():
                if isinstance(raw, (list, tuple)) and len(raw) >= 2 and int(raw[1]) > 0:
                    rate_map[str(key)] = int(raw[0]) / int(raw[1])
        if rate_map:
            out.class_rate_runs.append(rate_map)
    return out


def load_transfer_window(data: Path | None = None, *, days: int = 7) -> TransferWindow:
    root = data or data_dir()
    cutoff = now_ms() - days * 86400 * 1000
    out = TransferWindow()
    evolved: set[str] = set()
    genesis: set[str] = set()
    evolved7d: set[str] = set()
    evolves7d = 0
    for row in iter_jsonl(root / "skill_genesis_log.jsonl"):
        skill = str(row.get("skillName") or "").strip()
        kind = str(row.get("type") or "")
        ts = _created_at_ms(row)
        if kind == "evolved" and skill:
            evolved.add(skill)
            if ts is not None and ts >= cutoff:
                evolves7d += 1
                evolved7d.add(skill)
        elif kind in {"genesis", ""} and skill:
            genesis.add(skill)
    out.evolved_skills = len(evolved)
    out.genesis_skills = len(genesis)
    out.evolves7d = evolves7d
    out.distinct_evolved7d = len(evolved7d)

    validation: set[str] = set()
    for row in iter_jsonl(root / "skill_validation_cases.jsonl"):
        skill = str(row.get("skillName") or row.get("skill") or "").strip()
        if skill:
            validation.add(skill)
    out.validation_skills = len(validation)
    out.evolved_with_cases = len(evolved & validation)
    out.genesis_with_cases = len(genesis & validation)

    for row in iter_jsonl(root / "skill_opportunities.jsonl"):
        ts = _created_at_ms(row)
        if ts is not None and ts < cutoff:
            continue
        out.opportunities += 1
        route = str(row.get("route") or "").lower()
        if route and route not in {"no-op", "noop", "skip"}:
            out.opportunity_actionable += 1
    return out


# Soft usage reconstruct horizon: longer than the 14d stale-confirm window so
# skills that accrued ≥3 real uses but lost their watch file without
# evolve_confirmed (sparse-traffic gap) still count toward soft≥3.
_SOFT_USAGE_HORIZON_DAYS = 28
_LAND_OUTCOMES = frozenset({"landed", "merged", "deployed", "watch_passed", "applied"})
_FAIL_OUTCOMES = frozenset({"failed", "error", "timeout", "session_failed", "abandoned"})
_ROLLBACK_OUTCOMES = frozenset({"rolled_back", "reverted"})
_ACCEPT_STATUSES = frozenset({"accepted", "dispatched", "started", "pr_opened", "attempted"})


def _soft_skills_from_usage(
    root: Path, *, soft_confirm_uses: int, days: int = _SOFT_USAGE_HORIZON_DAYS
) -> set[str]:
    """Skills evolved in-horizon that accrued ≥soft_confirm_uses real post-evolve uses.

    Open watches alone under-count when a watch file is cleared without writing
    evolve_confirmed (sparse traffic never reaches the 6-use hard window).
    """
    cutoff = now_ms() - days * 86400 * 1000
    latest_evolve: dict[str, int] = {}
    for row in iter_jsonl(root / "skill_genesis_log.jsonl"):
        if str(row.get("type") or "") != "evolved":
            continue
        ts = _created_at_ms(row)
        if ts is None or ts < cutoff:
            continue
        name = str(row.get("skillName") or row.get("skill") or "").strip()
        if not name:
            continue
        latest_evolve[name] = max(latest_evolve.get(name, 0), ts)
    if not latest_evolve:
        return set()
    uses: dict[str, int] = {name: 0 for name in latest_evolve}
    for row in iter_jsonl(root / "skill_usage.jsonl"):
        name = str(row.get("skillName") or row.get("skill") or "").strip()
        if name not in uses:
            continue
        src = str(row.get("source") or "").strip().lower()
        if src not in {"", "real"}:
            continue
        used = row.get("usedAt")
        try:
            used_ms = int(used) if used is not None else 0
        except (TypeError, ValueError):
            continue
        if used_ms >= latest_evolve[name]:
            uses[name] += 1
    return {name for name, n in uses.items() if n >= soft_confirm_uses}


def load_watch_window(data: Path | None = None, *, soft_confirm_uses: int = 3) -> WatchWindow:
    root = data or data_dir()
    path = root / "skill_evolve_watch.json"
    out = WatchWindow()
    soft_skills: set[str] = set()
    if path.is_file():
        try:
            payload = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            payload = {}
        if isinstance(payload, dict):
            for skill, entry in payload.items():
                if not isinstance(entry, dict):
                    continue
                out.watches += 1
                uses = int(entry.get("postUses") or entry.get("post_uses") or 0)
                name = str(skill)
                if uses >= soft_confirm_uses:
                    soft_skills.add(name)
                elif uses >= 1:
                    out.soft_open += 1
    soft_skills |= _soft_skills_from_usage(root, soft_confirm_uses=soft_confirm_uses)
    out.soft_confirmed = len(soft_skills)
    return out


def load_dispatch_window(data: Path | None = None) -> DispatchWindow:
    """Count L4 markers. Land truth is marker ``outcome`` (review ``status`` stays accepted)."""
    root = data or data_dir()
    out = DispatchWindow()
    folder = root / "coding_dispatch"
    if not folder.is_dir():
        return out
    for path in folder.glob("*.json"):
        try:
            row = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        if not isinstance(row, dict):
            continue
        out.files += 1
        status = str(row.get("status") or row.get("state") or "").lower()
        outcome = str(row.get("outcome") or row.get("_dispatchPhase") or "").lower()
        terminal = outcome or status
        if status in _ACCEPT_STATUSES or outcome in _LAND_OUTCOMES | _FAIL_OUTCOMES | {"declined"}:
            out.accepted += 1
        if terminal in _LAND_OUTCOMES:
            out.landed += 1
            out.land_eff += land_efficiency(_attempt_ordinal(row.get("attemptId")))
        if terminal in _FAIL_OUTCOMES:
            out.failed += 1
        if terminal in _ROLLBACK_OUTCOMES:
            out.rolled_back += 1
    return out


def load_meta_window(data: Path | None = None, *, days: int = 7) -> MetaWindow:
    root = data or data_dir()
    cutoff = now_ms() - days * 86400 * 1000
    out = MetaWindow()
    parametric_streak = 0
    rows = [
        row
        for row in iter_jsonl(root / "meta_evolution_log.jsonl")
        if (ts := _created_at_ms(row)) is not None and ts >= cutoff
    ]
    rows.sort(key=lambda r: _created_at_ms(r) or 0)
    latest_util: dict[str, int] | None = None
    for row in rows:
        out.revisions += 1
        if row.get("proposed") is True:
            out.proposed += 1
        action = str(row.get("action") or "").lower()
        if action == "adopted" or (
            row.get("proposed") is False and "adopt" in str(row.get("reason") or "").lower()
        ):
            out.adopted += 1
        if action == "rejected":
            out.rejected += 1
        if action == "reverted":
            out.reverted += 1
        util = row.get("operatorUtility") or {}
        if isinstance(util, dict) and any(
            int(util.get(k) or 0) > 0 for k in ("adopted7d", "rejected7d", "reverted7d")
        ):
            latest_util = {
                "adopted": int(util.get("adopted7d") or 0),
                "rejected": int(util.get("rejected7d") or 0),
                "reverted": int(util.get("reverted7d") or 0),
            }
        rev_class = str(row.get("revisionClass") or "").lower()
        if rev_class == "structural":
            out.structural += 1
            parametric_streak = 0
        elif rev_class == "parametric":
            out.parametric += 1
            if action == "adopted" or "adopt" in str(row.get("reason") or "").lower():
                parametric_streak += 1
            else:
                parametric_streak = 0
        out.consecutive_parametric_adoptions = max(
            out.consecutive_parametric_adoptions, parametric_streak
        )
    # Prefer explicit action tallies; fall back to latest operatorUtility window once.
    if out.adopted + out.rejected + out.reverted == 0 and latest_util:
        out.adopted = latest_util["adopted"]
        out.rejected = latest_util["rejected"]
        out.reverted = latest_util["reverted"]
    return out


def load_closure_window(data: Path | None = None) -> ClosureWindow:
    """All-time candidate statuses (L4 supply is sparse — do not 7d-filter to zero)."""
    root = data or data_dir()
    out = ClosureWindow()
    for row in iter_jsonl(root / "self_correction_candidates.jsonl"):
        kind = str(row.get("type") or "")
        if kind not in {"self_correction_candidate", "self_correction_review", ""}:
            # Dispatch markers are separate; count candidates/reviews with status.
            if kind == "self_correction_dispatch":
                out.dispatched += 1
                continue
            continue
        status = str(row.get("status") or "").lower()
        if not status and kind == "self_correction_candidate":
            status = "proposed"
        if status in {"proposed", "accepted", "applied", "landed", "rejected", "reverted", "superseded"}:
            out.proposed += 1
        if status in {"dispatched", "accepted", "applied", "landed"}:
            out.dispatched += 1
        if status in {"applied", "landed", "accepted"}:
            out.landed += 1
        if status in {"reverted", "rejected"}:
            out.reverted += 1
    return out
