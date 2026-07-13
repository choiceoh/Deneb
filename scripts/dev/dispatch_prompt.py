#!/usr/bin/env python3
"""Compose the L4 coding-dispatch prompt from the externalized contract artifact.

RSI P5-4 slice 1 (recursion-surface widening, proposal side): the dispatch
prompt's POLICY half lives as a versioned meta artifact —
``<managed genesis dir>/meta/dispatch-contract-prompt.md`` — materialized by
the gateway from the compiled default in
``gateway-go/internal/domain/skills/genesis/generation/prompts.go`` and
sidecar-refreshed when that default moves (pristine files only). This module
owns only COMPOSITION: the candidate data block (data, not policy) plus the
contract text read from the artifact.

Honesty rule: no fallback contract text lives here. A missing or
suspiciously-short artifact DEFERS the dispatch (exit ``DEFER_EXIT``, no
marker written, candidate stays queued) so the contract has exactly one
source of truth — duplicating the default here would drift silently from the
compiled one.

Reads the picked candidate JSON on stdin. Writes the dispatch marker (the
candidate record + promptVersion/promptSource provenance) BEFORE emitting the
prompt, preserving the previous inline implementation's ordering contract:
a crashed session must not redispatch forever.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from pathlib import Path

# Must match generation.MetaDispatchContractPrompt (Go) — test_dispatch_prompt.py
# asserts the parity against the Go source so the two cannot drift.
ARTIFACT_NAME = "dispatch-contract-prompt.md"
# Mirrors generation.MetaArtifactMinBytes: a shorter file is a truncated write
# or a botched future evolve, not a usable contract.
MIN_ARTIFACT_BYTES = 200
# Exit code meaning "artifact unavailable — defer this dispatch, burn nothing".
DEFER_EXIT = 3


def resolve_contract(meta_dir: Path) -> tuple[str, str] | None:
    """Return (contract_text, sha12) or None when the artifact is unusable."""
    path = meta_dir / ARTIFACT_NAME
    try:
        raw = path.read_text(encoding="utf-8", errors="replace").strip()
    except OSError:
        return None
    if len(raw.encode("utf-8")) < MIN_ARTIFACT_BYTES:
        return None
    return raw, hashlib.sha256(raw.encode("utf-8")).hexdigest()[:12]


def compose(candidate: dict, contract: str) -> str:
    """Candidate data block + contract policy — same shape the inline
    implementation produced, so the rollout is behavior-neutral when the
    artifact still equals the compiled default."""
    return f"""자기교정 큐 후보를 구현하라 (RSI L4 자동 배차, id={candidate["id"]}).

## 후보
- 제목: {candidate.get("title", "")}
- 스킬: {candidate.get("skillName", "")}
- 관찰: {candidate.get("candidate", "")}
- 제안 변경: {candidate.get("proposedChange", "")}
- 근거: {candidate.get("evidence", "")}
- 리스크 노트: {candidate.get("risk", "")}

{contract}"""


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--meta-dir", required=True, help="managed genesis meta dir")
    ap.add_argument("--marker", required=True, help="dispatch marker path to write")
    args = ap.parse_args()

    candidate = json.load(sys.stdin)
    resolved = resolve_contract(Path(args.meta_dir))
    if resolved is None:
        print(
            f"dispatch contract artifact unusable at {args.meta_dir}/{ARTIFACT_NAME} "
            "(absent or below the size floor) — deferring dispatch",
            file=sys.stderr,
        )
        return DEFER_EXIT
    contract, version = resolved

    # Preserve prior outcome history across retries so land-rate accounting does
    # not collapse failed→landed into a single "landed" marker (bot #3614).
    attempts: list[dict] = []
    dispatched_at = None
    marker_path = Path(args.marker)
    if marker_path.is_file():
        try:
            prev = json.loads(marker_path.read_text(encoding="utf-8", errors="replace"))
        except (OSError, json.JSONDecodeError, TypeError):
            prev = None
        if isinstance(prev, dict):
            prior = prev.get("attempts")
            if isinstance(prior, list):
                attempts = [a for a in prior if isinstance(a, dict)]
            if prev.get("outcome"):
                snap = {
                    k: prev[k]
                    for k in (
                        "outcome",
                        "outcomeRc",
                        "outcomeElapsedSec",
                        "outcomeAt",
                        "outcomePrState",
                        "promptVersion",
                    )
                    if k in prev
                }
                if snap:
                    attempts.append(snap)
            if isinstance(prev.get("dispatchedAt"), (int, float)):
                dispatched_at = int(prev["dispatchedAt"])

    marker = dict(candidate)
    marker["promptVersion"] = version
    marker["promptSource"] = "artifact"
    marker["dispatchedAt"] = dispatched_at or int(__import__("time").time() * 1000)
    if attempts:
        marker["attempts"] = attempts[-10:]
    marker_path.write_text(
        json.dumps(marker, ensure_ascii=False) + "\n", encoding="utf-8"
    )

    print(compose(candidate, contract))
    return 0


if __name__ == "__main__":
    sys.exit(main())
