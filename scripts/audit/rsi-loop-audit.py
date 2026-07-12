#!/usr/bin/env python3
"""rsi-loop-audit.py — independent RSI loop health auditor.

Reads the gateway's /health and miniapp.rsi.status RPC to verify the recursive
self-improvement loop is honestly turning. Designed to be run by an operator
or cron — never by the loop itself (the loop must not grade its own auditor).

The audit checks five invariants drawn from the RSI roadmap:

  1. LIVENESS: is the fast loop (L1 skill evolution) actually evolving skills?
  2. HONESTY:  is falseAcceptRate within a safe band (< 0.15)?
  3. CONFIRM:  is confirmRate above the stall threshold (> 0.30)?
  4. SLOWLOOP: is the P2 meta-evolution task producing proposals?
  5. LABELS:   are P3 verifier co-evolution labels accumulating?

Each check is graded PASS / WARN / FAIL with a one-line diagnosis. The overall
exit code is 0 (all PASS), 1 (any WARN), or 2 (any FAIL).

Usage:
  rsi-loop-audit.py                    # audit http://127.0.0.1:18789
  rsi-loop-audit.py --url http://...   # custom gateway
  rsi-loop-audit.py --json             # machine-readable output

Requires the gateway to be running (reads live state, not static analysis).
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from typing import Any


def fetch_json(url: str, timeout: float = 5.0) -> dict[str, Any] | None:
    """Fetch JSON from a URL. Returns None on any error (fail-open)."""
    try:
        req = urllib.request.Request(url, headers={"Accept": "application/json"})
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read())
    except (urllib.error.URLError, urllib.error.HTTPError, ValueError, OSError):
        return None


def call_rpc(base_url: str, method: str, token: str | None = None, timeout: float = 5.0) -> dict[str, Any] | None:
    """Call a miniapp RPC. Returns the result dict or None."""
    headers = {"Content-Type": "application/json"}
    if token:
        headers["X-Deneb-Client-Token"] = token
    payload = json.dumps({"type": "req", "id": "rsi-audit", "method": method, "params": {}}).encode()
    try:
        req = urllib.request.Request(f"{base_url}/api/v1/miniapp/rpc", data=payload, headers=headers, method="POST")
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            data = json.loads(resp.read())
            return data.get("result") or data
    except (urllib.error.URLError, urllib.error.HTTPError, ValueError, OSError):
        return None


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
        line = f"  {icon} {self.status:4s} {self.name:20s} {self.diagnosis}"
        if self.detail:
            line += f"\n         {self.detail}"
        return line


def check_liveness(health: dict | None) -> Result:
    """L1: is the fast loop evolving skills?"""
    if health is None:
        return Result("L1.liveness", Result.HARD, "gateway unreachable — cannot audit")
    evo = health.get("evolution") or health.get("evolutionHealth") or {}
    total = evo.get("evolveCount7d", evo.get("evolveCount", 0))
    if total == 0 and evo.get("staleCount", 0) == 0:
        return Result("L1.liveness", Result.HARD, "0 evolves — loop is not running",
                      "check: genesis EvolveUnderperformers 6h task, autonomous sweep")
    if total > 0:
        return Result("L1.liveness", Result.OK, f"{total} evolves (7d) — loop is active")
    return Result("L1.liveness", Result.SOFT, "no recent evolves but skills exist — may be dormant")


def check_honesty(health: dict | None) -> Result:
    """False-accept rate within safe band."""
    if health is None:
        return Result("accept.honesty", Result.HARD, "gateway unreachable")
    evo = health.get("evolution") or health.get("evolutionHealth") or {}
    far = evo.get("falseAcceptRate", -1)
    if far < 0:
        return Result("accept.honesty", Result.SOFT, "falseAcceptRate not reported — pre-P1.5 data?",
                      "the field exists since #3433; missing it means the gateway is old or genesis is off")
    if far > 0.20:
        return Result("accept.honesty", Result.HARD, f"falseAcceptRate={far:.2f} (>0.20 — acceptor is too lenient)",
                      "roadmap P1.5: flip gates + held-out isolation should keep this < 0.15")
    if far > 0.15:
        return Result("accept.honesty", Result.SOFT, f"falseAcceptRate={far:.2f} (elevated but tolerable)")
    return Result("accept.honesty", Result.OK, f"falseAcceptRate={far:.2f} (≤0.15 — healthy)")


def check_confirm(health: dict | None) -> Result:
    """Confirm rate above stall threshold."""
    if health is None:
        return Result("evolve.confirm", Result.HARD, "gateway unreachable")
    evo = health.get("evolution") or health.get("evolutionHealth") or {}
    cr = evo.get("confirmRate", -1)
    if cr < 0:
        return Result("evolve.confirm", Result.SOFT, "confirmRate not reported")
    if cr < 0.20:
        return Result("evolve.confirm", Result.HARD, f"confirmRate={cr:.2f} (<0.20 — evolves are not sticking)",
                      "low confirm means accepted evolves get rolled back — judge or gate may be too loose")
    if cr < 0.30:
        return Result("evolve.confirm", Result.SOFT, f"confirmRate={cr:.2f} (low but not critical)")
    return Result("evolve.confirm", Result.OK, f"confirmRate={cr:.2f} (≥0.30 — healthy)")


def check_slowloop(rsi_status: dict | None) -> Result:
    """P2: meta-evolution producing proposals?"""
    if rsi_status is None:
        return Result("P2.slowloop", Result.SOFT, "rsi.status RPC unavailable — gateway too old or not wired")
    layers = {l["key"]: l for l in rsi_status.get("layers", []) if isinstance(l, dict)}
    meta = layers.get("L2") or layers.get("meta") or layers.get("slow")
    if meta is None:
        return Result("P2.slowloop", Result.SOFT, "L2 layer not in rsi.status response")
    state = meta.get("state", "UNKNOWN")
    if state in ("LIVE", "DATA-GATED"):
        return Result("P2.slowloop", Result.OK, f"L2={state} — slow loop is active",
                      "; ".join(m.get("value", "") for m in meta.get("metrics", [])[:3]))
    if state == "STARVED":
        return Result("P2.slowloop", Result.SOFT, "L2=STARVED — not enough data to propose meta-revisions yet",
                      "normal early in the cycle; accumulates weekly")
    if state in ("FROZEN", "IDLE"):
        return Result("P2.slowloop", Result.HARD, f"L2={state} — slow loop is not turning",
                      "check: MetaEvolutionTask weekly schedule, drift freeze (evolution_drift.go)")
    return Result("P2.slowloop", Result.SOFT, f"L2={state} — unknown state")


def check_labels(rsi_status: dict | None, health: dict | None) -> Result:
    """P3: verifier co-evolution labels accumulating?"""
    if rsi_status is None:
        return Result("P3.labels", Result.SOFT, "rsi.status RPC unavailable")
    layers = {l["key"]: l for l in rsi_status.get("layers", []) if isinstance(l, dict)}
    p3 = layers.get("L3") or layers.get("verifier") or layers.get("coevolve")
    if p3 is None:
        return Result("P3.labels", Result.SOFT, "L3 layer not in rsi.status — P3 may not be wired yet")
    state = p3.get("state", "UNKNOWN")
    if state == "LIVE":
        return Result("P3.labels", Result.OK, "L3=LIVE — verifier co-evolution is producing labels")
    if state == "DATA-GATED":
        # Check if false-accept/reject labels are accumulating in health
        if health:
            evo = health.get("evolution") or health.get("evolutionHealth") or {}
            resolved = evo.get("resolvedFalseAccepts", 0)
            if resolved > 0:
                return Result("P3.labels", Result.SOFT, f"L3=DATA-GATED — {resolved} false-accepts resolved (labels accumulating)",
                              "cutover to active co-evolution when label volume justifies it")
        return Result("P3.labels", Result.SOFT, "L3=DATA-GATED — collecting labels before cutover")
    return Result("P3.labels", Result.SOFT, f"L3={state}")


# ── Main ────────────────────────────────────────────────────────────────────

def main() -> int:
    ap = argparse.ArgumentParser(description="RSI loop health auditor")
    ap.add_argument("--url", default=os.environ.get("DENEB_GATEWAY_URL", "http://127.0.0.1:18789"),
                    help="gateway base URL (default: http://127.0.0.1:18789)")
    ap.add_argument("--token", default=os.environ.get("DENEB_CLIENT_TOKEN", ""),
                    help="client token for RPC (reads ~/.deneb/client_token if unset)")
    ap.add_argument("--json", action="store_true", help="machine-readable JSON output")
    args = ap.parse_args()

    # Resolve token from ~/.deneb/client_token if not provided.
    token = args.token
    if not token:
        token_file = os.path.expanduser("~/.deneb/client_token")
        if os.path.isfile(token_file):
            with open(token_file) as f:
                token = f.read().strip()

    base_url = args.url.rstrip("/")

    # Fetch data.
    health = fetch_json(f"{base_url}/health")
    rsi_status = call_rpc(base_url, "miniapp.rsi.status", token)

    if health is None and rsi_status is None:
        msg = f"❌ Gateway unreachable at {base_url} — is the gateway running?"
        if args.json:
            print(json.dumps({"error": msg, "exit": 2}))
        else:
            print(msg)
        return 2

    # Run checks.
    results = [
        check_liveness(health),
        check_honesty(health),
        check_confirm(health),
        check_slowloop(rsi_status),
        check_labels(rsi_status, health),
    ]

    # Determine exit code.
    worst = Result.OK
    for r in results:
        if r.status == Result.HARD:
            worst = Result.HARD
            break
        if r.status == Result.SOFT and worst != Result.HARD:
            worst = Result.SOFT
    exit_code = {Result.OK: 0, Result.SOFT: 1, Result.HARD: 2}[worst]

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

    # Human-readable.
    print(f"RSI Loop Audit — {base_url}")
    print(f"{'─' * 60}")
    if health:
        evo = health.get("evolution") or health.get("evolutionHealth") or {}
        print(f"  게이트웨이: 연결됨")
        print(f"  evolve(7d): {evo.get('evolveCount7d', evo.get('evolveCount', '?'))}  "
              f"rollback: {evo.get('rollbackCount7d', evo.get('rollbackCount', '?'))}  "
              f"confirm: {evo.get('confirmRate', '?')}")
    else:
        print(f"  게이트웨이: /health 응답 없음 (RPC만 시도)")
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
