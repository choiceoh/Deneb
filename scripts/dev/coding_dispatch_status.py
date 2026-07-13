#!/usr/bin/env python3
"""Atomically persist the last RSI L4 dispatcher tick outcome."""

from __future__ import annotations

import argparse
import json
import time
from pathlib import Path


FAILURE_RESULTS = frozenset({
    "queue_missing", "environment_failed", "setup_failed", "prompt_failed",
    "ledger_failed", "session_failed",
})
SUCCESS_RESULTS = frozenset({"completed", "pr_opened", "merged"})


def record_status(
    path: Path,
    result: str,
    *,
    detail: str = "",
    candidate_id: str = "",
    now_ms: int | None = None,
) -> dict:
    now_ms = now_ms if now_ms is not None else int(time.time() * 1000)
    try:
        current = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError, TypeError):
        current = {}
    if not isinstance(current, dict):
        current = {}

    current.update({
        "lastTickAtMs": now_ms,
        "lastResult": result,
        "detail": detail.strip(),
    })
    if candidate_id:
        current["candidateId"] = candidate_id
    if result == "dispatched":
        current["lastDispatchAtMs"] = now_ms
        current["consecutiveFailures"] = 0
    elif result in FAILURE_RESULTS:
        current["consecutiveFailures"] = int(current.get("consecutiveFailures") or 0) + 1
    elif result != "busy":
        current["consecutiveFailures"] = 0
    if result in SUCCESS_RESULTS:
        current["lastSuccessfulAtMs"] = now_ms

    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(current, ensure_ascii=False) + "\n", encoding="utf-8")
    tmp.replace(path)
    return current


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--state-file", required=True)
    parser.add_argument("--result", required=True)
    parser.add_argument("--detail", default="")
    parser.add_argument("--candidate", default="")
    parser.add_argument("--now-ms", type=int)
    args = parser.parse_args(argv)
    record_status(
        Path(args.state_file), args.result,
        detail=args.detail, candidate_id=args.candidate, now_ms=args.now_ms,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
