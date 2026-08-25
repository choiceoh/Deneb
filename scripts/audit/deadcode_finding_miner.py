"""Deadcode-delta miner — proactive L4 supply, RSI roadmap P5 workstream 3.

The first P5-ws3 slice (``health_finding_miner.py``) files codebase-health and
runtime-health standing defects. This second slice closes the gap the roadmap
left open ("deadcode-audit deltas remain follow-up"): newly-orphaned functions
that ``scripts/audit/deadcode-audit.sh`` reports as NEW dead code (not in the
checked-in baseline) are filed as propose-only, scope=code self-correction
candidates so the coding lane can retire them instead of letting them
accumulate silently (the 2026-06 audit series removed ~8,700 LOC that had built
up exactly this way).

Design decision (mirrors ``health_finding_miner.py`` verbatim): SCRIPTS-SIDE
miner filing over the miniapp RPC, NOT a gateway PeriodicTask. The input is a
whole-program reachability analysis over a git checkout — it lives OUTSIDE the
serving process and moves at repo cadence, not runtime cadence. The RPC edge,
reopen semantics, and per-run cap are IMPORTED from that module so the miners
cannot drift (same principle as ``sop_miner.py``).

Delta source: ``deadcode-audit.sh`` has no JSON mode — it prints new findings
as ``  + <file> :: <symbol>`` lines and exits 1 when any exist (0 clean, 2 on
tooling failure). This miner shells it, parses those lines, and treats exit 1
as the normal "found deltas" signal, not a failure.

Safety (mirrors the template lane):

  - Propose-only landing: miners only file candidates. ``deadcode-finding`` is
    on the compiled dispatch allowlist as of 2026-07-15 (first-batch human
    review dropped); coding-dispatch + deploy-watch remain the ship gates.
  - Dedup/reopen mirrors genesis ``selfCorrectionReopenBlocked`` via the shared
    ``select_candidates`` — one open candidate per finding; a rejected twin
    never re-files (an operator "keep this dead code" veto is respected); an
    APPLIED twin re-files only after a cooldown while the symbol is still dead
    ("the deletion did not land").
  - Per-run cap bounds queue growth; every candidate carries the exact
    ``<file> :: <symbol>`` finding so review stays deterministic.

stdlib-only and importable for deterministic tests; the CLI is
``scripts/audit/deadcode-finding-miner.py``.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import time
from typing import Any, TextIO

from health_finding_miner import (
    DEFAULT_GATEWAY_URL,
    GatewayError,
    fetch_existing,
    pending_impact_observations_for,
    record_candidate,
    record_impact,
    select_candidates,
    write_miner_status,
)
from tool_quality_miner import fetch_behavior

SOURCE_PREFIX = "deadcode-finding"

# Dead code is a clean, low-risk deletion, but a queue full of it helps nobody —
# a bounded few per run keeps review focused while the backlog drains steadily.
MAX_PER_RUN = 3

# deadcode-audit.sh normalizes every finding to "<file> :: <symbol>" and prefixes
# the new ones with "  + " (see its stdout contract). Match that exactly.
_NEW_LINE = re.compile(r"^\s*\+\s+(?P<file>\S+)\s+::\s+(?P<symbol>.+?)\s*$")

_RISK_NOTE = (
    "deadcode 도구는 _test.go를 보지 않으므로, 지우기 전에 HEAD에서 정말 도달 불가인지 "
    "(테스트 전용이거나 문서화된 확장 지점이 아닌지) 확인해야 합니다. 기본 처방은 삭제이며, "
    "정말 남겨야 하는 심볼이면 docs/agent-rules/testing.md 절차대로 운영자 승인을 받아 "
    "baseline에 올립니다 — 리뷰를 잠재우려고 baseline을 편집하면 안 됩니다."
)


# --- delta parsing + candidate building (pure) ---------------------------------


def parse_new_findings(audit_output: str) -> list[tuple[str, str]]:
    """Extract (file, symbol) pairs from deadcode-audit.sh's NEW-findings block.

    Only the ``  + `` lines are new dead code; the ``  - `` lines are stale
    baseline entries (a resolve, not a defect) and are ignored. Deduped and
    sorted so the per-run cap is deterministic.
    """
    seen: set[tuple[str, str]] = set()
    for line in audit_output.splitlines():
        # Skip the "  - " resolved block explicitly — a bare regex on "+" could
        # otherwise misread a symbol name, but the sed contract guarantees the
        # marker is the first non-space glyph.
        stripped = line.lstrip()
        if not stripped.startswith("+"):
            continue
        m = _NEW_LINE.match(line)
        if not m:
            continue
        seen.add((m.group("file"), m.group("symbol").strip()))
    return sorted(seen)


# --- runtime corroboration (Hud talk adoption, 2026-07-20) ---------------------
#
# Static reachability has a known phantom class (tool-version skew flagged live
# code before — the reason the deep gate is advisory). Where a "dead" symbol is
# actually a REGISTERED entry point (rpcmap --handler resolves it to an RPC
# method or tool), the observe window gives runtime evidence: recorded tool
# calls > 0 means the symbol ran — filing its deletion would be a phantom, so
# it is dropped loudly. Zero observed calls corroborates the static verdict on
# the candidate's evidence. Non-entry-point symbols (the common case) have no
# runtime observability and are annotated as static-evidence-only.

RUNTIME_WINDOW_DAYS = 30

_ENTRY_LINE = re.compile(r"^\s*\[(?P<kind>rpc|tool|event)\]\s+(?P<name>\S+)\s*$")


def symbol_probe_name(symbol: str) -> str:
    """Bare identifier for rpcmap --handler: a deadcode symbol may carry a
    receiver or package qualifier ('(*Tracker).Foo', 'pkg.Bar') — the registry
    maps bare handler names."""
    name = symbol.strip().split(" ")[-1]
    if ")." in name:
        name = name.rsplit(").", 1)[-1]
    elif "." in name:
        name = name.rsplit(".", 1)[-1]
    return name.strip("()*")


def parse_entry_points(rpcmap_output: str) -> list[tuple[str, str]]:
    """(kind, name) pairs from rpcmap --handler output (empty = not an entry)."""
    out: list[tuple[str, str]] = []
    for line in rpcmap_output.splitlines():
        m = _ENTRY_LINE.match(line)
        if m:
            out.append((m.group("kind"), m.group("name")))
    return out


def probe_entry_points(symbol: str, root: str) -> list[tuple[str, str]]:
    """Shell rpcmap --handler for symbol; [] on ANY failure — corroboration is
    best-effort evidence enrichment and must never block the miner."""
    try:
        proc = subprocess.run(
            [sys.executable, os.path.join(root, "scripts", "dev", "rpcmap.py"),
             "--handler", symbol_probe_name(symbol)],
            capture_output=True, text=True, timeout=30, check=False)
    except (OSError, subprocess.SubprocessError):
        return []
    if proc.returncode != 0:
        return []
    return parse_entry_points(proc.stdout)


def corroborate(findings: list[tuple[str, str]],
                entry_points_for,
                tool_calls: dict[str, int]) -> tuple[list[tuple[str, str, str]],
                                                     list[tuple[str, str, str]]]:
    """Split findings into kept (with an evidence note) and phantoms (dropped).

    A phantom is a statically-dead symbol whose registered tool recorded calls
    in the observe window — runtime contradicts the static verdict, and filing
    a deletion for live code is exactly the known phantom failure class.
    """
    kept: list[tuple[str, str, str]] = []
    phantoms: list[tuple[str, str, str]] = []
    for file, symbol in findings:
        entries = entry_points_for(symbol)
        if not entries:
            kept.append((file, symbol,
                         "runtime corroboration: n/a — not a registered RPC/tool entry "
                         "point, static reachability is the only evidence"))
            continue
        live = [(k, n) for k, n in entries if k == "tool" and tool_calls.get(n, 0) > 0]
        if live:
            kind, name = live[0]
            phantoms.append((file, symbol,
                             f"registered {kind} '{name}' recorded {tool_calls.get(name, 0)} "
                             f"calls in the {RUNTIME_WINDOW_DAYS}d observe window — statically "
                             f"'dead' but invoked at runtime (phantom, not filed)"))
            continue
        names = ", ".join(f"{k}:{n}" for k, n in entries)
        kept.append((file, symbol,
                     f"runtime corroboration: registered entry point(s) [{names}] recorded 0 "
                     f"observed calls in the {RUNTIME_WINDOW_DAYS}d window — static verdict "
                     f"corroborated"))
    return kept, phantoms


def deadcode_candidates(findings: list[tuple[str, str]],
                        notes: dict[tuple[str, str], str] | None = None) -> list[dict[str, Any]]:
    """Newly-dead symbols as propose-only scope=code candidates.

    Uncapped: the shared reopen/dedup filter runs before the per-run cap so
    blocked findings do not consume dispatch slots. The source id hashes the
    full ``<file> :: <symbol>`` so it is stable across runs and cannot
    prefix-collide with another symbol under startswith matching.
    """
    out: list[dict[str, Any]] = []
    for file, symbol in findings:
        finding = f"{file} :: {symbol}"
        fid = hashlib.sha256(finding.encode()).hexdigest()[:12]
        # deadcode-audit.sh cd's into gateway-go, so its paths are
        # gateway-go-relative (cmd/..., internal/...). Other self-correction
        # candidates carry repo-relative targets (gateway-go/internal/...), and
        # the coding lane resolves targetFiles from the repo root — normalize so
        # the candidate lands on the real file, not a non-existent repo-root
        # path. The raw finding stays in evidence, and the source hash is over
        # the raw finding so dedup is unaffected by this normalization.
        target = file if file.startswith("gateway-go/") else f"gateway-go/{file}"
        out.append({
            "scope": "code",
            "skillName": "deadcode-audit",
            "title": f"죽은 코드: {symbol}",
            "candidate": (
                f"{file} 의 '{symbol}' 이 모든 게이트웨이 바이너리에서 도달 불가입니다 "
                f"(deadcode-audit 새 발견 — 체크인된 baseline에 없음)."
            ),
            "evidence": (
                f"{finding} — x/tools deadcode over ./cmd/... reports this symbol "
                f"unreachable and it is absent from scripts/audit/deadcode-baseline.txt"
                + (f"; {notes[(file, symbol)]}" if notes and (file, symbol) in notes else "")
            ),
            "reason": "deadcode-audit delta — proactive L4 supply (RSI P5 ws3)",
            "targetFiles": [target],
            "proposedChange": (
                f"'{symbol}' 과 그것만 참조하던 고아 헬퍼를 함께 삭제한 뒤, "
                f"scripts/audit/deadcode-audit.sh 를 다시 돌려 새 델타 없이 발견이 "
                f"사라지는지 확인한다. 테스트에서만 도달하거나 문서화된 확장 지점이라면 "
                f"삭제 대신 baseline 등록(운영자 승인)으로 처리한다."
            ),
            "risk": _RISK_NOTE,
            "source": f"{SOURCE_PREFIX}:{fid}",
            # Deterministic usefulness oracle: the exact finding disappears from
            # the next audit run (deleted — or baselined, which is an operator-
            # approved resolution of the same finding). This miner closes its
            # own contracts on later runs (deadcode_impact_resolver), mirroring
            # how the health miner closes health.finding_present.
            "impactContract": {
                "metric": f"deadcode.finding_present:{fid}",
                "direction": "decrease",
                "baseline": 1,
                "target": 0,
                "minSamples": 1,
                # The declared falsifier: absence only counts when the symbol is
                # GONE. A finding that left the audit via deadcode-baseline.txt
                # was suppressed, not fixed — and that file is editable by the
                # very agent being scored (resolver enforces this).
                "guardrails": ["resolved_by_deletion_not_baseline"],
            },
        })
    return out


def baseline_finding_ids(root: str) -> set[str]:
    """Finding ids for everything currently in the checked-in deadcode baseline.

    Same "<file> :: <symbol>" shape the ids hash, so a suppressed finding is
    recognisable by id alone. Missing/unreadable baseline yields an empty set —
    the caller then behaves exactly as before this check existed.
    """
    path = os.path.join(root, "scripts", "audit", "deadcode-baseline.txt")
    out: set[str] = set()
    try:
        with open(path, encoding="utf-8") as fh:
            for line in fh:
                entry = line.strip()
                if not entry or entry.startswith("#") or " :: " not in entry:
                    continue
                out.add(hashlib.sha256(entry.encode()).hexdigest()[:12])
    except OSError:
        return set()
    return out


def finding_ids(findings: list[tuple[str, str]]) -> set[str]:
    """Stable ids for the CURRENT audit findings (same hash as the source id)."""
    return {
        hashlib.sha256(f"{file} :: {symbol}".encode()).hexdigest()[:12]
        for file, symbol in findings
    }


def deadcode_impact_resolver(current_ids: set[str], baselined_ids: set[str] | None = None):
    """Resolver for pending deadcode contracts against a fresh audit run.

    Presence is checked against the RAW parsed findings (pre-phantom-filter):
    the oracle is "does the audit still report it", not "would we file it".
    Metrics outside this miner's namespace return None for their own evaluator.

    A finding can leave the audit two ways, and they are not the same result:
    the symbol was DELETED, or it was added to deadcode-baseline.txt. Only the
    audit's "+" lines feed current_ids, so a baselined finding also reads as
    absent — which would score suppression as a verified fix, on a file no
    CODEOWNERS rule protects and which the dispatched agent can edit. This
    miner's own risk note tells that agent "do NOT edit the baseline to silence
    review"; rewarding the edit anyway is an instruction pointed against its
    own incentive (CLAUDE.md forbids silencing failures with baselines).
    Baselined findings therefore report as still-present: no credit, whether the
    entry was gamed or genuinely operator-approved — an approved keep means the
    candidate improved no code either.
    """
    prefix = "deadcode.finding_present:"
    baselined = baselined_ids or set()

    def resolve(metric: str):
        if not metric.startswith(prefix):
            return None
        fid = metric.removeprefix(prefix)
        if fid in baselined:
            return 1.0, 1, (
                f"fresh deadcode-audit: finding {fid} left the audit by BASELINE, "
                "not deletion — suppression earns no usefulness credit"
            )
        present = fid in current_ids
        state = "still reported" if present else "absent"
        return float(present), 1, f"fresh deadcode-audit: finding {fid} {state}"

    return resolve


# --- bench runner (thin subprocess edge) ---------------------------------------


def repo_root() -> str:
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


def miner_status_path() -> str:
    """FIXED under ~/.deneb/data (same convention as the health miner) so the Go
    rsi-status reader and this writer agree. The miner used to write nothing:
    the 2026-08-18 tooling failure of the weekly unit left no trace anywhere
    the operator looks."""
    return os.path.expanduser("~/.deneb/data/deadcode_finding_miner_status.json")


def record_run_status(payload: dict[str, Any], stderr: TextIO, dry_run: bool) -> None:
    """Best-effort status drop for rsi-status L4 (never on dry runs)."""
    if dry_run:
        return
    write_miner_status(payload, stderr, path=miner_status_path())


def run_deadcode_audit(root: str, stderr: TextIO) -> str:
    """Run deadcode-audit.sh and return its stdout.

    Exit 0 (clean) and exit 1 (new findings) are BOTH normal — 1 just means the
    delta block is populated. Only exit 2 (or a spawn failure) is a real tooling
    fault the miner cannot mine through.
    """
    script = os.path.join(root, "scripts", "audit", "deadcode-audit.sh")
    print("running deadcode-audit (whole-program reachability, ~1-2 min)…", file=stderr)
    try:
        proc = subprocess.run(
            [script],
            capture_output=True, text=True, cwd=root, check=False, timeout=600,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        raise GatewayError(f"deadcode-audit could not run: {exc}") from exc
    if proc.returncode not in (0, 1):
        raise GatewayError(
            f"deadcode-audit tooling failure (rc={proc.returncode}): {proc.stderr[-400:]}"
        )
    return proc.stdout


# --- CLI -----------------------------------------------------------------------


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--audit-output",
                        help="pre-captured deadcode-audit.sh stdout (skips the live run)")
    parser.add_argument("--url", default=os.environ.get("DENEB_GATEWAY_URL", DEFAULT_GATEWAY_URL),
                        help="gateway base URL (env DENEB_GATEWAY_URL)")
    parser.add_argument("--token", default=os.environ.get("DENEB_CLIENT_TOKEN", ""),
                        help="client token (reads ~/.deneb/client_token if unset)")
    parser.add_argument("--max", type=int, default=MAX_PER_RUN,
                        help="per-run cap on deadcode candidates")
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

    root = repo_root()
    now_ms = int(time.time() * 1000)
    try:
        if args.audit_output:
            with open(args.audit_output, encoding="utf-8") as handle:
                audit_output = handle.read()
        else:
            audit_output = run_deadcode_audit(root, err)
    except (OSError, GatewayError) as exc:
        print(f"deadcode-audit unavailable: {exc}", file=err)
        record_run_status({"lastRunAtMs": now_ms, "ok": False, "error": str(exc)[-400:],
                           "findings": 0, "planned": 0, "filed": 0}, err, args.dry_run)
        return 1

    findings = parse_new_findings(audit_output)
    # Presence oracle for contract closure — captured BEFORE the phantom filter
    # prunes the filing list (observation asks the audit, not the filing policy).
    current_ids = finding_ids(findings)
    base_url = args.url.rstrip("/")
    try:
        existing = fetch_existing(base_url, token)
    except GatewayError as exc:
        if not args.dry_run:
            print(f"cannot read the candidate queue — refusing to file blind: {exc}", file=err)
            return 1
        print(f"gateway unreachable — DRY-RUN continues WITHOUT dedup: {exc}", file=err)
        existing = []

    impact_observations, impact_skipped = pending_impact_observations_for(
        existing, deadcode_impact_resolver(current_ids, baseline_finding_ids(root)), now_ms
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

    notes: dict[tuple[str, str], str] = {}
    phantoms: list[tuple[str, str, str]] = []
    if findings:
        tool_calls: dict[str, int] = {}
        try:
            behavior = fetch_behavior(base_url, token, RUNTIME_WINDOW_DAYS)
            for t in behavior.get("tools") or []:
                name = str(t.get("name") or "").strip()
                if name:
                    tool_calls[name] = int(t.get("calls") or 0)
        except GatewayError as exc:
            # Best-effort: without observe data the phantom guard cannot fire,
            # but rpcmap-based entry-point annotation still applies.
            print(f"observe.behavior unavailable — runtime call counts skipped: {exc}", file=err)
        kept, phantoms = corroborate(
            findings, lambda sym: probe_entry_points(sym, root), tool_calls)
        for file, symbol, reason in phantoms:
            print(f"phantom dropped (not filed): {file} :: {symbol} — {reason}", file=err)
        notes = {(f, sym): note for f, sym, note in kept}
        findings = [(f, sym) for f, sym, _ in kept]

    to_file, skipped = select_candidates(
        deadcode_candidates(findings, notes), existing, now_ms, max(args.max, 0))

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
        "findings": len(findings),
        "phantoms": len(phantoms),
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
    record_run_status({
        "lastRunAtMs": now_ms, "ok": True, "error": "",
        "findings": summary["findings"], "planned": summary["planned"], "filed": summary["filed"],
    }, err, args.dry_run)
    if args.json:
        print(json.dumps(summary, ensure_ascii=False), file=out)
    else:
        print(
            f"deadcode-finding-miner: findings={summary['findings']} "
            f"planned={summary['planned']} filed={summary['filed']} "
            f"skipped={summary['skipped']} rejected={summary['rejected']} "
            f"impact-evaluated={summary['impactEvaluated']} "
            f"impact-pending={summary['impactPending']}"
            + (" (dry-run)" if args.dry_run else ""),
            file=out,
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
