#!/usr/bin/env python3
"""Read and append RSI L4 dispatch lifecycle events through the gateway RPC.

The genesis tracker is the single writer for ``self_correction_candidates.jsonl``.
Shell automation therefore uses this client instead of appending the ledger
directly. Secrets are read from the normal client-token file and never printed.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

import dispatch_outcome

DEFAULT_GATEWAY_URL = "http://127.0.0.1:18789"


class RPCError(RuntimeError):
    """The gateway could not durably accept the requested lifecycle event."""


def load_token(state_dir: Path) -> str:
    token = os.environ.get("DENEB_CLIENT_TOKEN", "").strip()
    if token:
        return token
    try:
        return (state_dir / "client_token").read_text(encoding="utf-8").strip()
    except OSError:
        return ""


def call_rpc(base_url: str, token: str, method: str, params: dict[str, Any]) -> dict[str, Any]:
    headers = {"Content-Type": "application/json"}
    if token:
        headers["X-Deneb-Client-Token"] = token
    body = json.dumps(
        {"type": "req", "id": "self-correction-dispatch", "method": method, "params": params}
    ).encode()
    request = urllib.request.Request(
        f"{base_url.rstrip('/')}/api/v1/miniapp/rpc",
        data=body,
        headers=headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            data = json.loads(response.read())
    except (urllib.error.URLError, urllib.error.HTTPError, ValueError, OSError) as exc:
        raise RPCError(f"{method} unavailable: {exc}") from exc
    if not isinstance(data, dict) or data.get("ok") is False or data.get("error"):
        raise RPCError(f"{method} rejected: {data.get('error') if isinstance(data, dict) else data}")
    payload = data.get("payload")
    if not isinstance(payload, dict):
        raise RPCError(f"{method} returned no payload")
    return payload


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--url", default=os.environ.get("DENEB_GATEWAY_URL", DEFAULT_GATEWAY_URL))
    parser.add_argument("--state-dir", default=os.environ.get("DENEB_STATE_DIR", "~/.deneb"))
    sub = parser.add_subparsers(dest="command", required=True)

    record = sub.add_parser("record", help="append one validated dispatch event")
    record.add_argument("--id", required=True)
    record.add_argument("--phase", required=True)
    record.add_argument("--attempt-id", required=True)
    record.add_argument("--branch", default="")
    record.add_argument("--pr-number", type=int, default=0)
    record.add_argument("--pr-url", default="")
    record.add_argument("--commit-sha", default="")
    record.add_argument("--deploy-head", default="")
    record.add_argument("--note", default="")

    listing = sub.add_parser("list", help="print current candidates as tab-separated lifecycle rows")
    listing.add_argument("--phase", action="append", default=[])
    listing.add_argument("--json", action="store_true", help="print matching candidate rows as JSON")

    next_candidate = sub.add_parser("next", help="print the next policy-approved dispatch candidate")
    next_candidate.add_argument("--dispatch-dir", required=True)
    next_candidate.add_argument("--abandon-after", type=int, default=dispatch_outcome.DEFAULT_ABANDON_AFTER_SEC)
    next_candidate.add_argument("--exclude-id", action="append", default=[])

    result = sub.add_parser("result", help="classify and append one session result from deterministic facts")
    result.add_argument("--id", required=True)
    result.add_argument("--attempt-id", required=True)
    result.add_argument("--branch", default="")
    result.add_argument("--rc", type=int, required=True)
    result.add_argument("--ahead", type=int)
    result.add_argument("--pr-state", default="")
    result.add_argument("--pr-number", type=int, default=0)
    result.add_argument("--pr-url", default="")
    result.add_argument("--commit-sha", default="")
    result.add_argument("--note", default="")

    impact = sub.add_parser("impact", help="record post-watch usefulness observations")
    impact.add_argument("--id", required=True)
    impact.add_argument("--attempt-id", required=True)
    impact.add_argument("--observed", type=float, required=True)
    impact.add_argument("--samples", type=int, required=True)
    impact.add_argument("--guardrail-violation", action="append", default=[])
    impact.add_argument("--note", default="")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    state_dir = Path(args.state_dir).expanduser()
    token = load_token(state_dir)
    try:
        if args.command == "record":
            call_rpc(
                args.url,
                token,
                "miniapp.self_improvement_coding.dispatch",
                {
                    "id": args.id,
                    "dispatchPhase": args.phase,
                    "attemptId": args.attempt_id,
                    "branch": args.branch,
                    "prNumber": args.pr_number,
                    "prUrl": args.pr_url,
                    "commitSha": args.commit_sha,
                    "deployHead": args.deploy_head,
                    "outcomeNote": args.note,
                },
            )
            return 0

        if args.command == "result":
            params = {
                "id": args.id,
                "attemptId": args.attempt_id,
                "branch": args.branch,
                "returnCode": args.rc,
                "prState": args.pr_state,
                "prNumber": args.pr_number,
                "prUrl": args.pr_url,
                "commitSha": args.commit_sha,
                "outcomeNote": args.note,
            }
            if args.ahead is not None:
                params["ahead"] = args.ahead
            payload = call_rpc(
                args.url,
                token,
                "miniapp.self_improvement_coding.dispatch",
                params,
            )
            phase = str(payload.get("dispatchPhase") or "")
            if not phase:
                raise RPCError("dispatch result returned no phase")
            print(phase)
            return 0

        if args.command == "impact":
            payload = call_rpc(
                args.url,
                token,
                "miniapp.self_improvement_coding.impact",
                {
                    "id": args.id,
                    "attemptId": args.attempt_id,
                    "observed": args.observed,
                    "samples": args.samples,
                    "guardrailViolations": args.guardrail_violation,
                    "note": args.note,
                },
            )
            status = str(payload.get("impactStatus") or "")
            if not status:
                raise RPCError("impact result returned no status")
            print(status)
            return 0

        if args.command == "next":
            excluded = {str(value).strip() for value in args.exclude_id if str(value).strip()}
            dispatch_dir = Path(args.dispatch_dir).expanduser()
            for marker in dispatch_dir.glob("*.json"):
                if dispatch_outcome.blocks_redispatch(
                    marker,
                    abandon_after_sec=args.abandon_after,
                ):
                    excluded.add(marker.stem)
            payload = call_rpc(
                args.url,
                token,
                "miniapp.self_improvement_coding.list",
                {
                    "status": "all",
                    "limit": 500,
                    "dispatchableOnly": True,
                    "excludeIds": sorted(excluded),
                },
            )
            candidates = payload.get("candidates") or []
            if candidates:
                print(json.dumps(candidates[0], ensure_ascii=False))
            return 0

        payload = call_rpc(
            args.url,
            token,
            "miniapp.self_improvement_coding.list",
            {"status": "all", "limit": 500},
        )
        phases = set(args.phase)
        candidates = [
            candidate for candidate in (payload.get("candidates") or [])
            if not phases or str(candidate.get("dispatchPhase") or "") in phases
        ]
        if args.json:
            print(json.dumps(candidates, ensure_ascii=False))
            return 0
        for candidate in candidates:
            phase = str(candidate.get("dispatchPhase") or "")
            fields = (
                candidate.get("id"),
                candidate.get("attemptId"),
                phase,
                candidate.get("commitSha"),
                candidate.get("deployHead"),
            )
            if fields[0] and fields[1]:
                print("\t".join(str(value or "").replace("\t", " ") for value in fields))
        return 0
    except RPCError as exc:
        print(str(exc), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
