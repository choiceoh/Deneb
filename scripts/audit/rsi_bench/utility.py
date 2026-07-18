"""Utility domain — operator-visible RSI outcomes (rubric 1.1)."""

from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Any

from .cache import self_evolution_from_cache
from .ledgers import (
    ClosureWindow,
    DispatchWindow,
    GenesisWindow,
    MetaWindow,
    WatchWindow,
    data_dir,
    load_closure_window,
    load_dispatch_window,
    load_genesis_window,
    load_meta_window,
    load_watch_window,
)
from .model import (
    DOMAIN_WEIGHTS,
    MIN_RESOLVED_FOR_HARD,
    SOFT_RESOLVE_SCORE_CAP,
    UNMEASURED_RATE_FLOOR,
    Domain,
    Evidence,
    Finding,
    Metric,
    clamp,
    grade_rate_high_good,
    stable_id,
)

BOOTSTRAP = {
    "closure-land": 22.0,
    "operator-verdict": 22.0,
    "codebase-delta": 25.0,
    "retention-proxy": 25.0,
    "dispatch-land": 22.0,
}


def _read_json(path: Path) -> dict[str, Any] | None:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    return data if isinstance(data, dict) else None


def score_closure_land(closure: ClosureWindow | None = None, data: Path | None = None) -> tuple[float, Evidence, list[Finding]]:
    """Shared with Health Fitness finding-land."""
    closure = closure or load_closure_window(data)
    findings: list[Finding] = []
    if closure.proposed <= 0:
        return (
            BOOTSTRAP["closure-land"],
            Evidence("utility-closure-land", "bootstrap", "no self_correction candidates", required=False),
            findings,
        )
    land_rate = closure.landed / closure.proposed
    revert_rate = closure.reverted / closure.proposed
    raw = 100.0 * land_rate * (1.0 - min(1.0, revert_rate * 2.0))
    score = clamp(0.65 * raw + 0.35 * BOOTSTRAP["closure-land"])
    if land_rate < 0.05:
        findings.append(
            Finding(
                id=stable_id("closure-land", f"{closure.landed}/{closure.proposed}"),
                domain="utility",
                pillar="closure-land",
                severity="high",
                path="self_correction_candidates.jsonl",
                evidence=(
                    f"proposed={closure.proposed} landed={closure.landed} "
                    f"reverted={closure.reverted} dispatched={closure.dispatched}"
                ),
                why="SkillSmith/L4: proposals without land means the loop does not close",
                remediation="Dispatch accepted L4 candidates through PR/CI/deploy watch",
                verify="python3 scripts/audit/rsi-bench.py --format json",
                priority=92.0,
            )
        )
    return (
        score,
        Evidence(
            "utility-closure-land",
            "measured",
            f"proposed={closure.proposed} landed={closure.landed} reverted={closure.reverted}",
        ),
        findings,
    )


def score_operator_verdict(meta: MetaWindow | None = None, data: Path | None = None) -> tuple[float, Evidence, list[Finding]]:
    meta = meta or load_meta_window(data)
    findings: list[Finding] = []
    export = os.environ.get("DENEB_FEED_CARD_EXPORT", "").strip()
    adopted = rejected = reverted = 0
    source = "meta_evolution_log"
    if export:
        payload = _read_json(Path(export))
        if payload:
            adopted = int(payload.get("adopted") or 0)
            rejected = int(payload.get("rejected") or 0)
            reverted = int(payload.get("reverted") or 0)
            source = "DENEB_FEED_CARD_EXPORT"
    if adopted + rejected + reverted <= 0:
        adopted, rejected, reverted = meta.adopted, meta.rejected, meta.reverted
    total = adopted + rejected + reverted
    if total <= 0:
        return (
            BOOTSTRAP["operator-verdict"],
            Evidence("utility-operator-verdict", "bootstrap", "no operator verdicts in window", required=False),
            findings,
        )
    adopt_rate = adopted / total
    revert_rate = reverted / total
    score = clamp(100.0 * adopt_rate * (1.0 - revert_rate))
    if total < 3:
        score = clamp(0.25 * score + 0.75 * BOOTSTRAP["operator-verdict"])
        score = min(score, 38.0)
    return (
        score,
        Evidence(
            "utility-operator-verdict",
            "measured",
            f"source={source} adopted={adopted} rejected={rejected} reverted={reverted}",
        ),
        findings,
    )


def score_codebase_delta(
    root: Path, *, cache: dict[str, Any] | None = None
) -> tuple[float, Evidence, list[Finding]]:
    """Prefer live snapshot, then cache-embedded health overall, then bootstrap."""
    findings: list[Finding] = []
    baseline = _read_json(root / "scripts" / "audit" / "health-v3-baseline.json")
    snapshot = _read_json(root / "scripts" / "audit" / "health-v3-snapshot.json")
    snap_overall: float | None = None
    source = "snapshot"
    if snapshot:
        snap_overall = float((snapshot.get("score") or {}).get("overall", snapshot.get("overall", 0.0)))
    elif cache and isinstance(cache.get("health_v3"), dict):
        hv = cache["health_v3"]
        if hv.get("overall") is not None:
            snap_overall = float(hv["overall"])
            source = "cache"
    if not baseline or snap_overall is None:
        return (
            BOOTSTRAP["codebase-delta"],
            Evidence(
                "utility-codebase-delta",
                "bootstrap",
                "health-v3 baseline/snapshot/cache overall missing",
                required=False,
            ),
            findings,
        )
    base_overall = float(baseline.get("overall", 0.0))
    delta = snap_overall - base_overall
    score = clamp(25.0 + delta * 6.0)
    if delta < -0.3:
        findings.append(
            Finding(
                id=stable_id("codebase-delta", f"{delta:.2f}"),
                domain="utility",
                pillar="codebase-delta",
                severity="high",
                path="scripts/audit/health-v3-snapshot.json",
                evidence=f"live {snap_overall:.1f} − baseline {base_overall:.1f} = {delta:+.1f} ({source})",
                why="P5-5: outer-loop utility regressing on the codebase",
                remediation="Land structural health findings before raising RSI cadence",
                verify="python3 scripts/audit/health-bench-v3.py --check",
                priority=85.0,
            )
        )
    return (
        score,
        Evidence(
            "utility-codebase-delta",
            "measured",
            f"source={source} live {snap_overall:.1f} − baseline {base_overall:.1f} = {delta:+.1f}",
        ),
        findings,
    )


def _score_retention(
    genesis: GenesisWindow, health: dict[str, Any], watch: WatchWindow
) -> tuple[float, Evidence, list[Finding]]:
    if health.get("confirm_rate") is not None and int(health.get("resolved_evolves_7d") or 0) >= MIN_RESOLVED_FOR_HARD:
        rate = float(health["confirm_rate"])
        resolved = int(health["resolved_evolves_7d"])
        mode = "hard"
    elif genesis.resolved >= MIN_RESOLVED_FOR_HARD:
        rate = genesis.confirm_rate or 0.0
        resolved = genesis.resolved
        mode = "hard"
    elif watch.soft_confirmed >= MIN_RESOLVED_FOR_HARD:
        rate = 1.0
        resolved = watch.soft_confirmed
        mode = "soft"
    else:
        return (
            UNMEASURED_RATE_FLOOR - 3.0,
            Evidence(
                "utility-retention-proxy",
                "bootstrap",
                f"resolved=0; CPE retention unmeasured (watches={watch.watches} soft={watch.soft_confirmed})",
                required=False,
            ),
            [],
        )
    score = grade_rate_high_good(rate, soft=0.70, hard=0.20)
    if mode == "soft":
        score = min(score, SOFT_RESOLVE_SCORE_CAP)
    return (
        score,
        Evidence(
            "utility-retention-proxy",
            "measured",
            f"confirmRate={rate:.3f} resolved={resolved} mode={mode}",
        ),
        [],
    )


def _score_dispatch(dispatch: DispatchWindow) -> tuple[float, Evidence, list[Finding]]:
    if dispatch.files <= 0:
        return (
            BOOTSTRAP["dispatch-land"],
            Evidence("utility-dispatch-land", "bootstrap", "no coding_dispatch files", required=False),
            [],
        )
    # accepted without land is weak utility; land/watch_passed is strong. Lands
    # are RHAE-weighted (ledgers.land_efficiency): a first-attempt land counts
    # 1.0, retries decay quadratically — landing is only full utility when the
    # loop lands efficiently.
    progress = dispatch.land_eff + 0.35 * dispatch.accepted
    raw = 100.0 * progress / max(dispatch.files, 1)
    if dispatch.rolled_back:
        raw *= max(0.0, 1.0 - dispatch.rolled_back / max(dispatch.files, 1))
    score = clamp(0.7 * raw + 0.3 * BOOTSTRAP["dispatch-land"])
    findings: list[Finding] = []
    if dispatch.accepted > 0 and dispatch.landed == 0:
        findings.append(
            Finding(
                id=stable_id("dispatch-land", "accepted-no-land"),
                domain="utility",
                pillar="dispatch-land",
                severity="medium",
                path="coding_dispatch",
                evidence=f"files={dispatch.files} accepted={dispatch.accepted} landed={dispatch.landed}",
                why="L4 dispatches accepted but none reached land (marker outcome / watch_passed)",
                remediation="Advance coding_dispatch through PR merge and deploy watch; ensure outcome=landed",
                verify="python3 scripts/audit/rsi-bench.py --format json",
                priority=75.0,
            )
        )
    return (
        score,
        Evidence(
            "utility-dispatch-land",
            "measured",
            f"files={dispatch.files} accepted={dispatch.accepted} landed={dispatch.landed} "
            f"landEff={dispatch.land_eff:.2f} failed={dispatch.failed}",
        ),
        findings,
    )


def evaluate_utility(
    root: Path,
    *,
    cache: dict[str, Any] | None = None,
    data: Path | None = None,
) -> Domain:
    data_path = data or data_dir()
    health = self_evolution_from_cache(cache)
    genesis = load_genesis_window(data_path)
    if health:
        for src, attr in (
            ("evolve_confirmed_7d", "confirmed"),
            ("evolve_rolled_back_7d", "rolled_back"),
        ):
            if health.get(src) is not None:
                setattr(genesis, attr, int(health[src]))
    meta = load_meta_window(data_path)
    closure = load_closure_window(data_path)
    watch = load_watch_window(data_path)
    dispatch = load_dispatch_window(data_path)

    c_score, c_ev, c_f = score_closure_land(closure)
    o_score, o_ev, o_f = score_operator_verdict(meta)
    d_score, d_ev, d_f = score_codebase_delta(root, cache=cache)
    r_score, r_ev, r_f = _score_retention(genesis, health, watch)
    x_score, x_ev, x_f = _score_dispatch(dispatch)

    # 25+20+20+15+20 = 100
    metrics = [
        Metric("closure-land", "Closure land", 25, c_score, "SkillSmith/L4 propose→land", {}, c_f),
        Metric("operator-verdict", "Operator verdict", 20, o_score, "ANCHOR feed-card utility", {}, o_f),
        Metric("codebase-delta", "Codebase delta", 20, d_score, "P5-5 health-v3 live−baseline", {}, d_f),
        Metric("retention-proxy", "Retention proxy", 15, r_score, "CPE confirm / soft watch keep", {}, r_f),
        Metric("dispatch-land", "Dispatch land", 20, x_score, "L4 coding_dispatch land fidelity", {}, x_f),
    ]
    return Domain(
        id="utility",
        title="Utility",
        weight=DOMAIN_WEIGHTS["utility"],
        metrics=metrics,
        evidence=[c_ev, o_ev, d_ev, r_ev, x_ev],
        ratcheted=True,  # 1.2: Utility enters the CI ratchet with Process
    )
