"""Tool-quality miner — proactive L4 supply, RSI surface expansion (Lane A).

The recursion surface already INCLUDES tool descriptions: 41 of 45 tools carry
their natural-language description as a Go literal (`toolctx.ToolDef.Description`
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

Safety (mirrors the template lane):

  - Propose-only: the `tool-quality` source namespace is deliberately NOT in
    coding-dispatch.sh's allowlist. Candidates stage for review; the allowlist
    flip is a separate graduation (roadmap ladder).
  - A candidate proposes a DESCRIPTION/SCHEMA clarification only — never
    removing a tool or widening its permission surface.
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
    call_rpc,
    fetch_existing,
    record_candidate,
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

# The registration hub is the entry point for a reviewer/coding-lane to locate
# the offending ToolDef.Description; the evidence names the exact tool.
TOOLREG_HUB = "gateway-go/internal/pipeline/chat/toolreg/core.go"

_RISK_NOTE = (
    "propose-only; tighten the tool's DESCRIPTION (and input schema only if a "
    "field is genuinely wrong) to reduce the observed rate — never remove the "
    "tool or widen its permission surface. Re-run observe.behavior after the "
    "change and confirm the rate drops before landing."
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
            "title": f"tool description/schema quality: {name} ({head})",
            "candidate": (
                f"The '{name}' tool shows {head} over {calls} calls — the model is "
                f"repeatedly misusing it, which points at a misleading description or "
                f"input schema rather than a runtime fault."
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
                f"Locate the ToolDef.Description (and input schema) for '{name}' — start at "
                f"{TOOLREG_HUB}, or grep the tool name across internal/pipeline/chat — and "
                f"tighten it to reduce the observed {head}: clarify required arguments, value "
                f"formats, and when-to-use so the model stops misusing it."
            ),
            "risk": _RISK_NOTE,
            "source": f"{SOURCE_PREFIX}:{name}",
        }))
    scored.sort(key=lambda s: (-s[0], str(s[1]["source"])))
    return [c for _, c in scored]


# --- behavior source (thin RPC edge) -------------------------------------------


def fetch_behavior(base_url: str, token: str, days: int) -> dict[str, Any]:
    return call_rpc(base_url, "miniapp.observe.behavior", {"days": days}, token)


# --- CLI -----------------------------------------------------------------------


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--behavior-report",
                        help="pre-fetched observe.behavior JSON (skips the live RPC)")
    parser.add_argument("--url", default=os.environ.get("DENEB_GATEWAY_URL", DEFAULT_GATEWAY_URL),
                        help="gateway base URL (env DENEB_GATEWAY_URL)")
    parser.add_argument("--token", default=os.environ.get("DENEB_CLIENT_TOKEN", ""),
                        help="client token (reads ~/.deneb/client_token if unset)")
    parser.add_argument("--days", type=int, default=WINDOW_DAYS, help="behavior window in days")
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
    try:
        if args.behavior_report:
            with open(args.behavior_report, encoding="utf-8") as handle:
                behavior = json.load(handle)
        else:
            behavior = fetch_behavior(base_url, token, args.days)
    except (OSError, ValueError, GatewayError) as exc:
        print(f"behavior source unavailable: {exc}", file=err)
        return 1
    tools = behavior.get("tools") if isinstance(behavior, dict) else None
    if not isinstance(tools, list):
        tools = []

    now_ms = int(time.time() * 1000)
    try:
        existing = fetch_existing(base_url, token)
    except GatewayError as exc:
        if not args.dry_run:
            print(f"cannot read the candidate queue — refusing to file blind: {exc}", file=err)
            return 1
        print(f"gateway unreachable — DRY-RUN continues WITHOUT dedup: {exc}", file=err)
        existing = []

    to_file, skipped = select_candidates(
        tool_quality_candidates(tools), existing, now_ms, max(args.max, 0))

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
        "tools": len(tools),
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
            f"tool-quality-miner: tools={summary['tools']} planned={summary['planned']} "
            f"filed={summary['filed']} skipped={summary['skipped']} rejected={summary['rejected']}"
            + (" (dry-run)" if args.dry_run else ""),
            file=out,
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
