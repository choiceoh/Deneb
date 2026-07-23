"""Advisory procedure-credit readout for RSI Bench (provenance → credit assignment).

The provenance work (#4181/#4185 wiki, #4189/#4190/#4196 RSI) stamped every
self-improvement outcome with the content-addressed token of the *procedure* that
governed it — the L1 evolve certificate's ``provenance.procedureRef``, and the L4
candidate's production ``procedureRef`` and dispatch ``dispatchProcedureRef``. That
was the *capture* half. This module is the *consumption* half: it groups the landed
(and total) outcomes back BY procedure ref, so the loop can SEE which procedure
version actually produced utility — the credit-assignment the capture exists to serve.

It reads the same ledgers the scored domains read, but only the additive provenance
fields:

- ``skill_genesis_log.jsonl`` — ``evolved`` rows carry ``provenance.procedureRef``
  (the L1 evolve procedure that rewrote the skill body).
- ``self_correction_candidates.jsonl`` — L4 candidates carry ``procedureRef``
  (production procedure) and ``dispatchProcedureRef`` (dispatch-contract procedure).
  Rows are append-only fragments per candidate ``id``; we fold them first-seen for
  the refs and last-seen for the terminal status, mirroring the Go tracker's fold.

It is deliberately **advisory**: not a scored Metric, not part of the ratchet, and
outside the Report scoring model entirely (main injects it as a payload sidecar). It
exists to make procedure credit observable — the natural next step once provenance is
captured. Promote to a ratcheted term only once a procedure's credit is trustworthy
enough to reward/penalize (a later phase, and a forbidden-surface change).
"""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from .ledgers import data_dir, iter_jsonl

# Terminal statuses that count as a landed L4 outcome — the same set
# load_closure_window() treats as landed (accepted/applied/landed).
_LAND_STATUSES = frozenset({"accepted", "applied", "landed"})

# Bucket for outcomes with no procedure ref (records predating provenance, or a
# deterministic producer that mints none). Surfaced so coverage stays honest —
# "how much credit is still unattributable" is itself the signal.
_UNATTRIBUTED = "(unattributed)"


@dataclass
class _Bucket:
    """One procedure ref's credit: how many outcomes it produced, how many landed."""

    total: int = 0
    landed: int = 0

    def to_dict(self) -> dict[str, int]:
        return {"total": self.total, "landed": self.landed}


def _bump(index: dict[str, _Bucket], ref: str, *, landed: bool) -> None:
    bucket = index.setdefault(ref or _UNATTRIBUTED, _Bucket())
    bucket.total += 1
    if landed:
        bucket.landed += 1


@dataclass
class ProcedureCredit:
    """Credit grouped by governing procedure ref across the three provenance lanes."""

    measured: bool  # any provenance-bearing outcome seen at all
    # L1 evolve procedure ref -> evolves it produced (evolves have no per-row land state).
    evolve: dict[str, _Bucket] = field(default_factory=dict)
    # L4 production procedure ref -> {total candidates, landed}.
    production: dict[str, _Bucket] = field(default_factory=dict)
    # L4 dispatch-contract procedure ref -> {total candidates, landed}.
    dispatch: dict[str, _Bucket] = field(default_factory=dict)

    @staticmethod
    def _attributed(index: dict[str, _Bucket]) -> int:
        return sum(1 for ref in index if ref != _UNATTRIBUTED)

    @staticmethod
    def _index_dict(index: dict[str, _Bucket]) -> dict[str, dict[str, int]]:
        # Sort by landed desc then total desc then ref, so the readout leads with
        # the procedures that actually produced utility (deterministic ties).
        ordered = sorted(index.items(), key=lambda kv: (-kv[1].landed, -kv[1].total, kv[0]))
        return {ref: bucket.to_dict() for ref, bucket in ordered}

    def to_dict(self) -> dict[str, Any]:
        return {
            "measured": self.measured,
            "distinct_procedures": {
                "evolve": self._attributed(self.evolve),
                "production": self._attributed(self.production),
                "dispatch": self._attributed(self.dispatch),
            },
            "evolve": self._index_dict(self.evolve),
            "production": self._index_dict(self.production),
            "dispatch": self._index_dict(self.dispatch),
            "advisory": True,
            "note": "not scored / not ratcheted — provenance→credit attribution",
        }

    def summary(self) -> str:
        if not self.measured:
            return f"unmeasured (no procedure-ref outcomes under {data_dir()})"
        ev = self._attributed(self.evolve)
        prod = self._attributed(self.production)
        disp = self._attributed(self.dispatch)
        prod_landed = sum(b.landed for r, b in self.production.items() if r != _UNATTRIBUTED)
        disp_landed = sum(b.landed for r, b in self.dispatch.items() if r != _UNATTRIBUTED)
        return (
            f"evolveProcs={ev} prodProcs={prod}(landed={prod_landed}) "
            f"dispatchProcs={disp}(landed={disp_landed})"
        )


def _evolve_ref(row: dict[str, Any]) -> str:
    prov = row.get("provenance")
    if isinstance(prov, dict):
        ref = prov.get("procedureRef")
        if isinstance(ref, str):
            return ref.strip()
    return ""


def load_procedure_credit(data: Path | None = None) -> ProcedureCredit:
    """Group self-improvement outcomes by the procedure ref that governed them.

    All-time, not windowed: L4 supply is sparse (matching load_closure_window's
    rationale) and credit attribution wants a procedure's whole lifetime, not a
    7d slice.
    """
    root = data or data_dir()
    out = ProcedureCredit(measured=False)

    # L1: one evolve per procedure ref. Confirm/rollback live on separate rows
    # without provenance, so evolves carry no per-row land state — total only.
    for row in iter_jsonl(root / "skill_genesis_log.jsonl"):
        if str(row.get("type") or "") != "evolved":
            continue
        ref = _evolve_ref(row)
        if not ref:
            continue
        out.measured = True
        _bump(out.evolve, ref, landed=False)

    # L4: fold append-only fragments by candidate id — first-seen for each ref
    # (the tracker keeps the started/production row's version), last non-empty
    # status for the terminal outcome.
    prod_ref: dict[str, str] = {}
    disp_ref: dict[str, str] = {}
    status: dict[str, str] = {}
    order: list[str] = []
    for row in iter_jsonl(root / "self_correction_candidates.jsonl"):
        cid = str(row.get("id") or "").strip()
        if not cid:
            continue
        if cid not in status:
            order.append(cid)
            status[cid] = ""
        pref = str(row.get("procedureRef") or "").strip()
        if pref and cid not in prod_ref:
            prod_ref[cid] = pref
        dref = str(row.get("dispatchProcedureRef") or "").strip()
        if dref and cid not in disp_ref:
            disp_ref[cid] = dref
        st = str(row.get("status") or "").strip().lower()
        if st:
            status[cid] = st

    for cid in order:
        landed = status.get(cid, "") in _LAND_STATUSES
        pref = prod_ref.get(cid, "")
        dref = disp_ref.get(cid, "")
        if pref:
            out.measured = True
            _bump(out.production, pref, landed=landed)
        if dref:
            out.measured = True
            _bump(out.dispatch, dref, landed=landed)

    return out
