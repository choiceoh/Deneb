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
    record_candidate,
    select_candidates,
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
    "Deadcode ignores _test.go, so confirm the symbol is truly unreachable at HEAD "
    "(not test-only or a documented extension point) before deleting. Preferred fix is "
    "deletion; if it is a genuine keep, baseline it with operator approval per "
    "docs/agent-rules/testing.md — do NOT edit the baseline to silence review."
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
            "title": f"dead code: {symbol}",
            "candidate": (
                f"'{symbol}' in {file} is unreachable from every gateway binary "
                f"(deadcode-audit NEW finding, not in the checked-in baseline)."
            ),
            "evidence": (
                f"{finding} — x/tools deadcode over ./cmd/... reports this symbol "
                f"unreachable and it is absent from scripts/audit/deadcode-baseline.txt"
                + (f"; {notes[(file, symbol)]}" if notes and (file, symbol) in notes else "")
            ),
            "reason": "deadcode-audit delta — proactive L4 supply (RSI P5 ws3)",
            "targetFiles": [target],
            "proposedChange": (
                f"Delete '{symbol}' and any now-orphaned helpers it solely referenced, "
                f"then re-run scripts/audit/deadcode-audit.sh and confirm the finding "
                f"clears with no new deltas. If it is genuinely test-reachable or a "
                f"documented extension point, baseline it instead (operator approval)."
            ),
            "risk": _RISK_NOTE,
            "source": f"{SOURCE_PREFIX}:{fid}",
        })
    return out


# --- bench runner (thin subprocess edge) ---------------------------------------


def repo_root() -> str:
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


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
    try:
        if args.audit_output:
            with open(args.audit_output, encoding="utf-8") as handle:
                audit_output = handle.read()
        else:
            audit_output = run_deadcode_audit(root, err)
    except (OSError, GatewayError) as exc:
        print(f"deadcode-audit unavailable: {exc}", file=err)
        return 1

    findings = parse_new_findings(audit_output)
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
    }
    if args.json:
        print(json.dumps(summary, ensure_ascii=False), file=out)
    else:
        print(
            f"deadcode-finding-miner: findings={summary['findings']} "
            f"planned={summary['planned']} filed={summary['filed']} "
            f"skipped={summary['skipped']} rejected={summary['rejected']}"
            + (" (dry-run)" if args.dry_run else ""),
            file=out,
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
