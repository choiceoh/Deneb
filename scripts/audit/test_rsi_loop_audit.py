"""Deterministic tests for the independent RSI loop auditor.

The auditor's value is honesty against the REAL gateway schemas: the genesis
health section lives under ``self_evolution`` (snake_case fields), the RPC
envelope carries its body under ``payload``, and a rate with zero resolved
evolves is UNMEASURED — the load-bearing assertions guard exactly those three
facts, because the first version of this tool got all three wrong and graded a
live loop FAIL.
"""

from __future__ import annotations

import json
import os
import tempfile
import unittest

from rsi_loop_audit import (
    Result,
    check_corpus,
    check_confirm,
    check_dispatch,
    check_honesty,
    check_labels,
    check_liveness,
    check_slowloop,
    genesis_section,
    overall_status,
    run_checks,
    unwrap_rpc_envelope,
)

NOW = 1_783_830_000_000  # fixed clock (ms)
HOUR = 3600 * 1000

# Shapes below mirror a live gateway capture (2026-07-12), trimmed to the
# fields the auditor reads.


def health_fixture(**overrides):
    section = {
        "evolves_7d": 3,
        "genesis_7d": 2,
        "evolve_rejected_7d": 6,
        "evolve_rolled_back_7d": 0,
        "evolve_confirmed_7d": 4,
        "resolved_evolves_7d": 4,
        "confirm_rate": 1.0,
        "false_accept_rate": 0.0,
        "last_activity_ms": NOW - 2 * HOUR,
        "last_activity_age": "2h",
        "meta_revisions_7d": 1,
        "meta_proposed_7d": 1,
    }
    section.update(overrides)
    return {"status": "ok", "self_evolution": section}


def layer(key, state, diagnosis="", metrics=()):
    return {
        "key": key,
        "title": key,
        "state": state,
        "diagnosis": diagnosis,
        "metrics": [{"label": label, "value": value} for label, value in metrics],
    }


def rsi_status_fixture(l2="LIVE", l3="DATA-GATED", l4="STARVED"):
    return {
        "layers": [
            layer("L1", "LIVE", "3 evolved"),
            layer("L2", l2, "1 slow-loop revisions", (("revisions (7d)", "1"),)),
            layer("L3", l3, "subtle probes pending"),
            layer("L4", l4, "no dispatchable code candidates", (("code-scope", "0"),)),
        ],
        "turning": 2,
    }



def healthy_corpus_dir() -> str:
    """A state dir whose validation corpus can discriminate.

    run_checks would otherwise read the REAL ~/.deneb ledger and make these
    assertions depend on the machine — the same production-state leak the
    genesis tracker guard closes.
    """
    d = tempfile.mkdtemp()
    os.makedirs(os.path.join(d, "data"), exist_ok=True)
    with open(os.path.join(d, "data", "skill_validation_cases.jsonl"), "w", encoding="utf-8") as fh:
        for _ in range(80):
            fh.write('{"source":"adversarial-coverage"}\n')
        for _ in range(20):
            fh.write('{"source":"auto-failed-skill-use"}\n')
    return d

class TestEnvelope(unittest.TestCase):
    def test_payload_key_is_unwrapped(self):
        body = {"layers": [], "turning": 0}
        env = {"type": "res", "id": "x", "ok": True, "payload": body}
        self.assertEqual(unwrap_rpc_envelope(env), body)

    def test_when_legacy_result_key_still_accepted(self):
        body = {"layers": []}
        self.assertEqual(unwrap_rpc_envelope({"result": body}), body)

    def test_ok_false_means_failure(self):
        self.assertIsNone(unwrap_rpc_envelope({"ok": False, "payload": {"layers": []}}))

    def test_when_bare_body_passes_through(self):
        body = {"layers": [], "turning": 1}
        self.assertEqual(unwrap_rpc_envelope(body), body)

    def test_when_non_dict_is_none(self):
        self.assertIsNone(unwrap_rpc_envelope(None))
        self.assertIsNone(unwrap_rpc_envelope("nope"))


class TestGenesisSection(unittest.TestCase):
    def test_when_self_evolution_key(self):
        h = health_fixture()
        self.assertEqual(genesis_section(h)["evolves_7d"], 3)

    def test_when_propus_alias(self):
        h = {"propus": {"evolves_7d": 1}}
        self.assertEqual(genesis_section(h)["evolves_7d"], 1)

    def test_missing_section(self):
        self.assertIsNone(genesis_section({"status": "ok"}))
        self.assertIsNone(genesis_section(None))


class TestLiveness(unittest.TestCase):
    def test_when_live_loop_passes(self):
        r = check_liveness(health_fixture(), NOW)
        self.assertEqual(r.status, Result.OK)

    def test_when_genesis_only_still_passes(self):
        r = check_liveness(health_fixture(evolves_7d=0, genesis_7d=2), NOW)
        self.assertEqual(r.status, Result.OK)

    def test_dormant_with_recent_activity_warns(self):
        r = check_liveness(health_fixture(evolves_7d=0, genesis_7d=0), NOW)
        self.assertEqual(r.status, Result.SOFT)

    def test_dead_loop_fails(self):
        h = health_fixture(evolves_7d=0, genesis_7d=0, last_activity_ms=NOW - 100 * HOUR)
        self.assertEqual(check_liveness(h, NOW).status, Result.HARD)

    def test_missing_section_warns_not_fails(self):
        self.assertEqual(check_liveness({"status": "ok"}, NOW).status, Result.SOFT)

    def test_unreachable_fails(self):
        self.assertEqual(check_liveness(None, NOW).status, Result.HARD)


class TestRates(unittest.TestCase):
    def test_when_zero_resolved_is_unmeasured_warn(self):
        # THE honesty case: rates exist but nothing resolved — never PASS/FAIL.
        h = health_fixture(resolved_evolves_7d=0, confirm_rate=0.0, false_accept_rate=0.0)
        self.assertEqual(check_honesty(h).status, Result.SOFT)
        self.assertIn("unmeasured", check_honesty(h).diagnosis)
        self.assertEqual(check_confirm(h).status, Result.SOFT)

    def test_missing_fields_warn_old_gateway(self):
        section = health_fixture()["self_evolution"]
        for key in ("confirm_rate", "false_accept_rate", "resolved_evolves_7d", "evolve_confirmed_7d"):
            section.pop(key, None)
        h = {"self_evolution": section}
        self.assertEqual(check_honesty(h).status, Result.SOFT)
        self.assertEqual(check_confirm(h).status, Result.SOFT)

    def test_small_sample_never_hard_fails(self):
        # 1 resolved, 1 rollback → falseAcceptRate 1.0 — must stay WARN.
        h = health_fixture(resolved_evolves_7d=1, false_accept_rate=1.0, confirm_rate=0.0)
        self.assertEqual(check_honesty(h).status, Result.SOFT)
        self.assertEqual(check_confirm(h).status, Result.SOFT)

    def test_when_healthy_rates_pass(self):
        h = health_fixture(resolved_evolves_7d=5, false_accept_rate=0.1, confirm_rate=0.9)
        self.assertEqual(check_honesty(h).status, Result.OK)
        self.assertEqual(check_confirm(h).status, Result.OK)

    def test_bad_rates_fail_with_sample(self):
        h = health_fixture(resolved_evolves_7d=6, false_accept_rate=0.5, confirm_rate=0.1)
        self.assertEqual(check_honesty(h).status, Result.HARD)
        self.assertEqual(check_confirm(h).status, Result.HARD)


class TestLayers(unittest.TestCase):
    def test_when_l2_live_passes(self):
        r = check_slowloop(rsi_status_fixture(l2="LIVE"), health_fixture())
        self.assertEqual(r.status, Result.OK)

    def test_l2_frozen_fails(self):
        r = check_slowloop(rsi_status_fixture(l2="FROZEN"), health_fixture())
        self.assertEqual(r.status, Result.HARD)

    def test_l2_fallback_to_health_meta(self):
        r = check_slowloop(None, health_fixture())
        self.assertEqual(r.status, Result.OK)

    def test_l2_fallback_frozen_marker_fails(self):
        r = check_slowloop(None, health_fixture(auto_adopt_frozen=True))
        self.assertEqual(r.status, Result.HARD)

    def test_l3_data_gated_warns_with_diagnosis(self):
        r = check_labels(rsi_status_fixture(l3="DATA-GATED"))
        self.assertEqual(r.status, Result.SOFT)
        self.assertIn("subtle probes pending", r.diagnosis)

    def test_when_l3_live_passes(self):
        self.assertEqual(check_labels(rsi_status_fixture(l3="LIVE")).status, Result.OK)

    def test_when_l4_starved_warns(self):
        r = check_dispatch(rsi_status_fixture(l4="STARVED"))
        self.assertEqual(r.status, Result.SOFT)
        self.assertIn("code-scope=0", r.detail)

    def test_l4_idle_fails(self):
        self.assertEqual(check_dispatch(rsi_status_fixture(l4="IDLE")).status, Result.HARD)

    def test_missing_rpc_warns(self):
        self.assertEqual(check_labels(None).status, Result.SOFT)
        self.assertEqual(check_dispatch(None).status, Result.SOFT)


class TestOverall(unittest.TestCase):
    def test_live_gateway_capture_grades_warn_not_fail(self):
        # The end-to-end regression: a healthy-but-data-gated loop (today's
        # production reality) must grade WARN overall, never FAIL.
        results = run_checks(health_fixture(resolved_evolves_7d=0), rsi_status_fixture(), NOW)
        worst, exit_code = overall_status(results)
        self.assertEqual(worst, Result.SOFT)
        self.assertEqual(exit_code, 1)
        self.assertFalse(any(r.status == Result.HARD for r in results))

    def test_when_all_healthy_exits_zero(self):
        results = run_checks(
            health_fixture(resolved_evolves_7d=5, confirm_rate=0.9, false_accept_rate=0.1),
            rsi_status_fixture(l2="LIVE", l3="LIVE", l4="LIVE"),
            NOW,
            healthy_corpus_dir(),
        )
        worst, exit_code = overall_status(results)
        self.assertEqual((worst, exit_code), (Result.OK, 0))

    def test_when_dead_loop_exits_two(self):
        h = health_fixture(evolves_7d=0, genesis_7d=0, last_activity_ms=NOW - 100 * HOUR)
        results = run_checks(h, rsi_status_fixture(), NOW, healthy_corpus_dir())
        worst, exit_code = overall_status(results)
        self.assertEqual((worst, exit_code), (Result.HARD, 2))


if __name__ == "__main__":
    unittest.main()


class CorpusDiscriminationTest(unittest.TestCase):
    """Why acceptance stalls — the link the other checks never showed.

    Traced by hand 2026-08-26: 1,695 validation cases, 10 from real failures
    (0.6%). A synthetic pool cannot separate a rewrite from its original, so
    candidates tie and the strict-improvement margin is unreachable (30d: 118
    proposals, 28 rejections, 0 accepted). The accept-rate alone reads
    "unmeasured" and says nothing about the cause.
    """

    def _corpus(self, rows):
        d = tempfile.mkdtemp()
        os.makedirs(os.path.join(d, "data"), exist_ok=True)
        with open(os.path.join(d, "data", "skill_validation_cases.jsonl"), "w", encoding="utf-8") as fh:
            for r in rows:
                fh.write(json.dumps(r, ensure_ascii=False) + "\n")
        return d

    def test_synthetic_corpus_warns(self):
        rows = [{"source": "adversarial-coverage"}] * 99 + [{"source": "auto-failed-skill-use"}]
        res = check_corpus(self._corpus(rows))
        self.assertEqual(res.status, Result.SOFT)
        self.assertIn("동점", res.diagnosis)

    def test_real_failure_corpus_passes(self):
        rows = [{"source": "adversarial-coverage"}] * 80 + [{"source": "auto-failed-skill-use"}] * 20
        res = check_corpus(self._corpus(rows))
        self.assertEqual(res.status, Result.OK)

    def test_missing_ledger_is_unmeasured_not_failure(self):
        res = check_corpus(tempfile.mkdtemp())
        self.assertEqual(res.status, Result.SOFT)
        self.assertIn("미측정", res.diagnosis)
