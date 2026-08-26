#!/usr/bin/env python3
"""rsi_loop_audit — independent RSI loop health auditor (importable module).

Reads the gateway's /health and the miniapp.rsi.status RPC to verify the
recursive self-improvement loop is honestly turning. Designed to be run by an
operator or cron — never by the loop itself (the loop must not grade its own
auditor). CLI wrapper: scripts/audit/rsi-loop-audit.py.

The audit grades six invariants drawn from the RSI roadmap:

  1. L1.liveness   — is the fast loop (skill evolution) actually producing?
  2. accept.honesty— falseAcceptRate within a safe band (<0.15), HONESTLY:
                     a rate with zero resolved evolves is UNMEASURED, not 0.
  3. evolve.confirm— confirmRate above the stall threshold (>0.30), same
                     unmeasured semantics.
  4. P2.slowloop   — is the weekly meta-evolution layer turning?
  5. P3.labels     — is verifier co-evolution collecting/consuming labels?
  6. L4.dispatch   — does the source self-edit lane have dispatchable supply?

Schema notes (verified against a live gateway, 2026-07-12):
  - /health exposes the genesis section under "self_evolution" (alias
    "propus"), with snake_case fields: evolves_7d, genesis_7d,
    resolved_evolves_7d, confirm_rate, false_accept_rate, meta_revisions_7d…
    The rate fields ship since the acceptor-scoreboard export; on an older
    gateway they are simply absent and graded WARN, never FAIL.
  - The miniapp RPC envelope is {"type","id","ok","payload"} — the useful body
    is under "payload" (NOT "result").
  - miniapp.rsi.status payload: {"layers":[{key,title,state,diagnosis,
    metrics:[{label,value}]}], "turning": n} with states LIVE / DATA-GATED /
    STARVED / FROZEN / IDLE.

Grading philosophy: FAIL means "the loop is broken or a self-brake fired —
operator attention now"; WARN means "not measurable yet or waiting on data —
honest, expected states included"; PASS means "measured and healthy".
Exit code: 0 all PASS, 1 any WARN, 2 any FAIL.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request
from typing import Any

# L1 dormancy horizon: with zero evolves/genesis in the 7d window, recent
# lifecycle activity (reviews, proposals) within this many hours still counts
# as "alive but dormant" (WARN) instead of dead (FAIL).
CORPUS_REAL_SHARE_FLOOR = 10.0

DORMANT_HORIZON_HOURS = 48

# Below this many resolved evolves, a rate is reported but never FAILs the
# audit — a 1/1 rollback must not read as falseAcceptRate=1.0 certainty
# (the scoreboard ships n alongside the rate for exactly this reason).
MIN_RESOLVED_FOR_HARD_VERDICT = 3


def fetch_json(url: str, timeout: float = 5.0) -> dict[str, Any] | None:
    """Fetch JSON from a URL. Returns None on any error (fail-open)."""
    try:
        req = urllib.request.Request(url, headers={"Accept": "application/json"})
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read())
    except (urllib.error.URLError, urllib.error.HTTPError, ValueError, OSError):
        return None


def unwrap_rpc_envelope(data: Any) -> dict[str, Any] | None:
    """Extract the useful body from a miniapp RPC response envelope.

    The live envelope is {"type","id","ok","payload"}; "result" is accepted as
    a legacy fallback. An explicit ok=false means the RPC failed — None.
    """
    if not isinstance(data, dict):
        return None
    if data.get("ok") is False:
        return None
    payload = data.get("payload")
    if isinstance(payload, dict):
        return payload
    result = data.get("result")
    if isinstance(result, dict):
        return result
    # Bare body (no envelope) — accept only if it looks like a body.
    if "payload" not in data and "result" not in data:
        return data
    return None


def call_rpc(base_url: str, method: str, token: str | None = None, timeout: float = 5.0) -> dict[str, Any] | None:
    """Call a miniapp RPC and return the unwrapped payload, or None."""
    headers = {"Content-Type": "application/json"}
    if token:
        headers["X-Deneb-Client-Token"] = token
    payload = json.dumps({"type": "req", "id": "rsi-audit", "method": method, "params": {}}).encode()
    try:
        req = urllib.request.Request(f"{base_url}/api/v1/miniapp/rpc", data=payload, headers=headers, method="POST")
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return unwrap_rpc_envelope(json.loads(resp.read()))
    except (urllib.error.URLError, urllib.error.HTTPError, ValueError, OSError):
        return None


def genesis_section(health: dict[str, Any] | None) -> dict[str, Any] | None:
    """The genesis health section, or None when the gateway doesn't ship one."""
    if not isinstance(health, dict):
        return None
    section = health.get("self_evolution") or health.get("propus")
    return section if isinstance(section, dict) else None


def layer_map(rsi_status: dict[str, Any] | None) -> dict[str, dict[str, Any]]:
    """rsi.status layers keyed by their layer key (L1..L4)."""
    if not isinstance(rsi_status, dict):
        return {}
    out: dict[str, dict[str, Any]] = {}
    for layer in rsi_status.get("layers") or []:
        if isinstance(layer, dict) and isinstance(layer.get("key"), str):
            out[layer["key"]] = layer
    return out


def layer_metrics(layer: dict[str, Any]) -> str:
    """Render a layer's metrics list as 'label=value; …' for the detail line."""
    parts = []
    for m in layer.get("metrics") or []:
        if isinstance(m, dict) and m.get("label") is not None:
            parts.append(f"{m.get('label')}={m.get('value')}")
    return "; ".join(parts)


# ── Audit checks ───────────────────────────────────────────────────────────

class Result:
    OK = "PASS"
    SOFT = "WARN"
    HARD = "FAIL"

    def __init__(self, name: str, status: str, diagnosis: str, detail: str = ""):
        self.name = name
        self.status = status
        self.diagnosis = diagnosis
        self.detail = detail

    def __str__(self) -> str:
        icon = {"PASS": "✅", "WARN": "⚠️", "FAIL": "❌"}[self.status]
        line = f"  {icon} {self.status:4s} {self.name:16s} {self.diagnosis}"
        if self.detail:
            line += f"\n         {self.detail}"
        return line


def check_liveness(health: dict | None, now_ms: int | None = None) -> Result:
    """L1: is the fast loop producing evolves or new skills?"""
    if health is None:
        return Result("L1.liveness", Result.HARD, "gateway unreachable — cannot audit")
    evo = genesis_section(health)
    if evo is None:
        return Result("L1.liveness", Result.SOFT, "no genesis section in /health — genesis off or pre-P1 gateway")
    evolves = evo.get("evolves_7d", 0) or 0
    genesis = evo.get("genesis_7d", 0) or 0
    if evolves > 0 or genesis > 0:
        return Result("L1.liveness", Result.OK,
                      f"{evolves} evolves + {genesis} new skills (7d) — fast loop is active")
    last_ms = evo.get("last_activity_ms", 0) or 0
    now = now_ms if now_ms is not None else int(time.time() * 1000)
    if last_ms > 0 and (now - last_ms) < DORMANT_HORIZON_HOURS * 3600 * 1000:
        return Result("L1.liveness", Result.SOFT,
                      f"0 commits (7d) but lifecycle activity {evo.get('last_activity_age', '?')} ago — dormant, not dead")
    return Result("L1.liveness", Result.HARD, "0 evolves, 0 genesis, no recent activity — loop is not running",
                  "check: genesis EvolveUnderperformers 6h task, autonomous sweep")


def _rate_check(name: str, evo: dict | None, field: str, n_field: str = "resolved_evolves_7d") -> tuple[Result | None, float, int]:
    """Shared unmeasured/small-n guards for the rate checks.

    Returns (early_result, rate, n): early_result is non-None when the rate is
    not hard-gradeable (missing/unmeasured/small sample).
    """
    if evo is None:
        return Result(name, Result.SOFT, "no genesis section in /health"), 0.0, 0
    if field not in evo:
        return Result(name, Result.SOFT, f"{field} not exported — gateway predates the acceptor scoreboard",
                      "additive /health field; redeploy the gateway to measure"), 0.0, 0
    rate = float(evo.get(field) or 0.0)
    n = int(evo.get(n_field) or 0)
    if n == 0:
        return Result(name, Result.SOFT, "unmeasured — 0 evolves resolved in the window",
                      "post-evolve watches must resolve (confirm/rollback) before the rate means anything"), rate, n
    if n < MIN_RESOLVED_FOR_HARD_VERDICT:
        return Result(name, Result.SOFT, f"{field}={rate:.2f} on n={n} — sample too small for a verdict"), rate, n
    return None, rate, n


def check_honesty(health: dict | None) -> Result:
    """Acceptor honesty: false-accept rate within the safe band."""
    if health is None:
        return Result("accept.honesty", Result.HARD, "gateway unreachable")
    early, far, n = _rate_check("accept.honesty", genesis_section(health), "false_accept_rate")
    if early is not None:
        return early
    if far > 0.20:
        return Result("accept.honesty", Result.HARD, f"falseAcceptRate={far:.2f} n={n} (>0.20 — acceptor too lenient)",
                      "roadmap P1.5: flip gates + held-out isolation should keep this < 0.15")
    if far > 0.15:
        return Result("accept.honesty", Result.SOFT, f"falseAcceptRate={far:.2f} n={n} (elevated but tolerable)")
    return Result("accept.honesty", Result.OK, f"falseAcceptRate={far:.2f} n={n} (≤0.15 — healthy)")


def check_confirm(health: dict | None) -> Result:
    """Confirm rate above the stall threshold."""
    if health is None:
        return Result("evolve.confirm", Result.HARD, "gateway unreachable")
    early, cr, n = _rate_check("evolve.confirm", genesis_section(health), "confirm_rate")
    if early is not None:
        return early
    if cr < 0.20:
        return Result("evolve.confirm", Result.HARD, f"confirmRate={cr:.2f} n={n} (<0.20 — evolves are not sticking)",
                      "accepted evolves keep rolling back — judge or gate may be too loose")
    if cr < 0.30:
        return Result("evolve.confirm", Result.SOFT, f"confirmRate={cr:.2f} n={n} (low but not critical)")
    return Result("evolve.confirm", Result.OK, f"confirmRate={cr:.2f} n={n} (≥0.30 — healthy)")


def _layer_check(name: str, layer: dict[str, Any] | None, *,
                 pass_states: frozenset[str], warn_states: frozenset[str],
                 missing_diagnosis: str) -> Result:
    """Grade one rsi.status layer by its honest state classification.

    FROZEN/IDLE always FAIL: an engaged self-brake or a dead lane is exactly
    what an independent auditor exists to keep from going unnoticed.
    """
    if layer is None:
        return Result(name, Result.SOFT, missing_diagnosis)
    state = str(layer.get("state", "UNKNOWN"))
    diagnosis = str(layer.get("diagnosis", "")).strip()
    detail = layer_metrics(layer)
    if state in pass_states:
        return Result(name, Result.OK, f"{layer.get('key')}={state} — {diagnosis}" if diagnosis else f"{layer.get('key')}={state}", detail)
    if state in warn_states:
        return Result(name, Result.SOFT, f"{layer.get('key')}={state} — {diagnosis}" if diagnosis else f"{layer.get('key')}={state}", detail)
    if state in ("FROZEN", "IDLE"):
        return Result(name, Result.HARD, f"{layer.get('key')}={state} — {diagnosis}" if diagnosis else f"{layer.get('key')}={state} — lane is not turning",
                      detail or "FROZEN = self-brake engaged (review drift audit); IDLE = lane never ran")
    return Result(name, Result.SOFT, f"{layer.get('key')}={state} — unknown state", detail)


def check_slowloop(rsi_status: dict | None, health: dict | None) -> Result:
    """P2: the weekly meta-evolution layer."""
    layers = layer_map(rsi_status)
    if not layers:
        # Fall back to the /health meta fields so an old-RPC gateway still audits.
        evo = genesis_section(health)
        if evo is None:
            return Result("P2.slowloop", Result.SOFT, "rsi.status RPC unavailable and no genesis health section")
        if evo.get("auto_adopt_frozen"):
            return Result("P2.slowloop", Result.HARD, "auto-adopt FROZEN (self-brake) — review the drift audit")
        revisions = evo.get("meta_revisions_7d", 0) or 0
        proposed = evo.get("meta_proposed_7d", 0) or 0
        if revisions > 0 or proposed > 0:
            return Result("P2.slowloop", Result.OK,
                          f"{revisions} meta cycles, {proposed} proposals (7d) — slow loop active (via /health)")
        return Result("P2.slowloop", Result.SOFT, "no meta cycle in 7d (weekly cadence — may simply be mid-window)")
    return _layer_check("P2.slowloop", layers.get("L2"),
                        pass_states=frozenset({"LIVE", "DATA-GATED"}),
                        warn_states=frozenset({"STARVED"}),
                        missing_diagnosis="L2 layer not in rsi.status response")


def check_labels(rsi_status: dict | None) -> Result:
    """P3: verifier co-evolution label flow."""
    layers = layer_map(rsi_status)
    return _layer_check("P3.labels", layers.get("L3"),
                        pass_states=frozenset({"LIVE"}),
                        warn_states=frozenset({"DATA-GATED", "STARVED"}),
                        missing_diagnosis="L3 layer not in rsi.status — RPC too old or P3 unwired")


def check_dispatch(rsi_status: dict | None) -> Result:
    """L4: source self-edit dispatch supply."""
    layers = layer_map(rsi_status)
    return _layer_check("L4.dispatch", layers.get("L4"),
                        pass_states=frozenset({"LIVE"}),
                        warn_states=frozenset({"DATA-GATED", "STARVED"}),
                        missing_diagnosis="L4 layer not in rsi.status — RPC too old or L4 unwired")


def check_corpus(state_dir: str | None = None) -> Result:
    """Why acceptance stalls: can the held-out corpus tell candidates apart?

    The other checks report THAT evolves stop resolving; none says why. Traced
    by hand on 2026-08-26: 1,695 validation cases held only 10 from real
    failures (0.6%) — the rest are adversarial mutations, backfill and
    curriculum. A synthetic pool cannot separate a rewrite from its original, so
    every candidate ties and the strict-improvement margin can never be met
    (30d: 118 proposals, 28 rejections, 0 accepted; the top rejection reason was
    "did not improve (5.9 vs 5.9)"). That is gate saturation, not candidate
    quality, and it is invisible from the accept-rate alone.
    """
    root = state_dir or os.path.join(os.path.expanduser("~"), ".deneb")
    path = os.path.join(root, "data", "skill_validation_cases.jsonl")
    try:
        with open(path, encoding="utf-8") as fh:
            rows = [json.loads(line) for line in fh if line.strip()]
    except (OSError, json.JSONDecodeError):
        return Result("corpus.discrim", Result.SOFT, "검증 케이스 원장을 읽을 수 없음 — 코퍼스 판별력 미측정")
    if not rows:
        return Result("corpus.discrim", Result.SOFT, "검증 케이스 0건 — held-out 게이트가 잴 것이 없다")
    real = sum(1 for r in rows if str(r.get("source", "")).startswith(("auto-failed-skill-use", "auto-successful-skill-use")))
    share = real * 100.0 / len(rows)
    detail = (f"전체 {len(rows)}건 · 실사용 유래 {real}건 ({share:.1f}%) · "
              f"나머지는 합성(adversarial/backfill/curriculum)")
    if share >= CORPUS_REAL_SHARE_FLOOR:
        return Result("corpus.discrim", Result.OK,
                      f"실사용 유래 케이스 {share:.1f}% — 후보를 가를 근거가 있다", detail)
    return Result("corpus.discrim", Result.SOFT,
                  f"코퍼스 {share:.1f}%만 실사용 유래 — 후보가 원본과 동점날 수밖에 없다 (게이트 포화)",
                  detail + " · 실패 케이스를 늘려야 수용이 가능해진다 (exercised=no 캡처, 실패 트레이스 채굴)")


def run_checks(health: dict | None, rsi_status: dict | None, now_ms: int | None = None,
               state_dir: str | None = None) -> list[Result]:
    """All seven audit checks, in report order."""
    return [
        check_liveness(health, now_ms),
        check_honesty(health),
        check_confirm(health),
        check_corpus(state_dir),
        check_slowloop(rsi_status, health),
        check_labels(rsi_status),
        check_dispatch(rsi_status),
    ]


def overall_status(results: list[Result]) -> tuple[str, int]:
    """Worst status across checks and its process exit code."""
    worst = Result.OK
    for r in results:
        if r.status == Result.HARD:
            return Result.HARD, 2
        if r.status == Result.SOFT:
            worst = Result.SOFT
    return worst, {Result.OK: 0, Result.SOFT: 1}[worst]


# ── Main ────────────────────────────────────────────────────────────────────

def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description="Independent RSI loop health auditor")
    ap.add_argument("--url", default=os.environ.get("DENEB_GATEWAY_URL", "http://127.0.0.1:18789"),
                    help="gateway base URL (default: http://127.0.0.1:18789)")
    ap.add_argument("--token", default=os.environ.get("DENEB_CLIENT_TOKEN", ""),
                    help="client token for RPC (reads ~/.deneb/client_token if unset)")
    ap.add_argument("--json", action="store_true", help="machine-readable JSON output")
    args = ap.parse_args(argv)

    token = args.token
    if not token:
        token_file = os.path.expanduser("~/.deneb/client_token")
        if os.path.isfile(token_file):
            with open(token_file) as f:
                token = f.read().strip()

    base_url = args.url.rstrip("/")
    health = fetch_json(f"{base_url}/health")
    rsi_status = call_rpc(base_url, "miniapp.rsi.status", token)

    if health is None and rsi_status is None:
        msg = f"❌ Gateway unreachable at {base_url} — is the gateway running?"
        if args.json:
            print(json.dumps({"error": msg, "exit": 2}))
        else:
            print(msg)
        return 2

    results = run_checks(health, rsi_status)
    worst, exit_code = overall_status(results)

    if args.json:
        print(json.dumps({
            "gateway": base_url,
            "reachable": health is not None,
            "rsi_rpc": rsi_status is not None,
            "checks": [{"name": r.name, "status": r.status, "diagnosis": r.diagnosis, "detail": r.detail} for r in results],
            "overall": worst,
            "exit": exit_code,
        }, ensure_ascii=False, indent=2))
        return exit_code

    print(f"RSI Loop Audit — {base_url}")
    print(f"{'─' * 60}")
    evo = genesis_section(health)
    if evo is not None:
        print("  게이트웨이: 연결됨")
        print(f"  evolve(7d): {evo.get('evolves_7d', '?')}  genesis: {evo.get('genesis_7d', '?')}  "
              f"rollback: {evo.get('evolve_rolled_back_7d', '?')}  resolved: {evo.get('resolved_evolves_7d', '?')}")
    elif health is not None:
        print("  게이트웨이: 연결됨 (genesis 섹션 없음)")
    else:
        print("  게이트웨이: /health 응답 없음 (RPC만 시도)")
    print(f"  rsi.status: {'가능' if rsi_status else '불가'}")
    print()
    for r in results:
        print(r)
    print(f"{'─' * 60}")
    summary = {"PASS": sum(1 for r in results if r.status == Result.OK),
               "WARN": sum(1 for r in results if r.status == Result.SOFT),
               "FAIL": sum(1 for r in results if r.status == Result.HARD)}
    print(f"종합: {worst}  ({summary['PASS']} PASS, {summary['WARN']} WARN, {summary['FAIL']} FAIL)")
    return exit_code


if __name__ == "__main__":
    sys.exit(main())
