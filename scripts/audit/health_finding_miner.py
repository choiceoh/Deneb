"""Proactive L4 supply miner — RSI roadmap P5 workstream 3 (importable module).

Every live code-candidate source is REACTIVE (runtime errors, tool gaps,
harness rejections): a healthy gateway produces nothing and the L4 coding lane
idles exactly when a self-improving system should be renovating. This miner is
the first PROACTIVE source: it reads the two deterministic health benches and
files their standing defects as propose-only, scope=code self-correction
candidates through the existing review lane.

Inputs (both deterministic, both already scored):

  - ``codebase-health-v2.py --format json`` findings with severity high or
    critical (e.g. the volatile-contract blast on ``domain/wiki``).
  - ``runtime-health.py --json`` dimensions scoring below a standing-weakness
    bar (e.g. latency, the #1 weakness at proposal time).

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


class GatewayError(RuntimeError):
    """The gateway RPC failed — the miner must fail loud, not file silently."""


# --- candidate builders (pure) -------------------------------------------------


def structural_candidates(report: dict[str, Any]) -> list[dict[str, Any]]:
    """High-severity structural findings, ranked by priority (desc) then id.

    Uncapped: the reopen/dedup filter runs before the per-run cap so blocked
    findings do not consume dispatch slots.
    """
    revision = str(report.get("revision") or "?")[:12]
    profile = report.get("profile") or "?"
    out: list[dict[str, Any]] = []
    findings = [
        f for f in report.get("findings") or []
        if f.get("severity") in STRUCTURAL_SEVERITIES and f.get("id") and f.get("path")
    ]
    findings.sort(key=lambda f: (-float(f.get("priority") or 0.0), str(f["id"])))
    for f in findings:
        fid = str(f["id"])
        kind = fid.split(":", 1)[0]
        proposed = str(f.get("remediation") or "").strip()
        verify = str(f.get("verify") or "").strip()
        if verify:
            proposed = f"{proposed} Verify: {verify}".strip()
        out.append({
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
            "reason": "codebase-health-v2 high-severity structural finding — "
                      "proactive L4 supply (RSI P5 ws3)",
            "targetFiles": [str(f["path"])],
            "proposedChange": proposed,
            "risk": _RISK_NOTE,
            "source": f"{SOURCE_PREFIX}:{fid}",
        })
    return out


def runtime_candidates(runtime: dict[str, Any] | None) -> list[dict[str, Any]]:
    """Standing runtime weaknesses: dims under the bar, weakest first, capped.

    The runtime report is a rolling 7d window, so the synthetic finding id is
    the dimension name (``runtime-latency``) — stable across runs, which is
    what makes the reopen semantics meaningful.
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
    for name, score in weak[:MAX_RUNTIME_PER_RUN]:
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
        })
    return out


# --- dedup / reopen (mirrors genesis selfCorrectionReopenBlocked) ---------------


def reopen_blocked(existing: list[dict[str, Any]], source: str, now_ms: int) -> str | None:
    """Why a fresh filing for ``source`` is suppressed, or None to allow.

    Twin match is separator-aware (mirrors genesis
    selfCorrectionSourceMatches): exact source, or the same source extended
    past a ":". A bare prefix cross-blocked source ids that are prefixes of
    one another, e.g. "…latency" vs "…latency-p99" (RSI code eval M7/F4).
    A live twin (proposed/accepted) or an operator-ruled one (rejected/
    superseded) blocks; an APPLIED twin re-opens only after the cooldown —
    the bench still reporting the finding now IS the "recurred again" signal.
    Past REOPEN_CAP twins the signature is permanently blocked (genesis
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
    if status != "applied":
        return f"{status} twin {newest.get('id')}"
    if now_ms - int(newest.get("createdAt") or 0) < REOPEN_COOLDOWN_MS:
        return f"applied twin {newest.get('id')} inside reopen cooldown"
    return None


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


# --- bench runners (thin subprocess edges) ---------------------------------------


def parse_leading_json(text: str) -> dict[str, Any]:
    """First JSON object in ``text`` — runtime-health appends metric lines."""
    obj, _ = json.JSONDecoder().raw_decode(text)
    if not isinstance(obj, dict):
        raise ValueError("expected a JSON object")
    return obj


def repo_root() -> str:
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


def run_structural_bench(root: str, stderr: TextIO) -> dict[str, Any]:
    script = os.path.join(root, "scripts", "audit", "codebase-health-v2.py")
    print("running codebase-health-v2 (fast profile)…", file=stderr)
    proc = subprocess.run(
        [sys.executable, script, "--format", "json"],
        capture_output=True, text=True, cwd=root, check=False, timeout=600,
    )
    if proc.returncode != 0:
        raise GatewayError(f"codebase-health-v2 failed (rc={proc.returncode}): {proc.stderr[-400:]}")
    return parse_leading_json(proc.stdout)


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


# --- CLI -------------------------------------------------------------------------


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--report", help="pre-generated codebase-health-v2 JSON report path")
    parser.add_argument("--runtime-report", help="pre-generated runtime-health --json output path")
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
    try:
        report = _load_json_file(args.report) if args.report else run_structural_bench(root, err)
    except (OSError, ValueError, GatewayError) as exc:
        print(f"structural bench unavailable: {exc}", file=err)
        return 1
    if args.runtime_report:
        try:
            runtime: dict[str, Any] | None = _load_json_file(args.runtime_report)
        except (OSError, ValueError) as exc:
            print(f"runtime report unreadable — skipping runtime source: {exc}", file=err)
            runtime = None
    else:
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

    structural_sel, structural_skip = select_candidates(
        structural_candidates(report), existing, now_ms, max(args.max_structural, 0))
    runtime_sel, runtime_skip = select_candidates(
        runtime_candidates(runtime), existing, now_ms, MAX_RUNTIME_PER_RUN)
    to_file = structural_sel + runtime_sel
    skipped = structural_skip + runtime_skip

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
    }
    if args.json:
        print(json.dumps(summary, ensure_ascii=False), file=out)
    else:
        print(
            f"health-finding-miner: planned={summary['planned']} filed={summary['filed']} "
            f"skipped={summary['skipped']} rejected={summary['rejected']}"
            + (" (dry-run)" if args.dry_run else ""),
            file=out,
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
