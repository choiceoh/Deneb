"""Tool-quality miner — proactive L4 supply, RSI surface expansion (Lane A).

The recursion surface already INCLUDES tool descriptions: 41 of 45 tools carry
their natural-language description as a Go literal (`toolport.ToolDef.Description`
in toolreg), which is a `*.go` file = the declared gateway-source propose-only
surface. What was missing is a SIGNAL that grounds "improve this tool's
description/schema" candidates — the agentlog already measures it but nothing
consumed it for self-improvement:

  - a high per-tool ERROR rate, or
  - a high malformed-argument REPAIR rate (the model keeps emitting bad args),

both point at a description/schema that misleads the model about a tool. This
miner reads the server-computed per-tool stats and files the worst offenders as
propose-only, scope=code self-correction candidates so the coding lane can
tighten the offending tool's description.

Source: the existing `miniapp.observe.behavior` RPC returns
`agentlog.AggregateResult.Tools[]` — per-tool `calls/errors/repaired/unknown/
blocked`, already aggregated server-side. Zero gateway changes; the miner just
reads that JSON (same RPC-edge pattern as health_finding_miner / sop_miner, from
which the RPC + reopen + cap machinery is imported so the miners cannot drift).

Two candidate kinds, both scope=code:
  - `:desc` — a tool with a high error or malformed-arg repair rate (its
    description/schema misleads the model);
  - `:latency` — a tool slower than its per-tool ceiling OR regressed vs its
    baseline window (a performance defect in the tool's implementation).

Safety (mirrors the template lane):

  - `tool-quality` is GRADUATED into coding-dispatch.sh's allowlist (operator
    directive 2026-07-13): its candidates auto-dispatch to the coding lane and
    land through the same gate stack (make check + live-test + CI green). The
    tool-quality-dryrun workflow previews what it would file. The miner itself
    still only runs on demand / by workflow, so the operator controls the flow.
  - A `:desc` candidate proposes a DESCRIPTION/SCHEMA clarification only and a
    `:latency` candidate an implementation perf fix — never removing a tool or
    widening its permission surface.
  - Dedup/reopen mirrors genesis selfCorrectionReopenBlocked; the source id is
    stable per tool name so an applied fix must actually lower the rate before
    the same tool re-files.
  - A minimum call volume gates noise, and a per-run cap bounds queue growth.

stdlib-only and importable for deterministic tests; the CLI is
``scripts/audit/tool-quality-miner.py``.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
from typing import Any, TextIO

from health_finding_miner import (
    DEFAULT_GATEWAY_URL,
    GatewayError,
    ImpactMetricUnavailable,
    call_rpc,
    fetch_existing,
    pending_impact_observations_for,
    record_candidate,
    record_impact,
    select_candidates,
)

SOURCE_PREFIX = "tool-quality"

# A tool needs enough calls before its rates mean anything — below this the
# signal is noise (one bad call in three is not a description problem).
MIN_CALLS = 20

# Deterministic bars. A tool over EITHER is a description/schema-quality
# candidate: a high error rate or a high malformed-arg repair rate both say the
# model is being misled about how to call the tool.
ERROR_RATE = 0.15
REPAIR_RATE = 0.10

MAX_PER_RUN = 2
WINDOW_DAYS = 30
RECENT_DAYS = 7

# Impact contracts close against the RECENT (7d) window so a fix's observation
# is not diluted by pre-fix calls still inside the 30d filing window. The
# observation window matches: only after 7 post-watch days is the recent window
# fully post-fix.
IMPACT_WINDOW_MS = RECENT_DAYS * 24 * 60 * 60 * 1000

# Latency ("too slow") is a PERFORMANCE signal, judged two ways so an inherently
# heavy tool (web/asr) is not mistaken for a regression:
#   1. per-tool expected-absolute ceiling — "slower than we expect for THIS
#      tool" (fair across tool kinds; web is allowed to be slow, read is not);
#   2. regression vs its own baseline window — "got materially slower than it
#      used to be" (catches degradation regardless of the absolute).
# A tool trips the latency trigger on EITHER. Both knobs are tunable — the
# ceilings below are conservative starting guesses; calibrate from a dry-run.
LATENCY_REGRESSION_FACTOR = 1.5
DEFAULT_EXPECTED_MS = 3000
EXPECTED_MS = {
    # fast local operations
    "read": 800, "write": 800, "edit": 800, "grep": 1500, "glob": 1500,
    "sessions": 1500, "calendar": 2000, "wiki": 2500, "knowledge": 2500,
    "mail_archive": 2500,
    # inherently heavy — generous ceilings so normal use is not flagged
    "exec": 6000, "web": 12000, "asr": 20000, "paddleocr": 20000,
}

# The registration hub is the entry point for a reviewer/coding-lane to locate
# the offending ToolDef.Description; the evidence names the exact tool.
TOOLREG_HUB = "gateway-go/internal/pipeline/chat/toolreg/core.go"

_RISK_NOTE = (
    "제안 전용입니다. 관측된 비율을 낮추도록 그 도구의 설명(정말 잘못된 필드가 있을 "
    "때만 입력 스키마까지)을 다듬습니다 — 도구를 없애거나 권한 표면을 넓히면 안 됩니다. "
    "변경 후 observe.behavior 를 다시 돌려 비율이 내려간 것을 확인하고 반영합니다."
)


# --- candidate builders (pure) -------------------------------------------------


def _rate(n: int, calls: int) -> float:
    return (n / calls) if calls > 0 else 0.0


def tool_quality_candidates(tools: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Tools whose error or repair rate crosses the bar, worst-impact first.

    Uncapped: the shared reopen/dedup filter runs before the per-run cap so
    blocked tools do not consume dispatch slots. Impact rank = (error+repair
    rate) x calls, so a widely-used tool with a moderate rate outranks a rarely
    used one that spikes.
    """
    scored: list[tuple[float, dict[str, Any]]] = []
    for t in tools:
        name = str(t.get("name") or "").strip()
        calls = int(t.get("calls") or 0)
        if not name or calls < MIN_CALLS:
            continue
        errors = int(t.get("errors") or 0)
        repaired = int(t.get("repaired") or 0)
        unknown = int(t.get("unknown") or 0)
        blocked = int(t.get("blocked") or 0)
        err_rate = _rate(errors, calls)
        rep_rate = _rate(repaired, calls)
        if err_rate < ERROR_RATE and rep_rate < REPAIR_RATE:
            continue
        headline = []
        if err_rate >= ERROR_RATE:
            headline.append(f"error rate {err_rate:.0%}")
        if rep_rate >= REPAIR_RATE:
            headline.append(f"malformed-arg repair rate {rep_rate:.0%}")
        head = " + ".join(headline)
        impact = (err_rate + rep_rate) * calls
        scored.append((impact, {
            "scope": "code",
            "skillName": "tool-quality",
            "title": f"도구 설명·스키마 품질: {name} ({head})",
            "candidate": (
                f"'{name}' 도구가 {calls}회 호출에서 {head} 를 보입니다 — 모델이 반복해서 "
                f"잘못 쓰고 있으며, 이는 런타임 결함이라기보다 설명이나 입력 스키마가 "
                f"오해를 부른다는 신호입니다."
            ),
            "evidence": (
                f"observe.behavior {WINDOW_DAYS}d: {name} calls={calls} errors={errors} "
                f"({err_rate:.0%}) repaired={repaired} ({rep_rate:.0%}) unknown={unknown} "
                f"blocked={blocked}"
            ),
            "reason": "agentlog tool-quality signal — high error/repair rate suggests a "
                      "misleading tool description or schema (RSI surface expansion — "
                      "tool-description propose-only surface)",
            "targetFiles": [TOOLREG_HUB],
            "proposedChange": (
                f"'{name}' 의 ToolDef.Description(과 입력 스키마)을 찾아 — {TOOLREG_HUB} 에서 "
                f"시작하거나 internal/pipeline/chat 전반에서 도구 이름을 검색 — 관측된 "
                f"{head} 를 줄이도록 다듬는다: 필수 인자·값 형식·언제 쓰는지를 분명히 해 "
                f"모델이 오용을 멈추게 한다."
            ),
            "risk": _RISK_NOTE,
            # :desc suffix so a description candidate and a :latency candidate for
            # the same tool never prefix-collide under startswith dedup matching.
            "source": f"{SOURCE_PREFIX}:{name}:desc",
            # Binary usefulness oracle (same shape as health.finding_present):
            # after the observation window, does the tool still trip the desc
            # filing predicate on a FRESH recent window? Closed by this miner
            # (tool_quality_impact_resolver) on later runs.
            "impactContract": {
                "metric": f"tool.quality.finding_present:{name}:desc",
                "direction": "decrease",
                "baseline": 1,
                "target": 0,
                "minSamples": 1,
                # Recovery only counts on a tool still being called: MIN_CALLS
                # already keeps a silent tool pending rather than minting a
                # false verified from silence.
                "guardrails": ["recovered_while_still_called"],
                "observationWindowMs": IMPACT_WINDOW_MS,
            },
        }))
    scored.sort(key=lambda s: (-s[0], str(s[1]["source"])))
    return [c for _, c in scored]


_PERF_RISK_NOTE = (
    "제안 전용입니다. 지연의 원인(경계 없는 작업·캐시 부재·병렬로 안전한데 직렬)을 "
    "구현에서 찾습니다 — 권한 표면을 넓히거나 결과를 약화시키면 안 됩니다. 변경 후 "
    "observe.behavior 를 다시 돌려 지연이 회복된 것을 확인하고 반영합니다."
)


def latency_candidates(recent: list[dict[str, Any]],
                       baseline: dict[str, dict[str, Any]]) -> list[dict[str, Any]]:
    """Tools that are too slow — over their per-tool ceiling OR regressed vs
    their own baseline window — as propose-only perf candidates, worst-impact
    first. `baseline` is the prior-window ToolStat indexed by name.
    """
    scored: list[tuple[float, dict[str, Any]]] = []
    for t in recent:
        name = str(t.get("name") or "").strip()
        calls = int(t.get("calls") or 0)
        if not name or calls < MIN_CALLS:
            continue
        avg = int(t.get("avgMs") or 0)
        if avg <= 0:
            continue
        ceiling = EXPECTED_MS.get(name, DEFAULT_EXPECTED_MS)
        base_avg = int((baseline.get(name) or {}).get("avgMs") or 0)
        over_ceiling = avg > ceiling
        regressed = base_avg > 0 and avg > LATENCY_REGRESSION_FACTOR * base_avg
        if not (over_ceiling or regressed):
            continue
        why = []
        if over_ceiling:
            why.append(f"{avg}ms avg > {ceiling}ms expected")
        if regressed:
            why.append(f"regressed {base_avg}ms→{avg}ms (×{avg / base_avg:.1f})")
        head = "; ".join(why)
        # Impact = how far over the ceiling, weighted by call volume.
        impact = max(avg - ceiling, avg - base_avg, 1) * calls
        scored.append((impact, {
            "scope": "code",
            "skillName": "tool-quality",
            "title": f"도구 지연: {name} 느림 ({head})",
            "candidate": (
                f"'{name}' 도구가 {calls}회 호출에서 너무 느립니다 — {head}. 기대치를 이만큼 "
                f"넘는 지연(또는 분명한 회귀)은 설명 문제가 아니라 구현의 성능 결함입니다."
            ),
            "evidence": (
                f"observe.behavior {RECENT_DAYS}d vs {WINDOW_DAYS}d baseline: {name} "
                f"calls={calls} avgMs={avg} ceiling={ceiling} baselineAvgMs={base_avg}"
            ),
            "reason": "agentlog tool-latency signal — tool slower than its per-tool ceiling "
                      "or regressed vs its baseline (RSI surface expansion — tool perf)",
            "targetFiles": [],  # impl file varies per tool; the proposedChange points the way
            "proposedChange": (
                f"'{name}' 도구 구현을 찾아(internal/pipeline/chat/tools 에서 도구 이름 검색) "
                f"지연을 줄인다: 작업에 경계를 두거나, 캐시를 넣거나 고치거나, 안전한 단계를 "
                f"병렬화한다. 변경 후 observe.behavior 로 avgMs 가 {ceiling}ms 아래로 "
                f"회복되는지 확인한다."
            ),
            "risk": _PERF_RISK_NOTE,
            "source": f"{SOURCE_PREFIX}:{name}:latency",
            "impactContract": {
                "metric": f"tool.quality.finding_present:{name}:latency",
                "direction": "decrease",
                "baseline": 1,
                "target": 0,
                "minSamples": 1,
                # Same falsifier as the desc oracle: a latency finding that
                # "recovered" because the tool stopped being called is silence,
                # not a fix.
                "guardrails": ["recovered_while_still_called"],
                "observationWindowMs": IMPACT_WINDOW_MS,
            },
        }))
    scored.sort(key=lambda s: (-s[0], str(s[1]["source"])))
    return [c for _, c in scored]


# --- impact closure (this miner's own contracts) --------------------------------


def _trips_desc(tool: dict[str, Any]) -> bool:
    calls = int(tool.get("calls") or 0)
    return (
        _rate(int(tool.get("errors") or 0), calls) >= ERROR_RATE
        or _rate(int(tool.get("repaired") or 0), calls) >= REPAIR_RATE
    )


def _trips_latency(tool: dict[str, Any], baseline: dict[str, dict[str, Any]]) -> bool:
    name = str(tool.get("name") or "")
    avg = int(tool.get("avgMs") or 0)
    if avg <= 0:
        return False
    base_avg = int((baseline.get(name) or {}).get("avgMs") or 0)
    return avg > EXPECTED_MS.get(name, DEFAULT_EXPECTED_MS) or (
        base_avg > 0 and avg > LATENCY_REGRESSION_FACTOR * base_avg
    )


def tool_quality_impact_resolver(recent: list[dict[str, Any]],
                                 baseline: dict[str, dict[str, Any]]):
    """Resolver for pending tool-quality contracts against fresh behavior stats.

    The oracle recomputes the exact FILING predicate on the recent window (the
    contract's observation window guarantees it is post-fix). A tool below
    MIN_CALLS cannot be judged either way — insufficient evidence keeps the
    verdict pending rather than minting a false verified from silence.
    """
    prefix = "tool.quality.finding_present:"
    recent_index = {str(t.get("name") or ""): t for t in recent}

    def resolve(metric: str):
        if not metric.startswith(prefix):
            return None
        rest = metric.removeprefix(prefix)
        name, _, kind = rest.rpartition(":")
        if not name or kind not in ("desc", "latency"):
            raise ImpactMetricUnavailable(f"malformed tool-quality metric: {metric}")
        tool = recent_index.get(name)
        calls = int((tool or {}).get("calls") or 0)
        if tool is None or calls < MIN_CALLS:
            raise ImpactMetricUnavailable(
                f"insufficient recent calls for {name} ({calls} < {MIN_CALLS})"
            )
        present = _trips_desc(tool) if kind == "desc" else _trips_latency(tool, baseline)
        state = "still trips" if present else "recovered"
        return float(present), calls, (
            f"fresh observe.behavior ({RECENT_DAYS}d): {name}:{kind} {state} "
            f"(calls={calls})"
        )

    return resolve


# --- behavior source (thin RPC edge) -------------------------------------------


def fetch_behavior(base_url: str, token: str, days: int) -> dict[str, Any]:
    return call_rpc(base_url, "miniapp.observe.behavior", {"days": days}, token)


# --- CLI -----------------------------------------------------------------------


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--behavior-report",
                        help="pre-fetched observe.behavior JSON for the baseline window "
                             "(skips the live RPC)")
    parser.add_argument("--recent-report",
                        help="pre-fetched observe.behavior JSON for the recent window "
                             "(latency regression numerator; skips the live RPC)")
    parser.add_argument("--url", default=os.environ.get("DENEB_GATEWAY_URL", DEFAULT_GATEWAY_URL),
                        help="gateway base URL (env DENEB_GATEWAY_URL)")
    parser.add_argument("--token", default=os.environ.get("DENEB_CLIENT_TOKEN", ""),
                        help="client token (reads ~/.deneb/client_token if unset)")
    parser.add_argument("--days", type=int, default=WINDOW_DAYS,
                        help="baseline behavior window in days (error/repair rates + latency baseline)")
    parser.add_argument("--recent-days", type=int, default=RECENT_DAYS,
                        help="recent behavior window in days (latency regression numerator)")
    parser.add_argument("--max", type=int, default=MAX_PER_RUN, help="per-run cap on candidates")
    parser.add_argument("--dry-run", action="store_true",
                        help="build and print the filing plan; record nothing")
    parser.add_argument("--json", action="store_true", help="machine-readable summary")
    return parser


def main(argv: list[str] | None = None, stdout: TextIO | None = None,
         stderr: TextIO | None = None) -> int:
    args = _parser().parse_args(argv)
    out = stdout or sys.stdout
    err = stderr or sys.stderr

    token = args.token
    if not token:
        token_file = os.path.expanduser("~/.deneb/client_token")
        if os.path.exists(token_file):
            with open(token_file, encoding="utf-8") as handle:
                token = handle.read().strip()

    base_url = args.url.rstrip("/")

    def load(report_path: str | None, days: int) -> list[dict[str, Any]]:
        if report_path:
            with open(report_path, encoding="utf-8") as handle:
                data = json.load(handle)
        else:
            data = fetch_behavior(base_url, token, days)
        tools = data.get("tools") if isinstance(data, dict) else None
        return tools if isinstance(tools, list) else []

    try:
        baseline = load(args.behavior_report, args.days)
    except (OSError, ValueError, GatewayError) as exc:
        print(f"behavior source unavailable: {exc}", file=err)
        return 1
    # Recent window drives the latency-regression numerator. If it is
    # unavailable, latency falls back to the per-tool ceiling on the baseline
    # window alone (still catches persistent slowness), never crashes.
    try:
        recent = load(args.recent_report, args.recent_days)
    except (OSError, ValueError, GatewayError) as exc:
        print(f"recent window unavailable — latency uses ceiling only: {exc}", file=err)
        recent = baseline

    baseline_index = {str(t.get("name") or ""): t for t in baseline}

    now_ms = int(time.time() * 1000)
    try:
        existing = fetch_existing(base_url, token)
    except GatewayError as exc:
        if not args.dry_run:
            print(f"cannot read the candidate queue — refusing to file blind: {exc}", file=err)
            return 1
        print(f"gateway unreachable — DRY-RUN continues WITHOUT dedup: {exc}", file=err)
        existing = []

    impact_observations, impact_skipped = pending_impact_observations_for(
        existing, tool_quality_impact_resolver(recent, baseline_index), now_ms
    )
    impact_evaluated: list[dict[str, str]] = []
    impact_errors: list[str] = []
    for observation in impact_observations:
        if args.dry_run:
            print(
                f"DRY-RUN would evaluate impact: {observation['id']} "
                f"observed={observation['observed']}",
                file=out,
            )
            continue
        try:
            status = record_impact(base_url, token, observation)
            impact_evaluated.append({"id": observation["id"], "status": status})
            print(f"impact {status}  {observation['id']}", file=out)
        except GatewayError as exc:
            impact_errors.append(f"{observation['id']}: {exc}")
            print(f"impact rejected  {observation['id']}: {exc}", file=err)
    for cid, reason in impact_skipped:
        print(f"impact skip {cid}: {reason}", file=out)

    candidates = tool_quality_candidates(baseline) + latency_candidates(recent, baseline_index)
    to_file, skipped = select_candidates(candidates, existing, now_ms, max(args.max, 0))

    filed: list[dict[str, str]] = []
    errors: list[str] = []
    for cand in to_file:
        if args.dry_run:
            print(f"DRY-RUN would file: {cand['source']}", file=out)
            print(json.dumps(cand, ensure_ascii=False, indent=2), file=out)
            continue
        try:
            cid = record_candidate(base_url, token, cand)
            filed.append({"id": cid, "source": cand["source"]})
            print(f"filed {cid}  {cand['source']}", file=out)
        except GatewayError as exc:
            # A record-time rejection (e.g. forbidden surface) is a healthy
            # refusal — report it and keep filing the rest.
            errors.append(f"{cand['source']}: {exc}")
            print(f"record rejected  {cand['source']}: {exc}", file=err)
    for cand, reason in skipped:
        print(f"skip {cand['source']}: {reason}", file=out)

    summary = {
        "tools": len(baseline),
        "planned": len(to_file),
        "filed": len(filed),
        "skipped": len(skipped),
        "rejected": len(errors),
        "dry_run": bool(args.dry_run),
        "candidates": filed,
        "impactPlanned": len(impact_observations),
        "impactEvaluated": len(impact_evaluated),
        "impactPending": len(impact_skipped),
        "impactRejected": len(impact_errors),
        "impacts": impact_evaluated,
    }
    if args.json:
        print(json.dumps(summary, ensure_ascii=False), file=out)
    else:
        print(
            f"tool-quality-miner: tools={summary['tools']} planned={summary['planned']} "
            f"filed={summary['filed']} skipped={summary['skipped']} rejected={summary['rejected']} "
            f"impact-evaluated={summary['impactEvaluated']} "
            f"impact-pending={summary['impactPending']}"
            + (" (dry-run)" if args.dry_run else ""),
            file=out,
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
