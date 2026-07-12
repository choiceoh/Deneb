"""Deterministic tests for the RSI loop-status classifier.

The whole value of this surface is telling DATA-GATED (built, waiting for fuel)
apart from STARVED (built, input wiring empty) — so those two are the load-
bearing assertions.
"""

from __future__ import annotations

import io
import json
import os
import tempfile
import unittest

from rsi_status import (
    DATA_GATED,
    FROZEN,
    IDLE,
    LIVE,
    STARVED,
    assess,
    assess_l1,
    assess_l2,
    assess_l3,
    assess_l4,
    main,
)

NOW = 1_700_000_000_000  # fixed clock (ms)
DAY = 24 * 60 * 60 * 1000
RECENT = NOW - DAY  # inside every window
OLD = NOW - 90 * DAY  # outside every window


class L1Test(unittest.TestCase):
    def test_evolves_are_live(self):
        s = assess_l1([{"createdAt": RECENT, "event": "evolved"},
                       {"createdAt": RECENT, "event": "confirmed"}], NOW)
        self.assertEqual(s.state, LIVE)
        self.assertEqual(s.metrics["evolved"], 1)

    def test_no_recent_events_is_idle(self):
        s = assess_l1([{"createdAt": OLD, "event": "evolved"}], NOW)
        self.assertEqual(s.state, IDLE)


class L2Test(unittest.TestCase):
    def test_freeze_wins(self):
        s = assess_l2([{"createdAt": RECENT, "action": "", "proposed": True}], frozen=True, now_ms=NOW)
        self.assertEqual(s.state, FROZEN)

    def test_cycles_are_live(self):
        s = assess_l2([
            {"createdAt": RECENT, "epoch": "evaluator", "artifact": "judge.md", "proposed": True},
            {"createdAt": RECENT, "action": "auto_adopted"},
        ], frozen=False, now_ms=NOW)
        self.assertEqual(s.state, LIVE)
        self.assertEqual(s.metrics["adopted"], 1)

    def test_empty_is_idle(self):
        self.assertEqual(assess_l2([], frozen=False, now_ms=NOW).state, IDLE)


class L3Test(unittest.TestCase):
    def test_blatant_only_is_data_gated_not_starved(self):
        # The judge catches every blatant defect; subtle probes not deployed yet.
        # This is the P3 data-gate — must read DATA-GATED, never STARVED/LIVE.
        s = assess_l3([{"createdAt": RECENT, "pairs": 12, "correct": 12,
                        "byClass": {"section-drop": [3, 3], "fake-tool": [3, 3],
                                    "truncation": [3, 3], "overfit": [3, 3]},
                        "misses": []}], NOW)
        self.assertEqual(s.state, DATA_GATED)
        self.assertFalse(s.metrics["subtle_probes_deployed"])
        self.assertIn("subtle", s.diagnosis.lower())

    def test_misses_are_live_fuel(self):
        s = assess_l3([{"createdAt": RECENT, "pairs": 8, "correct": 6,
                        "byClass": {"safety-drop": [2, 4], "section-drop": [4, 4]},
                        "misses": [{"skill": "sk", "degradation": "safety-drop", "verdict": "passed_defect"},
                                   {"skill": "sk", "degradation": "safety-drop", "verdict": "passed_defect"}]}], NOW)
        self.assertEqual(s.state, LIVE)
        self.assertEqual(s.metrics["misses"], 2)

    def test_false_reject_only_is_live(self):
        s = assess_l3([{"createdAt": RECENT, "byClass": {"safety-drop": [4, 4]},
                        "falseRejects": [{"skill": "sk"}]}], NOW)
        self.assertEqual(s.state, LIVE)

    def test_no_runs_is_idle(self):
        self.assertEqual(assess_l3([], NOW).state, IDLE)


class L4Test(unittest.TestCase):
    def test_skill_and_test_scope_only_is_starved(self):
        # Candidates exist but none are code-scope from a dispatch source — the
        # actual production state we diagnosed. Must be STARVED (wiring gap),
        # not IDLE (no candidates) and not DATA-GATED.
        rows = [
            {"type": "self_correction_candidate", "id": "a", "scope": "skill", "source": "skill-lifecycle"},
            {"type": "self_correction_candidate", "id": "b", "scope": "test", "source": "self-harness"},
        ]
        s = assess_l4(rows, dispatch_total=0, dispatch_today=0)
        self.assertEqual(s.state, STARVED)
        self.assertEqual(s.metrics["dispatchable"], 0)
        self.assertEqual(s.metrics["by_scope"], {"skill": 1, "test": 1})

    def test_code_scope_from_dispatch_source_is_live(self):
        rows = [{"type": "self_correction_candidate", "id": "c", "scope": "code",
                 "status": "proposed", "source": "evolve-tool-gap:xyz"}]
        s = assess_l4(rows, dispatch_total=0, dispatch_today=0)
        self.assertEqual(s.state, LIVE)
        self.assertEqual(s.metrics["dispatchable"], 1)

    def test_status_delta_demotes_candidate(self):
        # A later status delta ({id,status}) must fold onto the candidate, not
        # be counted as a fresh record — an applied candidate is not dispatchable.
        rows = [
            {"type": "self_correction_candidate", "id": "c", "scope": "code",
             "status": "proposed", "source": "evolve-tool-gap:xyz"},
            {"id": "c", "status": "applied"},
        ]
        s = assess_l4(rows, dispatch_total=1, dispatch_today=0)
        self.assertEqual(s.metrics["dispatchable"], 0)
        self.assertEqual(s.state, STARVED)

    def test_no_candidates_is_idle(self):
        self.assertEqual(assess_l4([], 0, 0).state, IDLE)


class AssessAndCliTest(unittest.TestCase):
    def _write(self, data_dir, name, records):
        with open(os.path.join(data_dir, name), "w", encoding="utf-8") as handle:
            for r in records:
                handle.write(json.dumps(r) + "\n")

    def test_assess_end_to_end_and_json_cli(self):
        with tempfile.TemporaryDirectory() as data_dir:
            self._write(data_dir, "skill_genesis_log.jsonl", [{"createdAt": RECENT, "event": "evolved"}])
            self._write(data_dir, "judge_accuracy_log.jsonl",
                        [{"createdAt": RECENT, "pairs": 12, "correct": 12,
                          "byClass": {"section-drop": [3, 3]}, "misses": []}])
            self._write(data_dir, "self_correction_candidates.jsonl",
                        [{"type": "self_correction_candidate", "id": "a", "scope": "skill", "source": "skill-lifecycle"}])

            layers = {layer.key: layer.state for layer in assess(data_dir, NOW)}
            self.assertEqual(layers["L1"], LIVE)
            self.assertEqual(layers["L2"], IDLE)       # no meta ledger
            self.assertEqual(layers["L3"], DATA_GATED)  # blatant-only
            self.assertEqual(layers["L4"], STARVED)     # skill-scope only

            out = io.StringIO()
            rc = main(["--json", "--data-dir", data_dir, "--now-ms", str(NOW)], stdout=out, stderr=io.StringIO())
            self.assertEqual(rc, 0)
            payload = json.loads(out.getvalue().split("DENEB_RSI_STATUS")[0])
            self.assertEqual(len(payload["layers"]), 4)
            self.assertEqual(payload["turning"], 1)  # only L1 live

    def test_missing_data_dir_is_all_idle_not_a_crash(self):
        layers = {layer.key: layer.state for layer in assess("/nonexistent/deneb/data", NOW)}
        self.assertEqual(set(layers.values()), {IDLE})


if __name__ == "__main__":
    unittest.main()
