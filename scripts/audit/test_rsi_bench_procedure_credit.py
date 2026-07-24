#!/usr/bin/env python3
"""Unit tests for the advisory procedure-credit readout (provenance → credit)."""

from __future__ import annotations

import contextlib
import io
import json
import tempfile
import unittest
from pathlib import Path

import rsi_bench_main
from rsi_bench.procedure_credit import load_procedure_credit


def _write(root: Path, name: str, rows: list[dict]) -> None:
    root.mkdir(parents=True, exist_ok=True)
    (root / name).write_text("".join(json.dumps(r) + "\n" for r in rows), encoding="utf-8")


def _evolved(skill: str, ref: str) -> dict:
    return {
        "type": "evolved",
        "skillName": skill,
        "createdAt": 1_700_000_000_000,
        "provenance": {"procedureRef": ref, "evolveModel": "m"},
    }


def _candidate(cid: str, *, status: str = "", prod: str = "", disp: str = "") -> dict:
    row: dict = {"type": "self_correction_candidate", "id": cid, "createdAt": 1_700_000_000_000}
    if status:
        row["status"] = status
    if prod:
        row["procedureRef"] = prod
    if disp:
        row["dispatchProcedureRef"] = disp
    return row


class ProcedureCreditTests(unittest.TestCase):
    def test_groups_evolve_refs(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            data = Path(tmp)
            _write(
                data,
                "skill_genesis_log.jsonl",
                [
                    _evolved("alpha", "proc-aaaaaaaaaaaa"),
                    _evolved("beta", "proc-aaaaaaaaaaaa"),  # same procedure, 2 evolves
                    _evolved("gamma", "proc-bbbbbbbbbbbb"),
                    {"type": "evolve_confirmed", "skillName": "alpha"},  # no provenance → ignored
                ],
            )
            pc = load_procedure_credit(data)
        self.assertTrue(pc.measured)
        self.assertEqual(pc.evolve["proc-aaaaaaaaaaaa"].total, 2)
        self.assertEqual(pc.evolve["proc-bbbbbbbbbbbb"].total, 1)
        self.assertEqual(pc.to_dict()["distinct_procedures"]["evolve"], 2)

    def test_folds_candidate_fragments_by_id(self) -> None:
        # Append-only fragments: started row carries the refs, a later row carries
        # the terminal status. Fold must keep first-seen refs and last-seen status.
        with tempfile.TemporaryDirectory() as tmp:
            data = Path(tmp)
            _write(
                data,
                "self_correction_candidates.jsonl",
                [
                    _candidate("c1", status="proposed", prod="proc-p1", disp="proc-d1"),
                    _candidate("c1", prod="proc-IGNORED"),  # later ref must NOT override
                    _candidate("c1", status="landed"),  # terminal → landed
                    _candidate("c2", status="proposed", prod="proc-p1", disp="proc-d2"),
                    _candidate("c2", status="rejected"),  # not landed
                ],
            )
            pc = load_procedure_credit(data)
        # production proc-p1 produced two candidates, one landed.
        self.assertEqual(pc.production["proc-p1"].total, 2)
        self.assertEqual(pc.production["proc-p1"].landed, 1)
        # dispatch refs are distinct per candidate.
        self.assertEqual(pc.dispatch["proc-d1"].landed, 1)
        self.assertEqual(pc.dispatch["proc-d2"].landed, 0)
        self.assertEqual(pc.dispatch["proc-d2"].total, 1)

    def test_unattributed_excluded_from_distinct_count(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            data = Path(tmp)
            _write(
                data,
                "self_correction_candidates.jsonl",
                [
                    _candidate("c1", status="landed", prod="proc-p1"),
                    _candidate("c2", status="landed"),  # no ref → unattributed, measured stays via c1
                ],
            )
            pc = load_procedure_credit(data)
        d = pc.to_dict()
        # c2 has no ref at all so it lands in NEITHER index (nothing to attribute).
        self.assertEqual(d["distinct_procedures"]["production"], 1)
        self.assertNotIn("(unattributed)", pc.production)

    def test_unattributed_bucket_when_status_present_but_no_ref(self) -> None:
        # A candidate whose production ref is present but dispatch ref is empty must
        # not create a phantom dispatch bucket; the empty ref simply isn't counted.
        with tempfile.TemporaryDirectory() as tmp:
            data = Path(tmp)
            _write(
                data,
                "self_correction_candidates.jsonl",
                [_candidate("c1", status="landed", prod="proc-p1")],
            )
            pc = load_procedure_credit(data)
        self.assertEqual(len(pc.dispatch), 0)
        self.assertEqual(pc.production["proc-p1"].landed, 1)

    def test_unmeasured_when_empty(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            pc = load_procedure_credit(Path(tmp))
        self.assertFalse(pc.measured)
        self.assertIn("unmeasured", pc.summary())
        d = pc.to_dict()
        self.assertEqual(d["distinct_procedures"], {"evolve": 0, "production": 0, "dispatch": 0})

    def test_index_ordering_leads_with_landed(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            data = Path(tmp)
            _write(
                data,
                "self_correction_candidates.jsonl",
                [
                    _candidate("c1", status="proposed", prod="proc-low"),  # 0 landed
                    _candidate("c2", status="landed", prod="proc-high"),  # 1 landed
                    _candidate("c3", status="landed", prod="proc-high"),
                ],
            )
            pc = load_procedure_credit(data)
        keys = list(pc.to_dict()["production"].keys())
        self.assertEqual(keys[0], "proc-high")  # most-landed leads


class SidecarInjectionTests(unittest.TestCase):
    """The advisory readout must ride alongside the report, never inside its scores."""

    def test_main_injects_sidecar_without_touching_scores(self) -> None:
        with tempfile.TemporaryDirectory() as data_tmp:
            data = Path(data_tmp)
            _write(
                data,
                "self_correction_candidates.jsonl",
                [_candidate("c1", status="landed", prod="proc-p1", disp="proc-d1")],
            )
            buf = io.StringIO()
            with contextlib.redirect_stdout(buf):
                rc = rsi_bench_main.main(["--json", "--data-dir", str(data)])

        self.assertEqual(rc, 0)
        payload = json.loads(buf.getvalue())
        pc = payload["procedure_credit"]
        self.assertTrue(pc["advisory"])
        self.assertEqual(pc["production"]["proc-p1"], {"total": 1, "landed": 1})
        # Scored model is untouched: no procedure key leaks into scores or domains.
        self.assertNotIn("procedure_credit", payload["score"])
        for domain in payload["domains"]:
            self.assertNotIn("procedure", domain["id"])
            for metric in domain["metrics"]:
                self.assertNotIn("procedure", metric["id"])


if __name__ == "__main__":
    unittest.main()
