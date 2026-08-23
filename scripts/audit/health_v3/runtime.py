"""Runtime domain — tightened world-class bars over runtime_health signals."""

from __future__ import annotations

import json
import math
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from .model import DOMAIN_WEIGHTS, Domain, Evidence, Finding, Metric, clamp, stable_id

AUDIT_DIR = Path(__file__).resolve().parent.parent
if str(AUDIT_DIR) not in sys.path:
    sys.path.insert(0, str(AUDIT_DIR))

import runtime_health as rh  # noqa: E402

# Tightened soft/hard bars (design worksheet → ~58.5 on 2026-07-15 live window).
CRASH_PER_DAY_HARD = 0.5
STABILITY_CEILING = 0.90
ERR_PER_RUN_SOFT, ERR_PER_RUN_HARD = 0.02, 0.25
LLM_HARD_PER_RUN_SOFT, LLM_HARD_PER_RUN_HARD = 0.0, 0.036
TIMEOUT_FRAC_SOFT, TIMEOUT_FRAC_HARD = 0.005, 0.05
TOOLERR_FRAC_SOFT, TOOLERR_FRAC_HARD = 0.005, 0.023
LAT_P95_SOFT, LAT_P95_HARD = 40.0, 180.0

DEFAULT_CACHE = AUDIT_DIR / "health-v3-runtime-cache.json"
DEFAULT_TTL_HOURS = 72


def state_cache_path() -> Path:
    """Live runtime-cache location — FIXED under ~/.deneb/data.

    Same convention (and the same gotcha) as the Tracker ledgers and
    graduation_state.json: DENEB_STATE_DIR does not move it, so the Go paths,
    the shell executors, and every bench CLI read one file.

    Why not the checked-in AUDIT_DIR copy: that file is tracked, so the daily
    deep refresh could not persist to it — refresh-bench-snapshots.sh reverted
    it right after writing to keep the auto-deploy tree clean. Result: every
    fast-profile consumer read the last COMMITTED cache, which ran past the
    72h TTL three days after its commit and failed the whole v3 CLI fail-closed
    (live 2026-07-18 → 07-27: the health-finding miner silently fell back to
    the v2 structural bench for nine days). The tracked copy stays as a seed
    for hosts that have never run a refresh; the live cache lives here.
    """
    return Path.home() / ".deneb" / "data" / "health-v3-runtime-cache.json"


# Score band reserved for values PAST the hard bar. rh.graded clamps to exactly
# 0.0 there, and with bars this tight production sits past five of six of them at
# once — so every pillar reads 0.0 and the domain stops being a measurement.
# Observed 2026-07-25: runtime 16.2 with error-rate / latency / llm-serving /
# tool-reliability / turn-reliability ALL exactly 0.0, unchanged across four
# consecutive nights, so the nightly ratchet could not distinguish "recovering"
# from "still broken" — the operator's actual question.
#
# This does NOT lower a bar: crossing hard still costs 95 of 100 points, every
# pillar past its bar still emits a `high` finding (the cut is 45), and no baseline
# is touched. It only keeps the tail informative instead of flat.
#
# COST, stated plainly: compressing the ramp into [BAND, 100] makes the same input
# score up to ~+3.6 higher inside [soft, hard]. Against baselines captured under
# the old curve the runtime ratchet is that much more lenient there, so a real
# regression of a few points could hide. Bounded deliberately — 0.05 buys usable
# sub-bar resolution for the least ramp distortion — and it is why the band is not
# larger. Runtime baselines should be re-recorded once production climbs back into
# the ramp region. Structure, the heavily weighted domain, is untouched, and this
# nightly is advisory (it opens a tracking issue, it does not gate).
SUB_BAR_BAND = 0.05


def _grade(value: float, soft: float, hard: float) -> float:
    """Graded score that keeps resolution below the hard bar.

    [0, soft] → 100, ramping down to SUB_BAR_BAND at `hard`, then decaying by how
    many DOUBLINGS past the bar the value sits: hard→8.0, 2×hard→4.0, 4×hard→2.7.
    Continuous at `hard` and monotonically decreasing throughout, so a worse
    measurement can never score higher than a better one.

    Deliberately local to health-v3: `rh.graded` stays untouched because
    runtime_health.py shares it and carries its own (looser) bars and baseline.
    """
    ramp = rh.graded(value, soft, hard)
    if hard <= 0 or value <= hard:
        return 100.0 * (SUB_BAR_BAND + (1.0 - SUB_BAR_BAND) * ramp)
    doublings = math.log2(value / hard)
    return 100.0 * SUB_BAR_BAND / (1.0 + doublings)


def score_signals(signals: rh.Signals) -> list[Metric]:
    if signals.runs <= 0:
        raise rh.InsufficientDataError("0 completed runs")
    if not signals.agent_ms:
        raise rh.InsufficientDataError(f"0 agentMs samples for {signals.runs} completed runs")

    runs = signals.runs
    days = max(signals.days, 0.5)
    crashes_per_day = signals.crashes / days
    err_per_run = signals.other_errors / runs
    llm_hard_per_run = signals.llm_hard / runs
    timeout_frac = signals.timeout_runs / runs
    toolerr_frac = signals.tool_errors / max(signals.tool_calls, 1)
    # Same cohort rule as runtime_health: latency is a user-facing question, and
    # automation lanes pin p95 to their own budget caps. Windows written before
    # the runKind label fall back to every run.
    latency_ms = signals.interactive_ms or signals.agent_ms
    latency_scoped = bool(signals.interactive_ms)
    interactive_runs = sum(
        count for kind, count in signals.kind_runs.items() if kind in rh.INTERACTIVE_KINDS
    )
    latency_denominator = interactive_runs if latency_scoped else runs
    p95 = rh.percentile(latency_ms, 0.95) / 1000.0
    tool_coverage = min(1.0, signals.tool_call_reports / runs)
    latency_coverage = min(1.0, len(latency_ms) / max(latency_denominator, 1))

    stability = clamp(_grade(crashes_per_day, 0.0, CRASH_PER_DAY_HARD) * STABILITY_CEILING)
    error_rate = clamp(_grade(err_per_run, ERR_PER_RUN_SOFT, ERR_PER_RUN_HARD))
    llm_serving = clamp(_grade(llm_hard_per_run, LLM_HARD_PER_RUN_SOFT, LLM_HARD_PER_RUN_HARD))
    turn_reliability = clamp(_grade(timeout_frac, TIMEOUT_FRAC_SOFT, TIMEOUT_FRAC_HARD))
    tool_reliability = clamp(
        _grade(toolerr_frac, TOOLERR_FRAC_SOFT, TOOLERR_FRAC_HARD) * tool_coverage
    )
    latency = clamp(_grade(p95, LAT_P95_SOFT, LAT_P95_HARD) * latency_coverage)

    def finding(metric_id: str, score: float, detail: str) -> list[Finding]:
        if score >= 70:
            return []
        severity = "high" if score < 45 else "medium"
        return [
            Finding(
                id=stable_id(metric_id, detail[:60]),
                domain="runtime",
                pillar=metric_id,
                severity=severity,
                path="journald:deneb-gateway",
                evidence=detail,
                why="Production runtime weakness raises operator-visible failure risk",
                remediation="Reduce the dominant failure class in the rolling window",
                verify="python3 scripts/audit/health-bench-v3.py --format json",
                priority=max(10.0, 100.0 - score),
            )
        ]

    return [
        Metric(
            "stability",
            "Stability",
            18,
            stability,
            "Crash/panic rate over the rolling window",
            {"crashes_per_day": round(crashes_per_day, 3)},
            finding("stability", stability, f"{signals.crashes} crashes in {days:.1f}d"),
        ),
        Metric(
            "error-rate",
            "Error rate",
            16,
            error_rate,
            "Operational errors per completed run",
            {"per_run": round(err_per_run, 3)},
            finding("error-rate", error_rate, f"{err_per_run:.3f} errors/run"),
        ),
        Metric(
            "llm-serving",
            "LLM serving",
            16,
            llm_serving,
            "Hard LLM faults that lose work",
            {"per_run": round(llm_hard_per_run, 3)},
            finding("llm-serving", llm_serving, f"{llm_hard_per_run:.3f} hard faults/run"),
        ),
        Metric(
            "turn-reliability",
            "Turn reliability",
            16,
            turn_reliability,
            "Fraction of runs that timed out",
            {"frac": round(timeout_frac, 4)},
            finding("turn-reliability", turn_reliability, f"{timeout_frac * 100:.1f}% timed out"),
        ),
        Metric(
            "tool-reliability",
            "Tool reliability",
            14,
            tool_reliability,
            "Tool call error fraction",
            {"frac": round(toolerr_frac, 4), "coverage": round(tool_coverage, 4)},
            finding("tool-reliability", tool_reliability, f"{toolerr_frac * 100:.1f}% tool errors"),
        ),
        Metric(
            "latency",
            "Latency",
            20,
            latency,
            "Run agentMs p95 under world-class bars",
            {
                "p95_s": round(p95, 1),
                "coverage": round(latency_coverage, 4),
                "scoped_to_interactive": latency_scoped,
            },
            finding("latency", latency, f"p95={p95:.0f}s"),
        ),
    ]


def signals_to_dict(signals: rh.Signals) -> dict[str, Any]:
    return {
        "runs": signals.runs,
        "timeout_runs": signals.timeout_runs,
        "agent_ms": list(signals.agent_ms),
        "interactive_ms": list(signals.interactive_ms),
        "kind_runs": dict(signals.kind_runs),
        "kind_timeouts": dict(signals.kind_timeouts),
        "turns": list(signals.turns),
        "tool_calls": signals.tool_calls,
        "tool_call_reports": signals.tool_call_reports,
        "tool_errors": signals.tool_errors,
        "crashes": signals.crashes,
        "llm_hard": signals.llm_hard,
        "llm_retries": signals.llm_retries,
        "other_errors": signals.other_errors,
        "days": signals.days,
    }


def signals_from_dict(data: dict[str, Any]) -> rh.Signals:
    return rh.Signals(
        runs=int(data.get("runs", 0)),
        timeout_runs=int(data.get("timeout_runs", 0)),
        agent_ms=[int(x) for x in data.get("agent_ms", [])],
        interactive_ms=[int(x) for x in data.get("interactive_ms", [])],
        kind_runs={str(k): int(v) for k, v in (data.get("kind_runs") or {}).items()},
        kind_timeouts={str(k): int(v) for k, v in (data.get("kind_timeouts") or {}).items()},
        turns=[int(x) for x in data.get("turns", [])],
        tool_calls=int(data.get("tool_calls", 0)),
        tool_call_reports=int(data.get("tool_call_reports", 0)),
        tool_errors=int(data.get("tool_errors", 0)),
        crashes=int(data.get("crashes", 0)),
        llm_hard=int(data.get("llm_hard", 0)),
        llm_retries=int(data.get("llm_retries", 0)),
        other_errors=int(data.get("other_errors", 0)),
        days=float(data.get("days", 7.0)),
    )


def write_cache(path: Path, signals: rh.Signals, *, ttl_hours: int = DEFAULT_TTL_HOURS) -> None:
    payload = {
        "schema_version": 3,
        "rubric_version": "3.0.0",
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "ttl_hours": ttl_hours,
        "signals": signals_to_dict(signals),
    }
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def load_cache(path: Path, *, now: datetime | None = None) -> tuple[rh.Signals, list[Evidence]]:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise FileNotFoundError(f"runtime cache unreadable: {path}: {exc}") from exc
    generated_raw = payload.get("generated_at", "")
    ttl_hours = int(payload.get("ttl_hours", DEFAULT_TTL_HOURS))
    evidence = [
        Evidence("runtime-cache", "measured", f"loaded {path.name} generated_at={generated_raw}", required=True)
    ]
    try:
        generated = datetime.fromisoformat(str(generated_raw))
        if generated.tzinfo is None:
            generated = generated.replace(tzinfo=timezone.utc)
    except ValueError:
        evidence.append(Evidence("runtime-cache-timestamp", "unavailable", "invalid generated_at", required=True))
        raise ValueError("runtime cache generated_at invalid")
    clock = now or datetime.now(timezone.utc)
    age_h = (clock - generated).total_seconds() / 3600.0
    if age_h > ttl_hours:
        evidence.append(
            Evidence(
                "runtime-cache-ttl",
                "unavailable",
                f"cache age {age_h:.1f}h exceeds ttl {ttl_hours}h",
                required=True,
            )
        )
        raise TimeoutError(f"runtime cache stale: age {age_h:.1f}h > ttl {ttl_hours}h")
    signals = signals_from_dict(payload.get("signals") or {})
    return signals, evidence


def collect_live_signals(*, since: str = "7 days ago", unit: str = "deneb-gateway") -> rh.Signals:
    raw = rh._read_journal(since, unit)
    lines = rh.journal_data_lines(raw)
    signals = rh.parse(lines)
    signals.days = rh.span_days(lines)
    if signals.runs <= 0:
        raise rh.InsufficientDataError("0 completed runs in journal window")
    return signals


def evaluate_runtime(
    root: Path,
    *,
    profile: str = "fast",
    cache_path: Path | None = None,
    refresh_cache: bool = False,
) -> Domain:
    # Explicit cache_path pins BOTH read and write to one file (tests, ad-hoc
    # runs). The default is the state-dir live cache, with the checked-in copy
    # as a read-only seed fallback — see state_cache_path() for why.
    write_target = cache_path or state_cache_path()
    read_candidates = (
        [cache_path]
        if cache_path
        else [state_cache_path(), root / "scripts" / "audit" / "health-v3-runtime-cache.json"]
    )
    evidence: list[Evidence] = []
    signals: rh.Signals | None = None

    if profile == "deep" or refresh_cache:
        try:
            signals = collect_live_signals()
            write_cache(write_target, signals)
            evidence.append(Evidence("runtime-live", "measured", "journald collected and cache refreshed", required=True))
        except Exception as exc:  # noqa: BLE001 — surface as unavailable evidence
            evidence.append(Evidence("runtime-live", "unavailable", str(exc), required=profile == "deep"))

    if signals is None:
        last_exc: Exception | None = None
        for candidate in read_candidates:
            try:
                signals, cache_evidence = load_cache(candidate)
                evidence.extend(cache_evidence)
                break
            except Exception as exc:  # noqa: BLE001 — try the next candidate
                last_exc = exc
        if signals is None:
            exc = last_exc if last_exc is not None else FileNotFoundError("no runtime cache candidate")
            evidence.append(Evidence("runtime-cache", "unavailable", str(exc), required=True))
            # Fail-closed placeholder metrics at 0 so composite does not invent health.
            metrics = [
                Metric(mid, title, weight, 0.0, intent, {"error": str(exc)})
                for mid, title, weight, intent in (
                    ("stability", "Stability", 18, "Crash/panic rate"),
                    ("error-rate", "Error rate", 16, "Operational errors per run"),
                    ("llm-serving", "LLM serving", 16, "Hard LLM faults"),
                    ("turn-reliability", "Turn reliability", 16, "Timeout fraction"),
                    ("tool-reliability", "Tool reliability", 14, "Tool error fraction"),
                    ("latency", "Latency", 20, "agentMs p95"),
                )
            ]
            return Domain(
                id="runtime",
                title="Runtime",
                weight=DOMAIN_WEIGHTS["runtime"],
                metrics=metrics,
                evidence=evidence,
                ratcheted=True,
            )

    metrics = score_signals(signals)
    return Domain(
        id="runtime",
        title="Runtime",
        weight=DOMAIN_WEIGHTS["runtime"],
        metrics=metrics,
        evidence=evidence,
        ratcheted=True,
    )
