"""Proactive L4 supply miner — RSI roadmap P5 workstream 3 (importable module).

Every live code-candidate source is REACTIVE (runtime errors, tool gaps,
harness rejections): a healthy gateway produces nothing and the L4 coding lane
idles exactly when a self-improving system should be renovating. This miner is
the first PROACTIVE source: it reads the two deterministic health benches and
files their standing defects as propose-only, scope=code self-correction
candidates through the existing review lane.

Candidate inputs (both deterministic, both already scored):

  - ``codebase-health-v2.py --format json`` findings with severity high or
    critical (e.g. the volatile-contract blast on ``domain/wiki``).
  - ``runtime-health.py --json`` dimensions scoring below a standing-weakness
    bar (e.g. latency, the #1 weakness at proposal time).

The post-deploy evaluator also understands Health Bench and RSI Bench overall,
domain, and metric score contracts. RSI Bench runs only when a pending contract
uses its namespace, so ordinary mining keeps its existing cost.

Design decision (2026-07-12, recorded per the P5-ws3 brief): this lane is a
SCRIPTS-SIDE miner filing over the miniapp RPC, NOT a gateway PeriodicTask,
unlike its template ``genesis/runtime_error_mining.go`` (#3491). Three reasons:

  1. Process boundary. The template mines the in-process observe ring; this
     lane's inputs live OUTSIDE the serving process (a git checkout + history
     for the structural bench, journald for the runtime bench). Shelling
     repo-analysis out of the gateway would couple the runtime to the dev
     toolchain for no gain.
  2. Single-writer queue invariant. Writes go through the gateway tracker via
     ``miniapp.self_improvement_coding.record`` (record-time forbidden-surface
     enforcement); this script never appends to the JSONL ledger itself.
     Dedup reads use ``.list`` so the candidate/review merge logic remains in
     the gateway instead of being copied into audit scripts.
  3. Cadence. Structural findings move at repo cadence, not runtime cadence —
     the miner belongs with the other audit scripts and their scheduling
     (manual or timer), not inside the serving runtime.

Safety (mirrors the template lane):

  - Propose-only at record time (surface tier). The ``health-finding`` source
    namespace graduated into coding-dispatch.sh's allowlist (2026-07-12); runtime
    weaknesses still land with empty targetFiles until localized.
  - Dedup/reopen mirrors genesis ``selfCorrectionReopenBlocked``: one open
    candidate per finding; rejected/superseded twins never re-file (operator
    veto respected); an APPLIED twin re-files only after a cooldown while the
    finding still shows in the bench ("the fix did not stick"); past
    ``REOPEN_CAP`` twins the signature is permanently blocked.
  - Per-run caps bound queue growth; every candidate carries the bench finding
    ID plus the evidence string so review stays deterministic.
  - Every filed health candidate carries a deterministic impact contract. On a
    later run, this same miner closes pending post-deploy observations from the
    fresh bench instead of relying on a human to declare that the fix worked.

stdlib-only and importable for deterministic tests; the CLI is
``scripts/audit/health-finding-miner.py``.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request
from typing import Any, TextIO

SOURCE_PREFIX = "health-finding"

# Structural bench: only high-severity findings are worth a coding dispatch.
STRUCTURAL_SEVERITIES = ("critical", "high")
MAX_STRUCTURAL_PER_RUN = 3

# Runtime bench: a dimension below this score is a standing weakness (the
# graded band bottoms out around here; healthy dims sit 75+). One candidate per
# run — runtime weaknesses are broad and a queue full of them helps nobody.
RUNTIME_WEAK_SCORE = 60.0
MAX_RUNTIME_PER_RUN = 1
RUNTIME_IMPACT_WINDOW_MS = 24 * 60 * 60 * 1000

# Mirrors genesis selfCorrectionReopenCooldown (2 × the 7d evolution-health
# window): an applied fix gets this long to prove itself before the same
# finding may re-file.
REOPEN_COOLDOWN_MS = 14 * 24 * 60 * 60 * 1000

# Mirrors genesis selfCorrectionReopenCap: after this many twins for the same
# source signature, auto-reopen stops permanently (operator must break the cycle).
REOPEN_CAP = 5

# Mirrors handlerminiapp lifecycleScanLimit — the deepest .list view available.
LIST_LIMIT = 500

DEFAULT_GATEWAY_URL = "http://127.0.0.1:18789"

_RISK_NOTE = (
    "Deterministic bench mining: confirm the finding still reproduces at HEAD before "
    "editing. Structural work must keep behavior identical and pass the full lane gates. "
    "If the remediation would touch acceptance machinery or security CODEOWNERS paths, "
    "land nothing and record why."
)

# The responsibility/fan-out family scores a large public surface and/or a
# multi-hundred-commit git-history window. No single bounded coding session can
# make such a finding DISAPPEAR (the history component only heals as the
# window slides; the surface takes multiple steps), so the default
# "confirm this finding disappears" verify contract made every dispatched
# agent decline — live 2026-07-14/15, all 4 declined dispatches were exactly
# these kinds, burning 2–18 min of coding-lane quota each with zero landings.
# They get an honest bounded-step contract instead, and NO finding-present
# impact contract (a landed bounded step would otherwise be mislabeled
# "no effect" when the finding, correctly, is still present).
# Keep in lockstep with genesis rsiIncrementalHealthKinds — Go withholds
# these from auto-dispatch (검토 대기). Changing the set here without the
# Go twin re-opens the unattended coding cap.
INCREMENTAL_KINDS = frozenset({
    "responsibility-cochange",
    "volatile-contract-responsibility",
    "diffuse-change-responsibility",
    "fanout-hotspot",
})

INCREMENTAL_VERIFY = (
    "Contract: this finding scores a large surface and/or a git-history window "
    "and CANNOT disappear in one session — do not aim for that. Land ONE bounded "
    "structural step instead: move the top 1-2 consumers behind a narrow port, "
    "or shrink the exported surface for one capability. If no such step is "
    "cleanly separable, land nothing and record why. Done = the bounded step "
    "compiles, passes the lane gates, and plausibly reduces the finding's "
    "structural driver (exports / fan-in / fan-out); the score itself moves "
    "over multiple steps and window slides."
)


class GatewayError(RuntimeError):
    """The gateway RPC failed — the miner must fail loud, not file silently."""


class ImpactMetricUnavailable(ValueError):
    """A known metric namespace is missing the requested fresh observation."""


# --- candidate builders (pure) -------------------------------------------------

# Mirror of genesis/surfaces acceptance-machinery path patterns that health
# miners commonly hit as directory targets. Gateway still rejects at record
# time; pre-filtering here avoids burning the per-run cap on known refusals
# (e.g. volatile-hub @ .../skills/genesis).
_FORBIDDEN_PATH_MARKERS = (
    "gateway-go/internal/domain/skills/genesis",
    "/skills/genesis/",
    "validation_engine.go",
    "eprocess/eprocess.go",
    "surfaces/surfaces.go",
    "scripts/dev/coding-dispatch.sh",
    "scripts/dev/dispatch_prompt.py",
    "scripts/dev/dispatch_outcome.py",
    "scripts/dev/pr.sh",
    ".github/workflows/ci.yml",
)


def forbidden_target_reason(target_files: list[str] | None) -> str | None:
    """Return a skip reason when any target is under a forbidden acceptor surface."""
    for raw in target_files or []:
        norm = str(raw).strip().replace("\\", "/").lower().lstrip("./").rstrip("/")
        if not norm:
            continue
        for marker in _FORBIDDEN_PATH_MARKERS:
            m = marker.lower()
            if (
                norm == m
                or norm.endswith("/" + m)
                or norm.endswith(m)
                or m.startswith(norm + "/")
                or ("/" + norm + "/") in ("/" + m + "/")
                or m in norm
                or norm in m
            ):
                return f"forbidden surface prefilter: {raw}"
    return None


def structural_candidates(report: dict[str, Any]) -> list[dict[str, Any]]:
    """High-severity structural findings, ranked by priority (desc) then id.

    Uncapped: the reopen/dedup filter runs before the per-run cap so blocked
    findings do not consume dispatch slots. Forbidden acceptor paths are
    dropped here so they never spend the cap (gateway would reject them).
    """
    revision = str(report.get("revision") or "?")[:12]
    profile = report.get("profile") or "?"
    out: list[dict[str, Any]] = []
    findings = [
        f for f in report.get("findings") or []
        if f.get("severity") in STRUCTURAL_SEVERITIES and f.get("id") and f.get("path")
    ]
    if report.get("schema_version") == 3:
        findings = [f for f in findings if f.get("domain", "structure") == "structure"]
    findings.sort(key=lambda f: (-float(f.get("priority") or 0.0), str(f["id"])))
    for f in findings:
        path = str(f["path"])
        if forbidden_target_reason([path]):
            continue
        fid = str(f["id"])
        kind = fid.split(":", 1)[0]
        if kind == "structure" and ":" in fid.split(":", 1)[1]:
            # v3 domain-prefixed ids ("structure:<rule>:<hash>") — the rule is
            # the second segment.
            kind = fid.split(":", 2)[1]
        proposed = str(f.get("remediation") or "").strip()
        verify = str(f.get("verify") or "").strip()
        incremental = kind in INCREMENTAL_KINDS
        if incremental:
            # These findings score a large surface and/or a multi-hundred-commit
            # history window; NO single bounded session can make the finding
            # disappear, and a "confirm it disappears" contract makes the
            # dispatched agent correctly decline every time (live 2026-07-14/15:
            # all 4 declined dispatches were exactly this family — 2 to 18
            # minutes of coding-lane quota each, zero landings). Replace the
            # verify ask with an honest bounded step.
            proposed = f"{proposed} {INCREMENTAL_VERIFY}".strip()
        elif verify:
            proposed = f"{proposed} Verify: {verify}".strip()
        candidate = {
            "scope": "code",
            "skillName": "codebase-health",
            "title": f"structural finding: {kind} @ {f['path']}",
            "candidate": str(f.get("why") or "").strip(),
            "evidence": (
                f"{fid} [{f.get('pillar')}/{f.get('severity')} "
                f"priority={float(f.get('priority') or 0.0):g}] "
                f"{str(f.get('evidence') or '').strip()} "
                f"(bench revision {revision}, profile {profile})"
            ),
            "reason": "codebase-health high-severity structural finding — "
                      "proactive L4 supply (RSI P5 ws3)",
            "targetFiles": [path],
            "proposedChange": proposed,
            "risk": _RISK_NOTE,
            "source": f"{SOURCE_PREFIX}:{fid}",
        }
        if not incremental:
            # finding-present → 0 is only an honest impact contract for
            # findings a single fix can extinguish; a bounded step on the
            # incremental family leaves the finding correctly present and
            # would be mislabeled "no effect".
            candidate["impactContract"] = {
                "metric": f"health.finding_present:{fid}",
                "direction": "decrease",
                "baseline": 1,
                "target": 0,
                "minSamples": 1,
                # Absence must come from a fresh bench run that still reports
                # other findings — a bench that produced nothing at all cannot
                # tell a fixed finding from an audit that did not run.
                "guardrails": ["absent_in_a_bench_that_still_reports_findings"],
            }
        out.append(candidate)
    return out


def runtime_candidates(runtime: dict[str, Any] | None) -> list[dict[str, Any]]:
    """Standing runtime weaknesses: dims under the bar, weakest first, capped.

    The runtime report is a rolling 7d window, so the synthetic finding id is
    the dimension name (``runtime-latency``) — stable across runs, which is
    what makes the reopen semantics meaningful.

    Returns EVERY weak dimension, weakest first, and leaves the per-run cap to
    ``select_candidates`` — which skips reopen-blocked rows without spending the
    cap. Truncating to the cap here instead starved the whole lane: the two
    weakest dims (latency, llm-serving) went into the 14d reopen cooldown, the
    slice kept re-picking a blocked dim, and the runtime lane filed NOTHING for
    the rest of the cooldown while ``tool-reliability`` (30.9) had never been
    filed once (2026-07-30 실측). The cap still holds — this only changes WHICH
    dim gets the slot, so a queue full of runtime findings stays impossible.
    """
    if not isinstance(runtime, dict):
        return []
    dims = runtime.get("dims") or {}
    meta = runtime.get("meta") or {}
    detail = runtime.get("detail") or {}
    extra = runtime.get("extra") or {}
    weak = sorted(
        ((name, float(score)) for name, score in dims.items()
         if isinstance(score, (int, float)) and float(score) < RUNTIME_WEAK_SCORE),
        key=lambda kv: (kv[1], kv[0]),
    )
    out: list[dict[str, Any]] = []
    for name, score in weak:
        detail_bits = "; ".join(str(d) for d in (detail.get(name) or [])[:3])
        extra_bits = " ".join(f"{k}={v}" for k, v in sorted((extra.get(name) or {}).items()))
        out.append({
            "scope": "code",
            "skillName": "runtime-health",
            "title": f"runtime standing weakness: {name} {score:.1f}/100",
            "candidate": (
                f"runtime-health dimension '{name}' is a standing weakness: {score:.1f}/100 "
                f"over the last {meta.get('days', '?')}d window "
                f"(composite {runtime.get('composite', '?')})."
            ),
            "evidence": (
                f"runtime-{name} [runtime-health/weak-dimension score={score:.1f}] "
                f"{detail_bits} | {extra_bits} | {meta.get('runs', '?')} runs"
            ),
            "reason": "runtime-health standing weakness — proactive L4 supply (RSI P5 ws3)",
            "targetFiles": [],
            "proposedChange": (
                f"Identify the dominant contributor to the '{name}' dimension in the gateway "
                f"and land a targeted improvement, then re-run scripts/audit/runtime-health.py "
                f"and confirm the {name} score recovers. Do not relabel or suppress the "
                f"signal — runtime-health's honest fault accounting is the contract."
            ),
            "risk": _RISK_NOTE,
            "source": f"{SOURCE_PREFIX}:runtime-{name}",
            "impactContract": {
                "metric": f"runtime.health.score:{name}",
                "direction": "increase",
                "baseline": score,
                "target": RUNTIME_WEAK_SCORE,
                "minSamples": 1,
                "observationWindowMs": RUNTIME_IMPACT_WINDOW_MS,
            },
        })
    return out


# --- dedup / reopen (mirrors genesis selfCorrectionReopenBlocked) ---------------


def reopen_blocked(existing: list[dict[str, Any]], source: str, now_ms: int) -> str | None:
    """Why a fresh filing for ``source`` is suppressed, or None to allow.

    Twin match is separator-aware (mirrors genesis
    selfCorrectionSourceMatches): exact source, or the same source extended
    past a ":". A bare prefix cross-blocked source ids that are prefixes of
    one another, e.g. "…latency" vs "…latency-p99" (RSI code eval M7/F4).
    A live twin (proposed/accepted) blocks; a REJECTED twin is the operator's
    standing veto and blocks for good. APPLIED and SUPERSEDED twins re-open
    only after the cooldown — the bench still reporting the finding now IS
    the "recurred again" signal. Superseded is a review-lane ruling, not a
    veto: runtime-error-rate was superseded two minutes after filing on
    2026-07-23 and the runtime lane then stayed dead for a month. Past
    REOPEN_CAP twins the signature is permanently blocked (genesis
    selfCorrectionReopenCap parity).
    """
    source = source.strip()
    if not source:
        return None
    newest: dict[str, Any] | None = None
    source_twins = 0
    for c in existing:
        src = str(c.get("source") or "")
        if not (src == source or src.startswith(source + ":")):
            continue
        source_twins += 1
        if newest is None or (c.get("createdAt") or 0) > (newest.get("createdAt") or 0):
            newest = c
    if newest is None:
        return None
    if source_twins > REOPEN_CAP:
        return f"reopen cap exceeded ({source_twins} twins > {REOPEN_CAP})"
    status = str(newest.get("status") or "proposed").lower()
    if status not in ("applied", "superseded"):
        return f"{status} twin {newest.get('id')}"
    settled_at = max(int(newest.get("createdAt") or 0), int(newest.get("updatedAt") or 0))
    if now_ms - settled_at < REOPEN_COOLDOWN_MS:
        return f"{status} twin {newest.get('id')} inside reopen cooldown"
    return None


def classify_skips(skipped: list[tuple[dict[str, Any], str]]) -> dict[str, int]:
    """Bucket skip reasons for the status drop: permanent (operator veto / reopen
    cap), cooldown (applied/superseded twin still cooling), capped (per-run
    cap), other (live twin). planned=0 with a non-empty skip list is a BLOCKED
    lane, not a quiet one — the status file must say which."""
    buckets = {"permanent": 0, "cooldown": 0, "capped": 0, "other": 0}
    for _, reason in skipped:
        if reason.startswith("rejected twin") or reason.startswith("reopen cap exceeded"):
            buckets["permanent"] += 1
        elif "inside reopen cooldown" in reason:
            buckets["cooldown"] += 1
        elif reason == "per-run cap reached":
            buckets["capped"] += 1
        else:
            buckets["other"] += 1
    return buckets


def select_candidates(
    candidates: list[dict[str, Any]],
    existing: list[dict[str, Any]],
    now_ms: int,
    cap: int,
) -> tuple[list[dict[str, Any]], list[tuple[dict[str, Any], str]]]:
    """Apply reopen blocking then the per-run cap; blocked rows don't spend it."""
    selected: list[dict[str, Any]] = []
    skipped: list[tuple[dict[str, Any], str]] = []
    for cand in candidates:
        if len(selected) >= cap:
            skipped.append((cand, "per-run cap reached"))
            continue
        reason = reopen_blocked(existing, cand["source"], now_ms)
        if reason:
            skipped.append((cand, reason))
            continue
        selected.append(cand)
    return selected, skipped


# --- gateway RPC edge -----------------------------------------------------------


def call_rpc(
    base_url: str,
    method: str,
    params: dict[str, Any],
    token: str,
    timeout: float = 30.0,
) -> dict[str, Any]:
    """Call a miniapp RPC and return the payload; raise GatewayError on failure.

    Unlike the fail-open audit readers, a producer must fail loud: filing
    nothing because the gateway was down has to be visible to the caller.
    """
    headers = {"Content-Type": "application/json"}
    if token:
        headers["X-Deneb-Client-Token"] = token
    body = json.dumps(
        {"type": "req", "id": "health-finding-miner", "method": method, "params": params}
    ).encode()
    req = urllib.request.Request(
        f"{base_url}/api/v1/miniapp/rpc", data=body, headers=headers, method="POST"
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            data = json.loads(resp.read())
    except (urllib.error.URLError, urllib.error.HTTPError, ValueError, OSError) as exc:
        raise GatewayError(f"{method} failed against {base_url}: {exc}") from exc
    if not isinstance(data, dict) or data.get("ok") is False or data.get("error"):
        raise GatewayError(f"{method} rejected: {data.get('error') or data}")
    payload = data.get("payload")
    if not isinstance(payload, dict):
        raise GatewayError(f"{method} returned no payload: {data}")
    return payload


def fetch_existing(base_url: str, token: str) -> list[dict[str, Any]]:
    payload = call_rpc(
        base_url,
        "miniapp.self_improvement_coding.list",
        {"status": "all", "limit": LIST_LIMIT},
        token,
    )
    candidates = payload.get("candidates")
    return candidates if isinstance(candidates, list) else []


def record_candidate(base_url: str, token: str, cand: dict[str, Any]) -> str:
    payload = call_rpc(base_url, "miniapp.self_improvement_coding.record", cand, token)
    recorded = payload.get("candidate") or {}
    return str(recorded.get("id") or "?")


def _numeric(value: Any) -> float | None:
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return float(value)
    return None


def _report_overall_score(report: dict[str, Any], label: str) -> float:
    score = report.get("score")
    value = _numeric(score.get("overall")) if isinstance(score, dict) else _numeric(score)
    if value is None:
        value = _numeric(report.get("overall"))
    if value is None:
        raise ImpactMetricUnavailable(f"{label} overall score unavailable")
    return value


def _report_domain_score(report: dict[str, Any], domain_id: str, label: str) -> float:
    domain_id = domain_id.strip()
    score = report.get("score")
    if isinstance(score, dict) and isinstance(score.get("domains"), dict):
        value = _numeric(score["domains"].get(domain_id))
        if value is not None:
            return value
    for domain in report.get("domains") or []:
        if str(domain.get("id") or "") == domain_id:
            value = _numeric(domain.get("score"))
            if value is not None:
                return value
    raise ImpactMetricUnavailable(f"{label} domain unavailable: {domain_id or '?'}")


def _report_metric_score(report: dict[str, Any], selector: str, label: str) -> float:
    selector = selector.strip()
    domain_filter, separator, metric_id = selector.partition("/")
    if not separator:
        metric_id, domain_filter = domain_filter, ""
    matches: list[float] = []
    for domain in report.get("domains") or []:
        if domain_filter and str(domain.get("id") or "") != domain_filter:
            continue
        for metric in domain.get("metrics") or []:
            if str(metric.get("id") or "") == metric_id:
                value = _numeric(metric.get("score"))
                if value is not None:
                    matches.append(value)
    if len(matches) == 1:
        return matches[0]
    if len(matches) > 1:
        raise ImpactMetricUnavailable(
            f"{label} metric is ambiguous; use domain/metric: {metric_id}"
        )
    raise ImpactMetricUnavailable(f"{label} metric unavailable: {selector or '?'}")


SCORE_METRIC_NAMESPACES = (
    ("health.score:", "health", "overall"),
    ("health.domain.score:", "health", "domain"),
    ("health.metric.score:", "health", "metric"),
    ("rsi.bench.score:", "rsi", "overall"),
    ("rsi.bench.domain.score:", "rsi", "domain"),
    ("rsi.bench.metric.score:", "rsi", "metric"),
)


def resolve_impact_metric(
    metric: str,
    structural_report: dict[str, Any],
    runtime_report: dict[str, Any] | None,
    rsi_report: dict[str, Any] | None,
) -> tuple[float, int, str] | None:
    """Resolve an owned metric, return None for another evaluator's namespace."""
    finding_prefix = "health.finding_present:"
    if metric.startswith(finding_prefix):
        finding_id = metric.removeprefix(finding_prefix)
        present = any(
            str(finding.get("id") or "") == finding_id
            for finding in structural_report.get("findings") or []
        )
        state = "still present" if present else "absent"
        return float(present), 1, f"fresh health bench: finding {finding_id} {state}"

    runtime_prefix = "runtime.health.score:"
    if metric.startswith(runtime_prefix):
        dimension = metric.removeprefix(runtime_prefix)
        runtime = runtime_report or {}
        value = _numeric((runtime.get("dims") or {}).get(dimension))
        if value is None:
            raise ImpactMetricUnavailable(
                f"runtime dimension unavailable: {dimension or '?'}"
            )
        samples = (runtime.get("meta") or {}).get("runs")
        if not isinstance(samples, (int, float)) or isinstance(samples, bool) or samples < 1:
            samples = 1
        return value, int(samples), f"fresh runtime health bench: {dimension}={value:g}"

    for prefix, report_name, score_kind in SCORE_METRIC_NAMESPACES:
        if not metric.startswith(prefix):
            continue
        report = structural_report if report_name == "health" else rsi_report
        label = "health" if report_name == "health" else "RSI Bench"
        if not isinstance(report, dict):
            raise ImpactMetricUnavailable(f"{label} report unavailable")
        selector = metric.removeprefix(prefix)
        if score_kind == "overall":
            if selector != "overall":
                raise ImpactMetricUnavailable(f"{label} score unavailable: {selector or '?'}")
            value = _report_overall_score(report, label)
        elif score_kind == "domain":
            value = _report_domain_score(report, selector, label)
        else:
            value = _report_metric_score(report, selector, label)
        bench = "health bench" if report_name == "health" else "RSI Bench"
        return value, 1, f"fresh {bench}: {score_kind} {selector}={value:g}"
    return None


def pending_impact_observations(
    existing: list[dict[str, Any]],
    structural_report: dict[str, Any],
    runtime_report: dict[str, Any] | None,
    now_ms: int,
    rsi_report: dict[str, Any] | None = None,
) -> tuple[list[dict[str, Any]], list[tuple[str, str]]]:
    """Build post-watch observations from fresh deterministic bench output.

    Only contracts owned by this miner are interpreted. Unknown metrics remain
    pending for their own evaluator rather than being guessed here.
    """
    return pending_impact_observations_for(
        existing,
        lambda metric: resolve_impact_metric(
            metric, structural_report, runtime_report, rsi_report
        ),
        now_ms,
    )


def pending_impact_observations_for(
    existing: list[dict[str, Any]],
    resolver,
    now_ms: int,
) -> tuple[list[dict[str, Any]], list[tuple[str, str]]]:
    """Shared attempt-gating/window walk over pending contracts (miner-agnostic).

    ``resolver(metric)`` returns ``(observed, samples, note)``, ``None`` when
    the metric belongs to another evaluator, or raises ImpactMetricUnavailable
    when its evidence is missing right now. Sibling miners (deadcode-finding,
    tool-quality) import THIS function so the lifecycle semantics — pending
    only, dispatch-attempt required, observation window honored — cannot drift
    from the health miner's (the same principle as the shared RPC edge).
    """
    observations: list[dict[str, Any]] = []
    skipped: list[tuple[str, str]] = []
    for candidate in existing:
        result = candidate.get("impactResult") or {}
        if result.get("status") != "pending":
            continue
        cid = str(candidate.get("id") or "?")
        attempt_id = str(candidate.get("attemptId") or "").strip()
        contract = candidate.get("impactContract") or {}
        metric = str(contract.get("metric") or "")
        if not attempt_id:
            skipped.append((cid, "pending candidate has no dispatch attempt"))
            continue
        try:
            ready_at = int(candidate.get("updatedAt") or 0) + int(
                contract.get("observationWindowMs") or 0
            )
        except (TypeError, ValueError):
            skipped.append((cid, "invalid observation window"))
            continue
        if now_ms < ready_at:
            skipped.append((cid, f"observation window pending until {ready_at}"))
            continue

        try:
            resolved = resolver(metric)
        except ImpactMetricUnavailable as exc:
            skipped.append((cid, str(exc)))
            continue
        if resolved is None:
            skipped.append((cid, f"metric owned by another evaluator: {metric or '?'}"))
            continue

        observed, samples, note = resolved

        observations.append({
            "id": cid,
            "attemptId": attempt_id,
            "observed": observed,
            "samples": samples,
            "note": note,
        })
    return observations, skipped


def record_impact(base_url: str, token: str, observation: dict[str, Any]) -> str:
    payload = call_rpc(
        base_url, "miniapp.self_improvement_coding.impact", observation, token
    )
    return str(payload.get("impactStatus") or "?")


# --- bench runners (thin subprocess edges) ---------------------------------------


def parse_leading_json(text: str) -> dict[str, Any]:
    """First JSON object in ``text`` — runtime-health appends metric lines."""
    obj, _ = json.JSONDecoder().raw_decode(text)
    if not isinstance(obj, dict):
        raise ValueError("expected a JSON object")
    return obj


def repo_root() -> str:
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


def run_structural_bench(root: str, stderr: TextIO) -> tuple[dict[str, Any], str, str]:
    """Prefer Health Bench 3.0; fall back to v2 when v3 cannot run.

    Returns (report, bench_source, fallback_reason). The source travels into the
    miner status file (write_miner_status) because the v3→v2 fallback ran
    SILENTLY for nine days (2026-07-18 → 07-27, stale runtime cache) — stderr
    of a 05:20 systemd unit is where degradation notices go to die. rsi-status
    L4 renders the source, so a fallback streak is visible on the operator's
    standard diagnostic surface, not just in journald.
    """
    v3 = os.path.join(root, "scripts", "audit", "health-bench-v3.py")
    fallback_reason = ""
    if os.path.isfile(v3):
        print("running health-bench-v3 (fast profile)…", file=stderr)
        proc = subprocess.run(
            [sys.executable, v3, "--format", "json"],
            capture_output=True, text=True, cwd=root, check=False, timeout=900,
        )
        if proc.returncode == 0:
            return parse_leading_json(proc.stdout), "health-bench-v3", ""
        fallback_reason = (proc.stderr[-200:] or f"rc={proc.returncode}").strip()
        print(
            f"health-bench-v3 unavailable ({fallback_reason}); "
            "falling back to codebase-health-v2",
            file=stderr,
        )
    else:
        fallback_reason = "health-bench-v3.py missing"
    script = os.path.join(root, "scripts", "audit", "codebase-health-v2.py")
    print("running codebase-health-v2 (fast profile)…", file=stderr)
    proc = subprocess.run(
        [sys.executable, script, "--format", "json"],
        capture_output=True, text=True, cwd=root, check=False, timeout=600,
    )
    if proc.returncode != 0:
        raise GatewayError(f"codebase-health-v2 failed (rc={proc.returncode}): {proc.stderr[-400:]}")
    return parse_leading_json(proc.stdout), "codebase-health-v2", fallback_reason


def miner_status_path() -> str:
    """FIXED under ~/.deneb/data (graduation-state convention; DENEB_STATE_DIR
    does not move it) so the Go rsi-status reader and this writer agree."""
    return os.path.expanduser("~/.deneb/data/health_finding_miner_status.json")


def write_miner_status(payload: dict[str, Any], stderr: TextIO, path: str | None = None) -> None:
    """Best-effort status drop for rsi-status L4 — never fails the mining run."""
    target = path or miner_status_path()
    try:
        os.makedirs(os.path.dirname(target), exist_ok=True)
        tmp = target + ".tmp"
        with open(tmp, "w", encoding="utf-8") as handle:
            json.dump(payload, handle, ensure_ascii=False, indent=2)
        os.replace(tmp, target)
    except OSError as exc:
        print(f"WARN: could not persist miner status ({exc})", file=stderr)


def runtime_report_from_v3(report: dict[str, Any]) -> dict[str, Any] | None:
    """Project a v3 report's runtime domain into the legacy runtime-health shape."""
    if report.get("schema_version") != 3:
        return None
    runtime = next((d for d in report.get("domains") or [] if d.get("id") == "runtime"), None)
    if not isinstance(runtime, dict):
        return None
    dims = {
        str(m["id"]): float(m["score"])
        for m in runtime.get("metrics") or []
        if m.get("id") is not None and isinstance(m.get("score"), (int, float))
    }
    if not dims:
        return None
    return {
        "composite": float(runtime.get("score") or 0.0),
        "dims": dims,
        "meta": {"days": "cache/live", "source": "health-bench-v3"},
        "detail": {},
        "extra": {},
    }


def run_runtime_bench(root: str, stderr: TextIO) -> dict[str, Any] | None:
    """Runtime bench, or None when unavailable (journald is host-specific)."""
    script = os.path.join(root, "scripts", "audit", "runtime-health.py")
    print("running runtime-health…", file=stderr)
    try:
        proc = subprocess.run(
            [sys.executable, script, "--json"],
            capture_output=True, text=True, cwd=root, check=False, timeout=300,
        )
        if proc.returncode != 0:
            raise ValueError(proc.stderr[-200:] or f"rc={proc.returncode}")
        return parse_leading_json(proc.stdout)
    except (OSError, ValueError, subprocess.SubprocessError) as exc:
        print(f"runtime-health unavailable — skipping runtime source: {exc}", file=stderr)
        return None


def run_rsi_bench(root: str, stderr: TextIO) -> dict[str, Any] | None:
    """RSI Bench snapshot, only called for a pending rsi.bench.* contract."""
    script = os.path.join(root, "scripts", "audit", "rsi-bench.py")
    print("running RSI Bench…", file=stderr)
    try:
        proc = subprocess.run(
            [sys.executable, script, "--format", "json"],
            capture_output=True, text=True, cwd=root, check=False, timeout=600,
        )
        if proc.returncode != 0:
            raise ValueError(proc.stderr[-200:] or f"rc={proc.returncode}")
        return parse_leading_json(proc.stdout)
    except (OSError, ValueError, subprocess.SubprocessError) as exc:
        print(f"RSI Bench unavailable — impact stays pending: {exc}", file=stderr)
        return None


def needs_rsi_bench(existing: list[dict[str, Any]]) -> bool:
    return any(
        (candidate.get("impactResult") or {}).get("status") == "pending"
        and str((candidate.get("impactContract") or {}).get("metric") or "").startswith(
            "rsi.bench."
        )
        for candidate in existing
    )


# --- CLI -------------------------------------------------------------------------


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--report", help="pre-generated codebase-health-v2 JSON report path")
    parser.add_argument("--runtime-report", help="pre-generated runtime-health --json output path")
    parser.add_argument("--rsi-report", help="pre-generated rsi-bench --format json report path")
    parser.add_argument("--url", default=os.environ.get("DENEB_GATEWAY_URL", DEFAULT_GATEWAY_URL),
                        help="gateway base URL (env DENEB_GATEWAY_URL)")
    parser.add_argument("--token", default=os.environ.get("DENEB_CLIENT_TOKEN", ""),
                        help="client token (reads ~/.deneb/client_token if unset)")
    parser.add_argument("--max-structural", type=int, default=MAX_STRUCTURAL_PER_RUN,
                        help="per-run cap on structural candidates")
    parser.add_argument("--dry-run", action="store_true",
                        help="build and print the filing plan; record nothing")
    parser.add_argument("--json", action="store_true", help="machine-readable summary")
    return parser


def _load_json_file(path: str) -> dict[str, Any]:
    with open(path, encoding="utf-8") as handle:
        return parse_leading_json(handle.read())


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

    root = repo_root()
    bench_source, fallback_reason = "pre-generated", ""
    try:
        if args.report:
            report = _load_json_file(args.report)
        else:
            report, bench_source, fallback_reason = run_structural_bench(root, err)
    except (OSError, ValueError, GatewayError) as exc:
        print(f"structural bench unavailable: {exc}", file=err)
        if not args.dry_run:
            write_miner_status({
                "lastRunAtMs": int(time.time() * 1000),
                "structuralSource": "unavailable",
                "fallbackReason": str(exc)[-200:],
                "planned": 0,
                "filed": 0,
            }, err)
        return 1
    if args.runtime_report:
        try:
            runtime: dict[str, Any] | None = _load_json_file(args.runtime_report)
        except (OSError, ValueError) as exc:
            print(f"runtime report unreadable — skipping runtime source: {exc}", file=err)
            runtime = None
    else:
        runtime = runtime_report_from_v3(report)
        if runtime is None:
            runtime = run_runtime_bench(root, err)

    base_url = args.url.rstrip("/")
    now_ms = int(time.time() * 1000)
    try:
        existing = fetch_existing(base_url, token)
    except GatewayError as exc:
        if not args.dry_run:
            print(f"cannot read the candidate queue — refusing to file blind: {exc}", file=err)
            return 1
        print(f"gateway unreachable — DRY-RUN continues WITHOUT dedup: {exc}", file=err)
        existing = []

    rsi_report: dict[str, Any] | None = None
    if args.rsi_report:
        try:
            rsi_report = _load_json_file(args.rsi_report)
        except (OSError, ValueError) as exc:
            print(f"RSI report unreadable — impact stays pending: {exc}", file=err)
    elif needs_rsi_bench(existing):
        rsi_report = run_rsi_bench(root, err)

    structural_sel, structural_skip = select_candidates(
        structural_candidates(report), existing, now_ms, max(args.max_structural, 0))
    runtime_sel, runtime_skip = select_candidates(
        runtime_candidates(runtime), existing, now_ms, MAX_RUNTIME_PER_RUN)
    to_file = structural_sel + runtime_sel
    skipped = structural_skip + runtime_skip

    impact_observations, impact_skipped = pending_impact_observations(
        existing, report, runtime, now_ms, rsi_report
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
        "structuralSource": bench_source,
        "fallbackReason": fallback_reason,
    }
    if not args.dry_run:
        buckets = classify_skips(skipped)
        write_miner_status({
            "lastRunAtMs": now_ms,
            "structuralSource": bench_source,
            "fallbackReason": fallback_reason,
            "planned": summary["planned"],
            "filed": summary["filed"],
            "skipped": summary["skipped"],
            "blockedPermanent": buckets["permanent"],
            "blockedCooldown": buckets["cooldown"],
            "capped": buckets["capped"],
        }, err)
    if args.json:
        print(json.dumps(summary, ensure_ascii=False), file=out)
    else:
        print(
            f"health-finding-miner: planned={summary['planned']} filed={summary['filed']} "
            f"skipped={summary['skipped']} rejected={summary['rejected']} "
            f"impact-evaluated={summary['impactEvaluated']} "
            f"impact-pending={summary['impactPending']} "
            f"impact-rejected={summary['impactRejected']}"
            + (" (dry-run)" if args.dry_run else ""),
            file=out,
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
