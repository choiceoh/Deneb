"""Recursive self-improvement (RSI) loop-status surface.

The RSI subsystem is four stacked loops (L1 skill evolution, L2 meta-evolution,
L3 verifier co-evolution, L4 source self-edit). Each writes an append-only
ledger under ``~/.deneb/data/``. Once the machinery is built, the interesting
question is no longer "does it compile" but "is each loop actually TURNING, or
is it built-but-waiting?" — a distinction the ledgers answer but which nobody
reads by hand twice the same way.

This module classifies each layer into one honest state:

  LIVE        the loop is producing AND consuming — it is turning.
  DATA-GATED  built and running, but the fuel it needs has not accumulated yet
              (e.g. the judge catches every planted defect so there are no
              labeled misses for L3 to learn from). Time fixes this, not code.
  STARVED     built, but its INPUT source is empty — an upstream/wiring gap that
              CODE fixes (e.g. L4 dispatch wants code-scope candidates but no
              source produces them).
  FROZEN      a self-brake deliberately halted the loop (L2 auto-adopt freeze).
  IDLE        no recent activity at all — the lane may be unscheduled.

Honesty (mirrors runtime_health's fault accounting): DATA-GATED is NOT a defect
— it is the correct state of a young loop with no data. Reporting it as a
problem would push toward manufacturing fake fuel. STARVED, by contrast, is an
actionable wiring gap. The two look similar in a naive "0 events" count; the
whole point of this surface is to tell them apart.

stdlib-only and importable for deterministic tests; the CLI is
``scripts/audit/rsi-status.py``.
"""

from __future__ import annotations

import argparse
import datetime
import json
import os
import sys
import time
from dataclasses import dataclass, field
from typing import Any, TextIO

DEFAULT_DATA_DIR = os.path.expanduser("~/.deneb/data")
WINDOW_DAYS = 7
DAY_MS = 24 * 60 * 60 * 1000

# e-process cutover graduation thresholds (must match
# tracker_eprocess_cutover.go): observation-mode baseline-test labels justify
# handing rollback firing to the anytime-valid test at n>=20 with >=90%
# legacy agreement. Labels accumulate over the whole ledger history — a
# windowed count would starve the evidence at organic cadence.
EPROCESS_CUTOVER_MIN_LABELS = 20
EPROCESS_CUTOVER_MIN_AGREEMENT = 0.90

# Organic false-accept mining window (must match judge_accuracy.go): rollbacks
# are scarce at organic cadence, so real-usage P3 labels use a 30d window.
ORGANIC_FALSE_ACCEPT_WINDOW_DAYS = 30

# Degradation classes the judge-accuracy lane replays. The BLATANT ones a
# competent judge always catches (so 0 misses there means nothing); the SUBTLE
# ones are the ones that actually produce labeled misses (P3 fuel).
BLATANT_CLASSES = frozenset({"section-drop", "fake-tool", "truncation", "overfit"})
SUBTLE_CLASSES = frozenset({"imperative-drop", "safety-drop", "imperative-weaken", "scope-narrow"})
# Escalated tier (probe curriculum ladder): the lane deploys in-place weaken
# probes only after the incumbent judge posts ESCALATION_WINDOW consecutive
# zero-miss drop-tier runs (genesis/judge_accuracy.go weakenTierUnlocked —
# keep both in sync).
WEAKEN_CLASSES = frozenset({"imperative-weaken", "scope-narrow"})
ESCALATION_WINDOW = 5

# L4 dispatch supply contract (must match scripts/dev/coding-dispatch.sh and
# genesis/rsi_status.go rsiDispatchSources): a candidate is dispatchable only if
# it is a proposed, code-scope correction from an evidence-bearing source.
# health-finding graduated 2026-07-12 (first batch reviewed clean); tool-quality
# graduated 2026-07-13 (operator directive); runtime-error and deadcode-finding
# stay staged until their own batch review.
L4_SOURCES = ("evolve-tool-gap", "self-harness", "health-finding", "tool-quality")

GRADUATION_STATE_PATH = os.path.expanduser("~/.deneb/data/graduation_state.json")


def _graduation_rows(path: str = "") -> dict:
    """Executed graduation-ladder unlocks (loop-owned, operator directive
    2026-07-14). Mirrors genesis/graduation_state.go; consumed by the
    dispatchable predicate and the ladder display so Go/sh/py cannot drift."""
    try:
        with open(path or GRADUATION_STATE_PATH, encoding="utf-8") as fh:
            rows = json.load(fh).get("rows") or {}
        return rows if isinstance(rows, dict) else {}
    except (OSError, json.JSONDecodeError):
        return {}


def _dispatchable_sources(rows: dict) -> tuple:
    extra = tuple(k[len("source:"):] for k, v in rows.items()
                  if k.startswith("source:") and isinstance(v, dict) and v.get("unlocked"))
    return L4_SOURCES + extra

LIVE, DATA_GATED, STARVED, FROZEN, IDLE = "LIVE", "DATA-GATED", "STARVED", "FROZEN", "IDLE"


@dataclass
class LayerStatus:
    key: str
    title: str
    state: str
    metrics: dict = field(default_factory=dict)
    diagnosis: str = ""


def read_jsonl(path: str) -> list[dict]:
    """Load an append-only JSONL ledger, tolerating partial/corrupt lines."""
    out: list[dict] = []
    try:
        with open(path, encoding="utf-8", errors="replace") as handle:
            for line in handle:
                line = line.strip()
                if not line:
                    continue
                try:
                    out.append(json.loads(line))
                except json.JSONDecodeError:
                    continue
    except OSError:
        return []
    return out


def _auto_adopt_frozen(path: str) -> bool:
    """True when the last auto-adopt freeze verdict has frozen=true.

    Mirrors genesis.Tracker.AutoAdoptFrozen: the path is JSONL (despite the
    .json suffix), and an unfreeze row (frozen:false) clears the brake.
    """
    rows = read_jsonl(path)
    if not rows:
        return False
    return bool(rows[-1].get("frozen"))


def _within(created_ms: Any, cutoff_ms: int) -> bool:
    return isinstance(created_ms, (int, float)) and created_ms >= cutoff_ms


def assess_l1(events: list[dict], now_ms: int) -> LayerStatus:
    """Skill evolution — genesis-log lifecycle events in the 7d window.

    The ledger keys each record by ``type`` (evolved, evolve_rejected,
    evolution_proposal, genesis). Post-evolve confirmations/rollbacks, when they
    occur, ride the usage rollback watch (tracker_usage), not this log — so
    "turning" is measured by committed evolves and new skills, not confirmations.
    """
    cutoff = now_ms - WINDOW_DAYS * DAY_MS
    type_bucket = {
        "evolved": "evolved",
        "genesis": "genesis",
        "evolution_proposal": "proposal",
        "evolve_rejected": "rejected",
    }
    counts = {"evolved": 0, "genesis": 0, "proposal": 0, "rejected": 0}
    for e in events:
        if not _within(e.get("createdAt"), cutoff):
            continue
        bucket = type_bucket.get(e.get("type") or "")
        if bucket:
            counts[bucket] += 1
    metrics = dict(counts)
    metrics.update(_eprocess_readiness(events))
    committed = counts["evolved"] + counts["genesis"]
    if sum(counts.values()) == 0:
        return LayerStatus("L1", "skill evolution", IDLE, metrics,
                           "no genesis-log events in 7d — evolver/genesis lanes idle")
    if committed > 0:
        return LayerStatus("L1", "skill evolution", LIVE, metrics,
                           f"{counts['evolved']} evolved / {counts['genesis']} new skills / "
                           f"{counts['proposal']} proposals / {counts['rejected']} rejected (7d)")
    return LayerStatus("L1", "skill evolution", DATA_GATED, metrics,
                       f"{counts['proposal']} proposals but 0 committed — candidates not clearing the gate")


def _eprocess_readiness(events: list[dict]) -> dict:
    """Score observation-mode baseline-test labels against the cutover
    graduation thresholds (mirrors Tracker.EProcessCutoverReadiness)."""
    labels = disagreements = 0
    for e in events:
        bt = e.get("baselineTest")
        if not isinstance(bt, dict):
            continue
        labels += 1
        if bt.get("disagreement"):
            disagreements += 1
    agreement = (labels - disagreements) / labels if labels else 0.0
    return {
        "eprocess_labels": labels,
        "eprocess_disagreements": disagreements,
        "eprocess_agreement": round(agreement, 4),
        "eprocess_cutover_ready": labels >= EPROCESS_CUTOVER_MIN_LABELS
        and agreement >= EPROCESS_CUTOVER_MIN_AGREEMENT,
    }


def _organic_false_accepts(events: list[dict], now_ms: int) -> int:
    """Count real-usage judge false-accept labels: baseline-CONFIRMED rollbacks
    (the e-process agreed the failure rate rose) in the 30d window (mirrors
    Tracker.OrganicFalseAccepts). A threshold-only rollback with a quiet
    e-process is a disagreement label, never P3 food."""
    cutoff = now_ms - ORGANIC_FALSE_ACCEPT_WINDOW_DAYS * DAY_MS
    count = 0
    for e in events:
        if e.get("type") != "evolve_rolled_back" or not _within(e.get("createdAt"), cutoff):
            continue
        bt = e.get("baselineTest")
        if isinstance(bt, dict) and bt.get("reject"):
            count += 1
    return count


def assess_l2(revisions: list[dict], frozen: bool, now_ms: int) -> LayerStatus:
    """Meta-evolution — weekly slow-loop revisions + auto-adopt freeze state."""
    cutoff = now_ms - 2 * WINDOW_DAYS * DAY_MS  # slow loop: 2-week look-back
    cycles = proposed = adopted = reverted = 0
    last = ""
    for r in revisions:
        if not _within(r.get("createdAt"), cutoff):
            continue
        action = r.get("action") or ""
        if action in ("auto_adopted", "adopted"):
            adopted += 1
        elif action in ("auto_reverted", "operator_reverted"):
            reverted += 1
        elif action == "":
            cycles += 1
            if r.get("proposed"):
                proposed += 1
        if not last:
            last = f"{r.get('epoch', '?')}/{os.path.basename(r.get('artifact', '?'))}"
    metrics = {"cycles": cycles, "proposed": proposed, "adopted": adopted, "reverted": reverted}
    if frozen:
        return LayerStatus("L2", "meta-evolution", FROZEN, metrics,
                           "drift self-brake engaged — auto-adopt frozen to propose-only")
    if cycles == 0 and adopted == 0:
        return LayerStatus("L2", "meta-evolution", IDLE, metrics,
                           "no slow-loop cycles in 14d — awaiting the weekly cadence")
    return LayerStatus("L2", "meta-evolution", LIVE, metrics,
                       f"{cycles} cycles / {proposed} proposed / {adopted} adopted / "
                       f"{reverted} reverted (14d), last {last}")


def assess_l3(runs: list[dict], genesis_events: list[dict], now_ms: int) -> LayerStatus:
    """Verifier co-evolution (P3) — judge-accuracy lane misses plus organic
    (real-usage) false-accept labels are the fuel."""
    cutoff = now_ms - WINDOW_DAYS * DAY_MS
    recent = [r for r in runs if _within(r.get("createdAt"), cutoff)]
    if not recent:
        return LayerStatus("L3", "verifier co-evolution", IDLE, {"runs": 0},
                           "judge-accuracy lane has not run in 7d")
    misses = 0
    false_rejects = 0
    classes_seen: set[str] = set()
    operator_labels = 0
    lane_runs = 0
    for r in recent:
        operator_labels += len(r.get("operatorVerdicts") or [])
        if not (r.get("pairs") or r.get("byClass") or r.get("falseRejects")):
            continue
        lane_runs += 1
        misses += len(r.get("misses") or [])
        false_rejects += len(r.get("falseRejects") or [])
        classes_seen.update((r.get("byClass") or {}).keys())
    subtle_deployed = bool(classes_seen & SUBTLE_CLASSES)
    weaken_deployed = bool(classes_seen & WEAKEN_CLASSES)
    organic = _organic_false_accepts(genesis_events, now_ms)
    metrics = {
        "runs": lane_runs,
        "misses": misses,
        "false_rejects": false_rejects,
        "organic_false_accepts_30d": organic,
        "operator_labels_7d": operator_labels,
        "subtle_probes_deployed": subtle_deployed,
        "weaken_probes_deployed": weaken_deployed,
    }
    if misses > 0 or false_rejects > 0 or organic > 0 or operator_labels > 0:
        return LayerStatus("L3", "verifier co-evolution", LIVE, metrics,
                           f"{misses} judge misses + {false_rejects} false-rejects + {organic} organic labels + "
                           f"{operator_labels} operator labels over {lane_runs} runs — P3 fuel accumulating")
    if not subtle_deployed:
        return LayerStatus("L3", "verifier co-evolution", DATA_GATED, metrics,
                           f"{lane_runs} runs, judge caught every BLATANT defect and subtle probes "
                           "are not in the ledger yet — awaiting the subtle-degradation deploy")
    if weaken_deployed:
        return LayerStatus("L3", "verifier co-evolution", DATA_GATED, metrics,
                           f"{lane_runs} runs, 0 misses even at the escalated weaken tier — "
                           "judge is strong at the current probe ceiling")
    return LayerStatus("L3", "verifier co-evolution", DATA_GATED, metrics,
                       f"{lane_runs} runs with subtle probes but 0 misses — judge is currently strong; "
                       f"the lane escalates to weaken probes after {ESCALATION_WINDOW} saturated runs")


def _merge_candidates(rows: list[dict]) -> dict[str, dict]:
    """Fold review and dispatch deltas onto full candidate records."""
    cand: dict[str, dict] = {}
    for rec in rows:
        rid = rec.get("id") or ""
        if not rid:
            continue
        if rec.get("type") == "self_correction_candidate":
            cand[rid] = dict(rec)
            continue
        current = cand.get(rid)
        if current is None:
            continue
        if rec.get("status"):
            current["status"] = rec["status"]
        if rec.get("type") == "self_correction_dispatch":
            for key in (
                "dispatchPhase", "attemptId", "branch", "prNumber", "prUrl",
                "commitSha", "deployHead", "outcomeNote",
            ):
                if rec.get(key) not in (None, "", 0):
                    current[key] = rec[key]
            if rec.get("dispatchPhase") == "watch_passed":
                current["status"] = "applied"
    return cand


def assess_l4(
    rows: list[dict],
    dispatch_total: int,
    dispatch_today: int,
    outcomes: dict[str, int] | None = None,
    dispatched_ids: set[str] | None = None,
    grad_rows: dict | None = None,
    runtime_status: dict[str, Any] | None = None,
    now_ms: int | None = None,
    marker_outcomes: dict[str, str] | None = None,
) -> LayerStatus:
    """Source self-edit — the coding-dispatch supply of code-scope candidates."""
    outcomes = outcomes or {}
    sources = _dispatchable_sources(grad_rows or {})
    runtime_status = runtime_status or {}
    marker_outcomes = marker_outcomes or {}
    cand = _merge_candidates(rows)
    dispatched_ids = dispatched_ids or set()
    by_scope: dict[str, int] = {}
    dispatchable = 0
    staged = 0
    in_flight = applied = failed = legacy_in_flight = 0
    staged_sources: dict[str, int] = {}
    oldest_pending_at = 0
    for rid, rec in cand.items():
        st = rec.get("status") or "proposed"
        scope = rec.get("scope") or "?"
        by_scope[scope] = by_scope.get(scope, 0) + 1
        src = rec.get("source") or ""
        phase = rec.get("dispatchPhase") or ""
        if scope == "code" and phase in ("started", "pr_opened", "merged", "deployed"):
            in_flight += 1
            continue
        if scope == "code" and phase == "watch_passed":
            applied += 1
            continue
        if scope == "code" and phase in ("failed", "rolled_back"):
            failed += 1
            outcome = marker_outcomes.get(rid, "")
            if (
                (phase == "rolled_back" or outcome not in ("landed", "attempted"))
                and st in ("proposed", "accepted")
                and src.startswith(sources)
            ):
                dispatchable += 1
                created_at = int(rec.get("createdAt") or 0)
                if created_at > 0 and (oldest_pending_at == 0 or created_at < oldest_pending_at):
                    oldest_pending_at = created_at
            continue
        if scope == "code" and rid in dispatched_ids:
            legacy_in_flight += 1
            continue
        # proposed = unreviewed backlog; accepted = review-endorsed, awaiting
        # implementation — both are queued dispatch supply (the heartbeat review
        # lane accepts candidates it cannot implement itself).
        if scope == "code" and st in ("proposed", "accepted"):
            if src.startswith(sources):
                dispatchable += 1
                created_at = int(rec.get("createdAt") or 0)
                if created_at > 0 and (oldest_pending_at == 0 or created_at < oldest_pending_at):
                    oldest_pending_at = created_at
            else:
                # Proposed code candidate from a source not yet in the dispatch
                # allowlist (runtime-error, deadcode-finding, …): staged L4 supply
                # awaiting review/graduation — real fuel, NOT a wiring gap.
                staged += 1
                prefix = src.split(":", 1)[0] if src else "(no source)"
                staged_sources[prefix] = staged_sources.get(prefix, 0) + 1
    # Land rate over DECIDED dispatches (ladder row: raise the daily cap after
    # N dispatches with >=50% land rate). "attempted" is non-terminal (a later
    # reprobe may upgrade it), but counting it in the denominator keeps the
    # rate honest rather than flattering.
    decided = sum(outcomes.values())
    landed = outcomes.get("landed", 0)
    land_rate = (landed / decided) if decided else None
    metrics = {
        "candidates": len(cand),
        "by_scope": by_scope,
        "dispatchable": dispatchable,
        "staged": staged,
        "staged_sources": staged_sources,
        "in_flight": in_flight,
        "applied": applied,
        "failed_or_rolled_back": failed,
        "legacy_in_flight": legacy_in_flight,
        "dispatched_total": dispatch_total,
        "dispatched_today": dispatch_today,
        "dispatch_outcomes": outcomes,
        "land_rate": land_rate,
        "last_tick_at_ms": int(runtime_status.get("lastTickAtMs") or 0),
        "last_result": str(runtime_status.get("lastResult") or ""),
        "last_successful_at_ms": int(runtime_status.get("lastSuccessfulAtMs") or 0),
        "consecutive_failures": int(runtime_status.get("consecutiveFailures") or 0),
        "oldest_pending_age_ms": (
            max(0, now_ms - oldest_pending_at) if now_ms is not None and oldest_pending_at else 0
        ),
    }
    outcome_note = ""
    if decided:
        parts = ", ".join(f"{k}:{v}" for k, v in sorted(outcomes.items()))
        outcome_note = f" · outcomes {parts} (land rate {land_rate:.0%})"
    if in_flight > 0:
        return LayerStatus("L4", "source self-edit", LIVE, metrics,
                           f"{in_flight} candidates crossing PR/deploy/watch lifecycle"
                           + outcome_note)
    if legacy_in_flight > 0:
        return LayerStatus("L4", "source self-edit", LIVE, metrics,
                           f"{legacy_in_flight} legacy dispatches in flight"
                           + outcome_note)
    if applied > 0:
        return LayerStatus("L4", "source self-edit", LIVE, metrics,
                           f"{applied} source edits survived merged deployment rollback watch"
                           + outcome_note)
    if dispatchable > 0 and metrics["consecutive_failures"] > 0:
        return LayerStatus("L4", "source self-edit", STARVED, metrics,
                           f"{dispatchable} dispatchable code candidates, dispatcher failed "
                           f"{metrics['consecutive_failures']} consecutive ticks "
                           f"(last={metrics['last_result'] or 'unknown'})"
                           + outcome_note)
    if dispatchable > 0:
        return LayerStatus("L4", "source self-edit", IDLE, metrics,
                           f"{dispatchable} dispatchable code candidates queued; "
                           "no authoritative dispatch is in flight"
                           + outcome_note)
    if len(cand) == 0:
        return LayerStatus("L4", "source self-edit", IDLE, metrics,
                           "no self-correction candidates — capture funnel idle"
                           + outcome_note)
    if staged > 0:
        staged_summary = ", ".join(f"{k}:{v}" for k, v in sorted(staged_sources.items()))
        return LayerStatus("L4", "source self-edit", STARVED, metrics,
                           f"{staged} code candidates staged from non-dispatch sources ({staged_summary}) "
                           "— propose-only supply awaiting allowlist graduation"
                           + outcome_note)
    scope_summary = ", ".join(f"{k}:{v}" for k, v in sorted(by_scope.items()))
    return LayerStatus("L4", "source self-edit", STARVED, metrics,
                       f"{len(cand)} candidates ({scope_summary}) but 0 are code-scope from "
                       f"{'/'.join(sources)} — no source produces code candidates (wiring gap)"
                       + outcome_note)


def assess(data_dir: str, now_ms: int) -> list[LayerStatus]:
    """Read every ledger under data_dir and classify all four layers."""
    genesis = read_jsonl(os.path.join(data_dir, "skill_genesis_log.jsonl"))
    revisions = read_jsonl(os.path.join(data_dir, "meta_evolution_log.jsonl"))
    judge = read_jsonl(os.path.join(data_dir, "judge_accuracy_log.jsonl"))
    candidates = read_jsonl(os.path.join(data_dir, "self_correction_candidates.jsonl"))
    # Mirror genesis.Tracker.AutoAdoptFrozen: the freeze file is JSONL of
    # DriftVerdict rows; only the LAST record's frozen bool matters. Presence
    # alone used to force FROZEN forever after an unfreeze write (frozen:false).
    frozen = _auto_adopt_frozen(os.path.join(data_dir, "auto_adopt_freeze.json"))

    dispatch_dir = os.path.join(data_dir, "coding_dispatch")
    dispatch_total = dispatch_today = 0
    outcomes: dict[str, int] = {}
    marker_outcomes: dict[str, str] = {}
    dispatched_ids: set[str] = set()
    today_cutoff = now_ms - (now_ms % DAY_MS)
    try:
        for name in os.listdir(dispatch_dir):
            if not name.endswith(".json"):
                continue
            path = os.path.join(dispatch_dir, name)
            dispatch_total += 1
            try:
                with open(path, encoding="utf-8", errors="replace") as handle:
                    marker = json.load(handle)
                marker_id = str(marker.get("id") or name[:-5])
                if marker_id:
                    dispatched_ids.add(marker_id)
                outcome = marker.get("outcome")
                if outcome:
                    outcomes[outcome] = outcomes.get(outcome, 0) + 1
                    if marker_id:
                        marker_outcomes[marker_id] = str(outcome)
            except (OSError, json.JSONDecodeError, TypeError, AttributeError):
                pass
            try:
                if os.path.getmtime(path) * 1000 >= today_cutoff:
                    dispatch_today += 1
            except OSError:
                continue
    except OSError:
        pass

    grad_rows = _graduation_rows(os.path.join(data_dir, "graduation_state.json"))
    runtime_status: dict[str, Any] = {}
    try:
        with open(os.path.join(data_dir, "coding_dispatch_status.json"), encoding="utf-8") as handle:
            loaded = json.load(handle)
            if isinstance(loaded, dict):
                runtime_status = loaded
    except (OSError, json.JSONDecodeError, TypeError):
        pass

    return [
        assess_l1(genesis, now_ms),
        assess_l2(revisions, frozen, now_ms),
        assess_l3(judge, genesis, now_ms),
        assess_l4(
            candidates, dispatch_total, dispatch_today, outcomes, dispatched_ids,
            grad_rows, runtime_status, now_ms, marker_outcomes,
        ),
        assess_ladder(genesis, revisions, candidates, outcomes, grad_rows=grad_rows),
    ]


# --- Graduation-ladder dashboard (mirrors genesis/rsi_ladder.go) ---
# Evidence thresholds for the machine-checkable ladder rows. The engine NEVER
# flips a lock — READY means "evidence met, operator decision available".
LADDER_DISPATCH_MIN_DECIDED = 5
LADDER_DISPATCH_MIN_LAND_RATE = 0.5
LADDER_CALIBRATION_BENCH_TARGET = 10
# P5-2 window opened 2026-07-12 (rsi-calibration.conf) — earlier bench samples
# belong to the default-cadence era.
LADDER_CALIBRATION_OPENED_MS = 1_783_900_800_000  # 2026-07-12T00:00:00Z

LADDER_READY = "준비됨"
LADDER_GROWING = "축적 중"
LADDER_MANUAL = "수동 판단"
LADDER_DONE = "완료"


def assess_ladder(genesis_events: list[dict], revisions: list[dict],
                  candidate_rows: list[dict], outcomes: dict[str, int],
                  grad_rows: dict | None = None) -> LayerStatus:
    """Continuously score every machine-checkable graduation-ladder row."""
    rows: list[tuple[str, str, str]] = []  # (title, state, detail)

    grad = grad_rows or {}
    ep = _eprocess_readiness(genesis_events)
    ep_unlocked = (grad.get("eprocess-cutover") or {}).get("unlocked") \
        and os.environ.get("DENEB_EPROCESS_OWNS_ROLLBACK") != "0"
    if os.environ.get("DENEB_EPROCESS_OWNS_ROLLBACK") == "1" or ep_unlocked:
        rows.append(("e-process 컷오버", LADDER_DONE, f"발화 소유 중 (라벨 n={ep['eprocess_labels']})"))
    elif ep["eprocess_cutover_ready"]:
        rows.append(("e-process 컷오버", LADDER_READY,
                     f"n={ep['eprocess_labels']}·합치 {ep['eprocess_agreement']:.0%} — 플립 결정 가능"))
    else:
        rows.append(("e-process 컷오버", LADDER_GROWING, f"라벨 {ep['eprocess_labels']}/20"))

    cap_row = grad.get("dispatch-cap") or {}
    decided = sum(outcomes.values())
    landed = outcomes.get("landed", 0)
    if cap_row.get("unlocked"):
        rows.append(("배차 캡 상향", LADDER_DONE, f"실행됨 — 일일 캡 {cap_row.get('value') or '?'} (자동 졸업)"))
    elif decided == 0:
        rows.append(("배차 캡 상향", LADDER_GROWING, "판정된 배차 0건"))
    else:
        rate = landed / decided
        detail = f"판정 {decided}건·랜딩률 {rate:.0%}"
        if decided >= LADDER_DISPATCH_MIN_DECIDED and rate >= LADDER_DISPATCH_MIN_LAND_RATE:
            rows.append(("배차 캡 상향", LADDER_READY, detail + " — 롤백 0건은 수동 확인 후 캡 결정"))
        else:
            rows.append(("배차 캡 상향", LADDER_GROWING, detail))

    cand = _merge_candidates(candidate_rows)
    staged_sources: dict[str, int] = {}
    for rec in cand.values():
        st = rec.get("status") or "proposed"
        if rec.get("scope") != "code" or st not in ("proposed", "accepted"):
            continue
        src = rec.get("source") or ""
        if src.startswith(_dispatchable_sources(grad)):
            continue
        prefix = src.split(":", 1)[0] if src else "(no source)"
        staged_sources[prefix] = staged_sources.get(prefix, 0) + 1
    if staged_sources:
        parts = "·".join(f"{k} {v}건" for k, v in sorted(staged_sources.items()))
        rows.append(("스테이징 소스 졸업", LADDER_READY, "첫 배치 리뷰 가능: " + parts))
    else:
        rows.append(("스테이징 소스 졸업", LADDER_GROWING, "스테이징 후보 0건 (마이너 대기)"))

    benched: dict[str, int] = {}
    for r in revisions:
        if (r.get("createdAt") or 0) < LADDER_CALIBRATION_OPENED_MS or r.get("action"):
            continue
        if r.get("benchIncumbent") or r.get("benchShadow") or r.get("benchGenesis"):
            epoch = r.get("epoch") or "?"
            benched[epoch] = benched.get(epoch, 0) + 1
    cal_detail = (f"epoch별 벤치 n: producer {benched.get('producer', 0)}"
                  f"·evaluator {benched.get('evaluator', 0)}·genesis {benched.get('genesis', 0)}"
                  f" (목표 각 {LADDER_CALIBRATION_BENCH_TARGET})")
    if all(benched.get(e, 0) >= LADDER_CALIBRATION_BENCH_TARGET
           for e in ("producer", "evaluator", "genesis")):
        rows.append(("캘리브레이션 창 종료", LADDER_READY, cal_detail + " — 드롭인 제거 결정 가능"))
    else:
        rows.append(("캘리브레이션 창 종료", LADDER_GROWING, cal_detail))

    rows.append(("소스 자동적용 티어", LADDER_MANUAL, "배포 롤백 1회 완주가 증거 — 운영자 판단 행"))

    metrics = {title: state for title, state, _ in rows}
    ready = [f"{t}({d})" for t, s, d in rows if s == LADDER_READY]
    if ready:
        return LayerStatus("GRAD", "graduation ladder", LIVE, metrics,
                           "증거 충족 — 운영자 결정 가능: " + " · ".join(ready))
    growing = " · ".join(f"{t}: {d}" for t, s, d in rows if s == LADDER_GROWING)
    return LayerStatus("GRAD", "graduation ladder", DATA_GATED, metrics,
                       "전 행 증거 축적 중 — " + growing)


# A layer is "turning" for the headline count when it is not merely idle/
# starved. The graduation-ladder dashboard (GRAD) is an evidence surface, not
# a loop — it never counts toward the headline numerator or denominator.
def turning(layers: list[LayerStatus]) -> int:
    return sum(1 for layer in layers if layer.key != "GRAD" and layer.state in (LIVE, FROZEN))


_STATE_GLYPH = {LIVE: "●", DATA_GATED: "◐", STARVED: "○", FROZEN: "❄", IDLE: "·"}


def print_summary(layers: list[LayerStatus], stream: TextIO) -> None:
    loop_count = sum(1 for layer in layers if layer.key != "GRAD")
    print(f"\nRSI loop status  ({turning(layers)}/{loop_count} turning)", file=stream)
    print("─" * 72, file=stream)
    for layer in layers:
        glyph = _STATE_GLYPH.get(layer.state, "?")
        print(f"  {glyph} {layer.key}  {layer.title:<22} {layer.state}", file=stream)
        print(f"      · {layer.diagnosis}", file=stream)
    print("─" * 72, file=stream)


def render_markdown(layers: list[LayerStatus], now_ms: int, data_dir: str) -> str:
    """Render a point-in-time status document from canonical ledgers.

    This is the only supported "current RSI status" document; architecture
    docs intentionally contain no live counters that can silently go stale.
    """
    generated = datetime.datetime.fromtimestamp(
        now_ms / 1000, tz=datetime.timezone.utc).isoformat().replace("+00:00", "Z")
    lines = [
        "# Deneb RSI live status",
        "",
        f"> Generated {generated} from `{os.path.abspath(data_dir)}`. Do not edit by hand.",
        "",
        f"**Turning: {turning(layers)}/{len(layers)}**",
        "",
        "| Layer | State | Diagnosis |",
        "|---|---|---|",
    ]
    for layer in layers:
        diagnosis = layer.diagnosis.replace("|", "\\|").replace("\n", " ")
        lines.append(f"| {layer.key} — {layer.title} | {layer.state} | {diagnosis} |")
    lines.extend(["", "## Metrics", ""])
    for layer in layers:
        lines.extend([
            f"### {layer.key}",
            "",
            "```json",
            json.dumps(layer.metrics, ensure_ascii=False, indent=2, sort_keys=True),
            "```",
            "",
        ])
    return "\n".join(lines).rstrip() + "\n"


def _write_atomic(path: str, content: str) -> None:
    path = os.path.abspath(os.path.expanduser(path))
    os.makedirs(os.path.dirname(path), exist_ok=True)
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as handle:
        handle.write(content)
    os.replace(tmp, path)


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Deneb RSI loop-status surface.")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--json", action="store_true")
    mode.add_argument("--markdown", action="store_true",
                      help="render the live status document from ledgers")
    parser.add_argument("--write-markdown", metavar="PATH",
                        help="atomically refresh a generated status document")
    parser.add_argument("--data-dir", default=DEFAULT_DATA_DIR)
    parser.add_argument("--now-ms", type=int, default=None, help="override clock (tests)")
    return parser


def main(argv: list[str] | None = None, *, stdout: TextIO | None = None, stderr: TextIO | None = None) -> int:
    args = _parser().parse_args(argv)
    out = stdout if stdout is not None else sys.stdout
    err = stderr if stderr is not None else sys.stderr
    now_ms = args.now_ms if args.now_ms is not None else int(time.time() * 1000)
    layers = assess(args.data_dir, now_ms)

    markdown = render_markdown(layers, now_ms, args.data_dir)
    if args.write_markdown:
        _write_atomic(args.write_markdown, markdown)

    if args.markdown:
        print(markdown, end="", file=out)
        return 0

    if args.json:
        print(json.dumps(
            {"turning": turning(layers), "layers": [
                {"key": layer.key, "title": layer.title, "state": layer.state,
                 "metrics": layer.metrics, "diagnosis": layer.diagnosis}
                for layer in layers]},
            ensure_ascii=False, indent=1), file=out)
    else:
        print_summary(layers, err)

    print("DENEB_RSI_STATUS " + " ".join(f"{layer.key}={layer.state}" for layer in layers), file=out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
