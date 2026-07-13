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
    LADDER_CALIBRATION_OPENED_MS,
    assess_ladder,
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
    turning,
    render_markdown,
)

NOW = 1_700_000_000_000  # fixed clock (ms)
DAY = 24 * 60 * 60 * 1000
RECENT = NOW - DAY  # inside every window
OLD = NOW - 90 * DAY  # outside every window


class L1Test(unittest.TestCase):
    def test_committed_evolves_are_live(self):
        # Real genesis-log schema keys by `type`, not event/action.
        s = assess_l1([{"createdAt": RECENT, "type": "evolved"},
                       {"createdAt": RECENT, "type": "genesis"},
                       {"createdAt": RECENT, "type": "evolution_proposal"}], NOW)
        self.assertEqual(s.state, LIVE)
        self.assertEqual(s.metrics["evolved"], 1)
        self.assertEqual(s.metrics["genesis"], 1)
        self.assertEqual(s.metrics["proposal"], 1)

    def test_proposals_without_commits_is_data_gated(self):
        # The lane is active but nothing clears the gate — DATA-GATED, not IDLE.
        s = assess_l1([{"createdAt": RECENT, "type": "evolution_proposal"},
                       {"createdAt": RECENT, "type": "evolve_rejected"}], NOW)
        self.assertEqual(s.state, DATA_GATED)
        self.assertEqual(s.metrics["rejected"], 1)

    def test_no_recent_events_is_idle(self):
        s = assess_l1([{"createdAt": OLD, "type": "evolved"}], NOW)
        self.assertEqual(s.state, IDLE)

    def test_eprocess_readiness_counts_all_history(self):
        # 20 baseline-test labels with 1 disagreement (95% agreement) → ready.
        # Labels are counted over the WHOLE ledger (old entries included) —
        # the ladder evidence accumulates, it does not expire.
        events = [{"createdAt": OLD, "type": "evolve_rolled_back",
                   "baselineTest": {"reject": True, "rejectReachable": True, "disagreement": i == 0}}
                  for i in range(20)]
        events.append({"createdAt": RECENT, "type": "evolved"})
        s = assess_l1(events, NOW)
        self.assertEqual(s.metrics["eprocess_labels"], 20)
        self.assertEqual(s.metrics["eprocess_disagreements"], 1)
        self.assertTrue(s.metrics["eprocess_cutover_ready"])

    def test_eprocess_readiness_needs_agreement_and_n(self):
        # n=19 all-agree: below the floor. n=20 at 85%: below agreement bar.
        base = {"createdAt": OLD, "type": "evolve_confirmed"}
        n19 = [dict(base, baselineTest={"rejectReachable": True, "disagreement": False}) for _ in range(19)]
        self.assertFalse(assess_l1(n19, NOW).metrics["eprocess_cutover_ready"])
        n20_noisy = [dict(base, baselineTest={"rejectReachable": True, "disagreement": i < 3}) for i in range(20)]
        self.assertFalse(assess_l1(n20_noisy, NOW).metrics["eprocess_cutover_ready"])

    def test_eprocess_readiness_excludes_unreachable_labels(self):
        # Labels recorded while rejection was mathematically unreachable
        # (rejectReachable false/absent) are excluded from the count — matching
        # the Go filter, so the dashboard cannot show a false READY (Codex).
        events = [{"createdAt": OLD, "type": "evolve_rolled_back",
                   "baselineTest": {"disagreement": False}}  # no rejectReachable → unfair
                  for _ in range(25)]
        s = assess_l1(events, NOW)
        self.assertEqual(s.metrics["eprocess_labels"], 0)
        self.assertEqual(s.metrics["eprocess_unfair_labels"], 25)
        self.assertFalse(s.metrics["eprocess_cutover_ready"])


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

    def test_assess_reads_last_freeze_verdict_not_file_presence(self):
        # Parity with Go AutoAdoptFrozen: frozen:false unfreezes even though
        # the marker file still exists.
        with tempfile.TemporaryDirectory() as data_dir:
            freeze = os.path.join(data_dir, "auto_adopt_freeze.json")
            with open(freeze, "w", encoding="utf-8") as handle:
                handle.write(json.dumps({"frozen": True, "createdAt": OLD}) + "\n")
                handle.write(json.dumps({"frozen": False, "createdAt": RECENT}) + "\n")
            layers = {layer.key: layer.state for layer in assess(data_dir, NOW)}
            self.assertEqual(layers["L2"], IDLE)
            with open(freeze, "a", encoding="utf-8") as handle:
                handle.write(json.dumps({"frozen": True, "createdAt": RECENT}) + "\n")
            layers = {layer.key: layer.state for layer in assess(data_dir, NOW)}
            self.assertEqual(layers["L2"], FROZEN)


class L3Test(unittest.TestCase):
    def test_blatant_only_is_data_gated_not_starved(self):
        # The judge catches every blatant defect; subtle probes not deployed yet.
        # This is the P3 data-gate — must read DATA-GATED, never STARVED/LIVE.
        s = assess_l3([{"createdAt": RECENT, "pairs": 12, "correct": 12,
                        "byClass": {"section-drop": [3, 3], "fake-tool": [3, 3],
                                    "truncation": [3, 3], "overfit": [3, 3]},
                        "misses": []}], [], NOW)
        self.assertEqual(s.state, DATA_GATED)
        self.assertFalse(s.metrics["subtle_probes_deployed"])
        self.assertIn("subtle", s.diagnosis.lower())

    def test_misses_are_live_fuel(self):
        s = assess_l3([{"createdAt": RECENT, "pairs": 8, "correct": 6,
                        "byClass": {"safety-drop": [2, 4], "section-drop": [4, 4]},
                        "misses": [{"skill": "sk", "degradation": "safety-drop", "verdict": "passed_defect"},
                                   {"skill": "sk", "degradation": "safety-drop", "verdict": "passed_defect"}]}], [], NOW)
        self.assertEqual(s.state, LIVE)
        self.assertEqual(s.metrics["misses"], 2)

    def test_false_reject_only_is_live(self):
        s = assess_l3([{"createdAt": RECENT, "byClass": {"safety-drop": [4, 4]},
                        "falseRejects": [{"skill": "sk"}]}], [], NOW)
        self.assertEqual(s.state, LIVE)

    def test_weaken_tier_zero_miss_reads_probe_ceiling(self):
        # Escalated (weaken-tier) probes in the ledger with zero misses: still
        # DATA-GATED, but the diagnosis must say the lane already probes at its
        # difficulty ceiling — not promise a future escalation.
        s = assess_l3([{"createdAt": RECENT, "pairs": 4, "correct": 4,
                        "byClass": {"imperative-drop": [2, 2], "imperative-weaken": [2, 2]},
                        "misses": []}], [], NOW)
        self.assertEqual(s.state, DATA_GATED)
        self.assertTrue(s.metrics["weaken_probes_deployed"])
        self.assertIn("weaken tier", s.diagnosis)

    def test_drop_tier_zero_miss_promises_escalation(self):
        # Drop-tier-only saturation names the ladder's next step.
        s = assess_l3([{"createdAt": RECENT, "pairs": 2, "correct": 2,
                        "byClass": {"imperative-drop": [1, 1], "safety-drop": [1, 1]},
                        "misses": []}], [], NOW)
        self.assertEqual(s.state, DATA_GATED)
        self.assertFalse(s.metrics["weaken_probes_deployed"])
        self.assertIn("escalates", s.diagnosis)

    def test_organic_false_accepts_are_live_fuel(self):
        # A baseline-CONFIRMED rollback (e-process agreed) is a real-usage P3
        # label; a baseline-quiet rollback is a disagreement label, not fuel.
        runs = [{"createdAt": RECENT, "pairs": 8, "correct": 8,
                 "byClass": {"safety-drop": [4, 4]}, "misses": []}]
        genesis = [
            {"createdAt": RECENT, "type": "evolve_rolled_back",
             "baselineTest": {"reject": True, "disagreement": False}},
            {"createdAt": RECENT, "type": "evolve_rolled_back",
             "baselineTest": {"reject": False, "disagreement": True}},
            {"createdAt": OLD, "type": "evolve_rolled_back",  # outside 30d
             "baselineTest": {"reject": True, "disagreement": False}},
        ]
        s = assess_l3(runs, genesis, NOW)
        self.assertEqual(s.state, LIVE)
        self.assertEqual(s.metrics["organic_false_accepts_30d"], 1)

    def test_no_runs_is_idle(self):
        self.assertEqual(assess_l3([], [], NOW).state, IDLE)

    def test_operator_verdict_is_live_fuel_without_synthetic_run(self):
        s = assess_l3([{
            "createdAt": RECENT,
            "judgeVersion": "v1",
            "operatorVerdicts": [{"decisionId": "sk@1.0.1", "verdict": "confirm"}],
        }], [], NOW)
        self.assertEqual(s.state, LIVE)
        self.assertEqual(s.metrics["runs"], 0)
        self.assertEqual(s.metrics["operator_labels_7d"], 1)


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

    def test_dispatch_outcomes_feed_land_rate(self):
        # Graduation-ladder evidence: recorded marker outcomes yield a land
        # rate over DECIDED dispatches; the LIVE diagnosis names it.
        rows = [{"type": "self_correction_candidate", "id": "d", "scope": "code",
                 "status": "accepted", "source": "health-finding:x"}]
        s = assess_l4(rows, dispatch_total=3, dispatch_today=0,
                      outcomes={"landed": 1, "declined": 1})
        self.assertEqual(s.metrics["land_rate"], 0.5)
        self.assertEqual(s.metrics["dispatch_outcomes"], {"landed": 1, "declined": 1})
        self.assertIn("land rate 50%", s.diagnosis)
        # No outcomes recorded yet (pre-accounting markers): rate is None, not 0.
        s = assess_l4(rows, dispatch_total=3, dispatch_today=0, outcomes={})
        self.assertIsNone(s.metrics["land_rate"])
        self.assertNotIn("land rate", s.diagnosis)

    def test_dispatch_lifecycle_replaces_marker_guessing(self):
        rows = [
            {"type": "self_correction_candidate", "id": "flight", "scope": "code",
             "status": "proposed", "source": "self-harness:x"},
            {"type": "self_correction_dispatch", "id": "flight", "dispatchPhase": "started",
             "attemptId": "a1"},
            {"type": "self_correction_candidate", "id": "closed", "scope": "code",
             "status": "accepted", "source": "self-harness:y"},
            {"type": "self_correction_dispatch", "id": "closed", "dispatchPhase": "watch_passed",
             "attemptId": "a2"},
        ]
        s = assess_l4(rows, dispatch_total=2, dispatch_today=0)
        self.assertEqual(s.state, LIVE)
        self.assertEqual(s.metrics["dispatchable"], 0)
        self.assertEqual(s.metrics["in_flight"], 1)
        self.assertEqual(s.metrics["applied"], 1)

    def test_legacy_marker_id_is_not_double_counted_as_dispatchable(self):
        rows = [{"type": "self_correction_candidate", "id": "legacy", "scope": "code",
                 "status": "proposed", "source": "self-harness:x"}]
        s = assess_l4(rows, dispatch_total=1, dispatch_today=0, dispatched_ids={"legacy"})
        self.assertEqual(s.state, LIVE)
        self.assertEqual(s.metrics["dispatchable"], 0)
        self.assertEqual(s.metrics["legacy_in_flight"], 1)

    def test_staged_non_dispatch_code_supply_is_visible(self):
        # Proposed code candidates from sources outside the dispatch allowlist
        # (runtime-error #3491) are staged supply. The layer stays STARVED
        # (nothing dispatchable) but must count them and must NOT claim "no
        # source produces code candidates".
        rows = [
            {"type": "self_correction_candidate", "id": "r1", "scope": "code",
             "status": "proposed", "source": "runtime-error:abc123"},
        ]
        s = assess_l4(rows, dispatch_total=0, dispatch_today=0)
        self.assertEqual(s.state, STARVED)
        self.assertEqual(s.metrics["dispatchable"], 0)
        self.assertEqual(s.metrics["staged"], 1)
        self.assertEqual(s.metrics["staged_sources"], {"runtime-error": 1})
        self.assertIn("staged", s.diagnosis)
        self.assertIn("allowlist graduation", s.diagnosis)
        self.assertNotIn("wiring gap", s.diagnosis)

    def test_health_finding_graduated_to_dispatchable(self):
        # Graduation regression (2026-07-12): health-finding cleared its first
        # batch review, so its candidates count DISPATCHABLE and turn L4 LIVE —
        # while runtime-error next to it stays staged.
        rows = [
            {"type": "self_correction_candidate", "id": "h1", "scope": "code",
             "status": "proposed", "source": "health-finding:volatile-hub:46a381ef4981"},
            {"type": "self_correction_candidate", "id": "r1", "scope": "code",
             "status": "proposed", "source": "runtime-error:abc123"},
        ]
        s = assess_l4(rows, dispatch_total=0, dispatch_today=0)
        self.assertEqual(s.state, LIVE)
        self.assertEqual(s.metrics["dispatchable"], 1)
        self.assertEqual(s.metrics["staged"], 1)
        self.assertEqual(s.metrics["staged_sources"], {"runtime-error": 1})

    def test_accepted_candidate_is_dispatch_supply(self):
        # Review-endorsed (accepted) candidates are LIVE dispatch supply, not
        # settled: the heartbeat review lane accepts queue candidates it cannot
        # implement itself (observed live 2026-07-12), and the dispatcher picks
        # them first. rejected/applied still settle.
        rows = [
            {"type": "self_correction_candidate", "id": "h1", "scope": "code",
             "status": "proposed", "source": "health-finding:x"},
            {"id": "h1", "status": "accepted"},
        ]
        s = assess_l4(rows, dispatch_total=0, dispatch_today=0)
        self.assertEqual(s.state, LIVE)
        self.assertEqual(s.metrics["dispatchable"], 1)

    def test_reviewed_staged_candidate_stops_counting(self):
        # A rejected staged candidate is settled, not awaiting graduation.
        rows = [
            {"type": "self_correction_candidate", "id": "h1", "scope": "code",
             "status": "proposed", "source": "health-finding:x"},
            {"id": "h1", "status": "rejected"},
        ]
        s = assess_l4(rows, dispatch_total=0, dispatch_today=0)
        self.assertEqual(s.metrics["staged"], 0)
        self.assertIn("wiring gap", s.diagnosis)

    def test_no_candidates_is_idle(self):
        self.assertEqual(assess_l4([], 0, 0).state, IDLE)


class AssessAndCliTest(unittest.TestCase):
    def _write(self, data_dir, name, records):
        with open(os.path.join(data_dir, name), "w", encoding="utf-8") as handle:
            for r in records:
                handle.write(json.dumps(r) + "\n")

    def test_assess_end_to_end_and_json_cli(self):
        with tempfile.TemporaryDirectory() as data_dir:
            self._write(data_dir, "skill_genesis_log.jsonl", [{"createdAt": RECENT, "type": "evolved"}])
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
            self.assertEqual(len(payload["layers"]), 5)  # L1-L4 + GRAD ladder
            self.assertEqual(payload["turning"], 1)  # only L1 live; GRAD never counts

    def test_missing_data_dir_is_all_idle_not_a_crash(self):
        layers = {layer.key: layer.state for layer in assess("/nonexistent/deneb/data", NOW)}
        self.assertEqual(layers.pop("GRAD"), DATA_GATED)  # ladder: evidence accumulating
        self.assertEqual(set(layers.values()), {IDLE})

    def test_markdown_is_generated_from_the_same_snapshot(self):
        layers = [assess_l1([{"createdAt": RECENT, "type": "evolved"}], NOW)]
        doc = render_markdown(layers, NOW, "/tmp/deneb-data")
        self.assertIn("# Deneb RSI live status", doc)
        self.assertIn("**Turning: 1/1**", doc)
        self.assertIn("| L1 — skill evolution | LIVE |", doc)
        self.assertIn("Do not edit by hand", doc)

    def test_cli_can_atomically_write_markdown(self):
        with tempfile.TemporaryDirectory() as data_dir:
            output = os.path.join(data_dir, "generated", "rsi-status.md")
            rc = main(["--data-dir", data_dir, "--now-ms", str(NOW),
                       "--write-markdown", output], stdout=io.StringIO(), stderr=io.StringIO())
            self.assertEqual(rc, 0)
            with open(output, encoding="utf-8") as handle:
                self.assertIn("# Deneb RSI live status", handle.read())


class LadderTest(unittest.TestCase):
    def test_empty_evidence_accumulates(self):
        s = assess_ladder([], [], [], {})
        self.assertEqual((s.key, s.state), ("GRAD", DATA_GATED))
        self.assertEqual(len(s.metrics), 5)
        self.assertIn("증거 축적 중", s.diagnosis)

    def test_dispatch_and_staged_evidence_read_ready(self):
        cands = [{"type": "self_correction_candidate", "id": "r1", "scope": "code",
                  "status": "proposed", "source": "runtime-error:abcd"}]
        s = assess_ladder([], [], cands, {"landed": 3, "declined": 2})
        self.assertEqual(s.state, LIVE)
        self.assertEqual(s.metrics["배차 캡 상향"], "준비됨")
        self.assertEqual(s.metrics["스테이징 소스 졸업"], "준비됨")
        self.assertIn("운영자 결정 가능", s.diagnosis)
        # Below the land-rate floor the cap row keeps accumulating.
        s = assess_ladder([], [], [], {"landed": 1, "failed": 4})
        self.assertEqual(s.metrics["배차 캡 상향"], "축적 중")

    def test_calibration_needs_every_epoch_at_target(self):
        revs = [{"createdAt": LADDER_CALIBRATION_OPENED_MS + 1, "epoch": e,
                 "benchShadow": {"skills": 1}} for e in ("producer",) * 10 + ("evaluator",) * 10]
        s = assess_ladder([], revs, [], {})
        self.assertEqual(s.metrics["캘리브레이션 창 종료"], "축적 중")
        revs += [{"createdAt": LADDER_CALIBRATION_OPENED_MS + 1, "epoch": "genesis",
                  "benchGenesis": {"scenarios": 3}}] * 10
        s = assess_ladder([], revs, [], {})
        self.assertEqual(s.metrics["캘리브레이션 창 종료"], "준비됨")

    def test_ladder_never_counts_toward_turning(self):
        ready = assess_ladder([], [], [], {"landed": 5})
        self.assertEqual(ready.state, LIVE)
        self.assertEqual(turning([ready]), 0)

    def test_markdown_denominator_excludes_grad(self):
        # H4: the markdown "Turning: n/N" denominator must exclude the GRAD
        # dashboard, matching print_summary and the "never counts toward the
        # headline" contract — it previously used len(layers) (N+1).
        l1 = assess_l1([{"createdAt": RECENT, "type": "evolved"}], NOW)
        grad = assess_ladder([], [], [], {"landed": 5})
        doc = render_markdown([l1, grad], NOW, "/tmp/deneb-data")
        self.assertIn("**Turning: 1/1**", doc)


class MirrorParityTest(unittest.TestCase):
    """The Go (rsi_ladder.go / rsi_status.go) and Python mirrors classify the
    same ledgers; a drifted constant made them disagree by a day (H4). Pin the
    load-bearing shared constants against the Go source so they cannot drift
    silently again."""

    def _go_source(self, rel: str) -> str:
        import pathlib
        return (pathlib.Path(__file__).resolve().parents[2] / rel).read_text(encoding="utf-8")

    def test_calibration_window_constant_matches_go(self):
        import datetime
        import re
        go = self._go_source(
            "gateway-go/internal/domain/skills/genesis/rsi_ladder.go")
        m = re.search(
            r"ladderCalibrationOpenedMs\s*=\s*time\.Date\((\d+),\s*(\d+),\s*(\d+),",
            go)
        self.assertIsNotNone(m, "ladderCalibrationOpenedMs not found in rsi_ladder.go")
        y, mo, d = (int(g) for g in m.groups())
        want = int(datetime.datetime(y, mo, d, tzinfo=datetime.timezone.utc).timestamp() * 1000)
        self.assertEqual(LADDER_CALIBRATION_OPENED_MS, want)


if __name__ == "__main__":
    unittest.main()
