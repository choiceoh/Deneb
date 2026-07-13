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

# Defense-in-depth for the acceptor boundary (RSI code eval C2/F1): the
# record-time surface gate only sees structured targetFiles, so a candidate
# whose PROSE names acceptance machinery would otherwise flow verbatim into a
# headless session with landing authority. Any mention of these basenames in
# the candidate text defers the dispatch to operator review — conservative on
# purpose; a legitimate candidate discussing a gate file still deserves a
# human eye before an unattended session edits near it. Pinned against the
# forbidden surface whitelist in surfaces.go by test_dispatch_prompt.py.
FORBIDDEN_SURFACE_BASENAMES = (
    "validation_engine.go",
    "validation_replay.go",
    "eprocess.go",
    "meta_judge_bench.go",
    "meta_producer_bench.go",
    "meta_genesis_bench.go",
    "meta_evolution.go",
    "judge_accuracy.go",
    "surfaces.go",
    "tracker_usage.go",
    "tracker_self_correction.go",
    "tracker_eprocess_cutover.go",
    "evolution_drift.go",
    "rsi_ladder.go",
    "ladder_watch.go",
    "graduation_state.go",
    "coding-dispatch.sh",
    "dispatch_prompt.py",
    "dispatch_outcome.py",
    "prompt_cache.go",
    "cache_breakpoints.go",
    "tier1_cache.go",
    "prompt_snapshot_persist.go",
    "dependabot.yml",
    "codeql.yml",
)


def forbidden_surface_mentions(candidate: dict) -> list[str]:
    """Forbidden-surface basenames mentioned anywhere in the candidate's
    free text or structured targets (case-insensitive)."""
    fields = [
        str(candidate.get(k, ""))
        for k in ("title", "skillName", "candidate", "proposedChange", "evidence", "risk")
    ]
    targets = candidate.get("targetFiles")
    if isinstance(targets, list):
        fields.extend(str(x) for x in targets)
    blob = "\n".join(fields).lower()
    return [name for name in FORBIDDEN_SURFACE_BASENAMES if name in blob]


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
    ap.add_argument("--attempt-id", required=True, help="unique delivery attempt id")
    ap.add_argument("--branch", required=True, help="delivery worktree branch")
    args = ap.parse_args()

    candidate = json.load(sys.stdin)
    if mentions := forbidden_surface_mentions(candidate):
        print(
            f"candidate {candidate.get('id')} mentions forbidden acceptance "
            f"surfaces {mentions} — deferring to operator review (no marker burned)",
            file=sys.stderr,
        )
        return DEFER_EXIT
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
    marker["attemptId"] = args.attempt_id
    marker["branch"] = args.branch
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
