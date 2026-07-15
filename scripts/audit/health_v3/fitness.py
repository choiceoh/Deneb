"""Fitness domain — RSI operator-visible utility (advisory until promoted)."""

from __future__ import annotations

import json
import os
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from .model import DOMAIN_WEIGHTS, Domain, Evidence, Finding, Metric, clamp, stable_id

# Bootstrap floors matching docs/research/health-bench-3.0.md worksheet when
# snapshot/ledger streams are absent. Explicitly not 100.
BOOTSTRAP = {
    "live-delta": 40.0,
    "trend-28d": 50.0,
    "finding-land": 42.0,
    "feed-card": 40.0,
}


def _read_json(path: Path) -> dict[str, Any] | None:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    return data if isinstance(data, dict) else None


def _grade_delta(delta: float) -> float:
    """Map overall delta (points) onto 0–100. Flat → ~40, +5 → ~70, -5 → ~15."""
    return clamp(40.0 + delta * 6.0)


def _score_live_delta(root: Path) -> tuple[float, Evidence, list[Finding]]:
    baseline = _read_json(root / "scripts" / "audit" / "health-v3-baseline.json")
    snapshot = _read_json(root / "scripts" / "audit" / "health-v3-snapshot.json")
    if not baseline or not snapshot:
        return (
            BOOTSTRAP["live-delta"],
            Evidence(
                "fitness-live-delta",
                "bootstrap",
                "baseline/snapshot missing; using design bootstrap floor",
                required=False,
            ),
            [],
        )
    base_overall = float(baseline.get("overall", 0.0))
    snap_overall = float((snapshot.get("score") or {}).get("overall", snapshot.get("overall", 0.0)))
    delta = snap_overall - base_overall
    score = _grade_delta(delta)
    findings: list[Finding] = []
    if delta < -0.3:
        findings.append(
            Finding(
                id=stable_id("live-delta", f"{delta:.2f}"),
                domain="fitness",
                pillar="live-delta",
                severity="high",
                path="scripts/audit/health-v3-snapshot.json",
                evidence=f"live overall {snap_overall:.1f} is {delta:.1f} below baseline {base_overall:.1f}",
                why="Accepted structural/runtime floor is regressing in the live snapshot",
                remediation="Restore the weakest domain before raising the baseline",
                verify="python3 scripts/audit/health-bench-v3.py --check",
                priority=80.0,
            )
        )
    return (
        score,
        Evidence(
            "fitness-live-delta",
            "measured",
            f"snapshot {snap_overall:.1f} − baseline {base_overall:.1f} = {delta:+.1f}",
            required=False,
        ),
        findings,
    )


def _score_trend(root: Path) -> tuple[float, Evidence, list[Finding]]:
    history_path = root / "scripts" / "audit" / "health-v3-history.jsonl"
    if not history_path.is_file():
        return (
            BOOTSTRAP["trend-28d"],
            Evidence("fitness-trend", "bootstrap", "no history jsonl yet", required=False),
            [],
        )
    points: list[float] = []
    try:
        for line in history_path.read_text(encoding="utf-8").splitlines():
            if not line.strip():
                continue
            row = json.loads(line)
            overall = float((row.get("score") or {}).get("overall", row.get("overall", 0.0)))
            points.append(overall)
    except (OSError, json.JSONDecodeError, TypeError, ValueError) as exc:
        return (
            BOOTSTRAP["trend-28d"],
            Evidence("fitness-trend", "unavailable", str(exc), required=False),
            [],
        )
    if len(points) < 2:
        return (
            BOOTSTRAP["trend-28d"],
            Evidence("fitness-trend", "bootstrap", f"only {len(points)} history points", required=False),
            [],
        )
    window = points[-28:]
    slope = window[-1] - window[0]
    score = clamp(50.0 + slope * 4.0)
    return (
        score,
        Evidence("fitness-trend", "measured", f"n={len(window)} slope={slope:+.1f}", required=False),
        [],
    )


def _data_dir() -> Path:
    override = os.environ.get("DENEB_DATA_DIR", "").strip()
    if override:
        return Path(override)
    home = Path.home()
    return home / ".deneb" / "data"


def _score_finding_land() -> tuple[float, Evidence, list[Finding]]:
    """Thin re-export of RSI Bench closure-land (shared L4 fidelity signal)."""
    try:
        from rsi_bench.utility import score_closure_land
    except ImportError:
        return (
            BOOTSTRAP["finding-land"],
            Evidence("fitness-finding-land", "unavailable", "rsi_bench not importable", required=False),
            [],
        )
    score, _ev, _findings = score_closure_land()
    return (
        score,
        Evidence("fitness-finding-land", "measured", f"re-export rsi closure-land score={score:.1f}"),
        [],
    )


def _score_feed_card() -> tuple[float, Evidence, list[Finding]]:
    """Thin re-export of RSI Bench operator-verdict."""
    try:
        from rsi_bench.utility import score_operator_verdict
    except ImportError:
        return (
            BOOTSTRAP["feed-card"],
            Evidence("fitness-feed-card", "unavailable", "rsi_bench not importable", required=False),
            [],
        )
    score, ev, _findings = score_operator_verdict()
    status = ev.status if ev.status in {"measured", "bootstrap", "unavailable"} else "bootstrap"
    return (
        score,
        Evidence("fitness-feed-card", status, f"re-export rsi operator-verdict: {ev.detail}"),
        [],
    )


def evaluate_fitness(root: Path) -> Domain:
    live_score, live_ev, live_findings = _score_live_delta(root)
    trend_score, trend_ev, trend_findings = _score_trend(root)
    land_score, land_ev, land_findings = _score_finding_land()
    feed_score, feed_ev, feed_findings = _score_feed_card()

    metrics = [
        Metric(
            "live-delta",
            "Live delta",
            30,
            live_score,
            "snapshot.overall − baseline.overall",
            {},
            live_findings,
        ),
        Metric(
            "trend-28d",
            "Trend 28d",
            25,
            trend_score,
            "Non-regression over snapshot history",
            {},
            trend_findings,
        ),
        Metric(
            "finding-land",
            "Finding→land fidelity",
            25,
            land_score,
            "health-finding propose/dispatch/land fidelity",
            {},
            land_findings,
        ),
        Metric(
            "feed-card",
            "Feed-card utility",
            20,
            feed_score,
            "7d adopt/reject/revert utility",
            {},
            feed_findings,
        ),
    ]
    return Domain(
        id="fitness",
        title="Fitness",
        weight=DOMAIN_WEIGHTS["fitness"],
        metrics=metrics,
        evidence=[live_ev, trend_ev, land_ev, feed_ev],
        ratcheted=False,  # advisory until P5-5 fidelity promotion
    )


def append_history(root: Path, report_dict: dict[str, Any]) -> None:
    path = root / "scripts" / "audit" / "health-v3-history.jsonl"
    row = {
        "ts": datetime.now(timezone.utc).isoformat(),
        "overall": report_dict.get("score", {}).get("overall"),
        "score": report_dict.get("score"),
        "revision": report_dict.get("revision"),
    }
    with path.open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(row, sort_keys=True) + "\n")
