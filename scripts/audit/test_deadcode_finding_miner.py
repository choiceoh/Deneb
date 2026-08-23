"""Deterministic tests for the deadcode-delta miner (P5 ws3, second slice).

Load-bearing assertions: only the ``  + `` NEW-findings block becomes
candidates (the ``  - `` resolved block is ignored), every candidate carries
the exact ``<file> :: <symbol>`` finding for deterministic review, the source
id is stable per finding, the shared reopen/cap semantics apply, and a real
run refuses to file when it cannot read the queue.
"""

from __future__ import annotations

import io
import json
import os
import tempfile
import unittest

import deadcode_finding_miner as miner

from deadcode_finding_miner import (
    SOURCE_PREFIX,
    deadcode_candidates,
    main,
    parse_new_findings,
)

# A realistic deadcode-audit.sh stdout: a resolved (stale) block, then the NEW
# findings block. Only the "+ " lines are defects.
# deadcode-audit.sh cd's into gateway-go, so its paths are gateway-go-relative
# (internal/..., cmd/...) — NOT repo-relative. The miner must normalize.
AUDIT_OUTPUT = """\
deadcode-audit: 1 baseline entries no longer dead (stale — refresh with --update):
  - internal/old/gone.go :: OnceUsed
deadcode-audit: NEW dead code (2 findings):
  + internal/pipeline/chat/run_orphan.go :: orphanHelper
  + internal/runtime/server/stale.go :: (*Server).unusedMethod
deadcode-audit: delete the code (preferred) or baseline it with operator approval.
"""

CLEAN_OUTPUT = "deadcode-audit: clean (137 accepted baseline entries, 0 new)\n"


class ParseNewFindingsTest(unittest.TestCase):
    def test_only_new_block_is_parsed(self):
        findings = parse_new_findings(AUDIT_OUTPUT)
        self.assertEqual(findings, [
            ("internal/pipeline/chat/run_orphan.go", "orphanHelper"),
            ("internal/runtime/server/stale.go", "(*Server).unusedMethod"),
        ])

    def test_resolved_block_is_ignored(self):
        # The "- ...OnceUsed" line must never surface as a defect.
        self.assertNotIn("OnceUsed", str(parse_new_findings(AUDIT_OUTPUT)))

    def test_when_clean_output_yields_nothing(self):
        self.assertEqual(parse_new_findings(CLEAN_OUTPUT), [])

    def test_when_dedup_and_sort_are_deterministic(self):
        dup = AUDIT_OUTPUT + "  + internal/pipeline/chat/run_orphan.go :: orphanHelper\n"
        self.assertEqual(parse_new_findings(dup), parse_new_findings(AUDIT_OUTPUT))


class CandidateTest(unittest.TestCase):
    def test_when_candidate_shape(self):
        cands = deadcode_candidates(parse_new_findings(AUDIT_OUTPUT))
        self.assertEqual(len(cands), 2)
        c = cands[0]
        self.assertEqual(c["scope"], "code")
        # gateway-go-relative audit path normalized to a repo-relative target so
        # the coding lane lands on the real file, not a non-existent repo-root path.
        self.assertEqual(c["targetFiles"], ["gateway-go/internal/pipeline/chat/run_orphan.go"])
        self.assertTrue(c["source"].startswith(f"{SOURCE_PREFIX}:"))
        # The exact finding must be in the evidence for deterministic review.
        self.assertIn("internal/pipeline/chat/run_orphan.go :: orphanHelper", c["evidence"])
        self.assertIn("orphanHelper", c["title"])

    def test_already_prefixed_path_not_double_prefixed(self):
        cands = deadcode_candidates([("gateway-go/cmd/x/main.go", "dead")])
        self.assertEqual(cands[0]["targetFiles"], ["gateway-go/cmd/x/main.go"])

    def test_when_source_is_stable_and_distinct(self):
        a = deadcode_candidates(parse_new_findings(AUDIT_OUTPUT))
        b = deadcode_candidates(parse_new_findings(AUDIT_OUTPUT))
        self.assertEqual([c["source"] for c in a], [c["source"] for c in b])
        self.assertNotEqual(a[0]["source"], a[1]["source"])


class CliDryRunTest(unittest.TestCase):
    def _fixture(self, tmp):
        path = os.path.join(tmp, "audit.txt")
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(AUDIT_OUTPUT)
        return path

    def test_dry_run_with_fixture_needs_no_gateway(self):
        with tempfile.TemporaryDirectory() as tmp:
            out, err = io.StringIO(), io.StringIO()
            rc = main(
                ["--audit-output", self._fixture(tmp), "--dry-run", "--json",
                 "--url", "http://127.0.0.1:1", "--token", "t"],
                stdout=out, stderr=err,
            )
            self.assertEqual(rc, 0)
            self.assertIn("DRY-RUN continues WITHOUT dedup", err.getvalue())
            summary = json.loads(out.getvalue().strip().splitlines()[-1])
            self.assertEqual(summary["findings"], 2)
            self.assertEqual(summary["planned"], 2)
            self.assertEqual(summary["filed"], 0)
            self.assertTrue(summary["dry_run"])

    def test_when_cap_limits_filing_plan(self):
        with tempfile.TemporaryDirectory() as tmp:
            out, err = io.StringIO(), io.StringIO()
            rc = main(
                ["--audit-output", self._fixture(tmp), "--dry-run", "--json",
                 "--max", "1", "--url", "http://127.0.0.1:1", "--token", "t"],
                stdout=out, stderr=err,
            )
            self.assertEqual(rc, 0)
            summary = json.loads(out.getvalue().strip().splitlines()[-1])
            self.assertEqual(summary["findings"], 2)
            self.assertEqual(summary["planned"], 1)

    def test_when_real_run_refuses_to_file_blind(self):
        with tempfile.TemporaryDirectory() as tmp:
            out, err = io.StringIO(), io.StringIO()
            rc = main(
                ["--audit-output", self._fixture(tmp),
                 "--url", "http://127.0.0.1:1", "--token", "t"],
                stdout=out, stderr=err,
            )
            self.assertEqual(rc, 1)
            self.assertIn("refusing to file blind", err.getvalue())


if __name__ == "__main__":
    unittest.main()


class RuntimeCorroborationTest(unittest.TestCase):
    """Phantom guard + evidence annotation (Hud talk adoption, 2026-07-20)."""

    def test_phantom_with_runtime_calls_is_dropped(self):
        findings = [("internal/a.go", "ToolFoo"), ("internal/b.go", "helperBar")]
        entries = {"ToolFoo": [("tool", "foo")], "helperBar": []}
        kept, phantoms = miner.corroborate(
            findings, lambda sym: entries.get(miner.symbol_probe_name(sym), []),
            {"foo": 42})
        self.assertEqual([(f, s) for f, s, _ in phantoms], [("internal/a.go", "ToolFoo")])
        self.assertIn("42", phantoms[0][2])
        self.assertEqual([(f, s) for f, s, _ in kept], [("internal/b.go", "helperBar")])
        self.assertIn("n/a", kept[0][2])

    def test_zero_call_entry_point_is_corroborated(self):
        kept, phantoms = miner.corroborate(
            [("internal/a.go", "ToolFoo")],
            lambda sym: [("tool", "foo")], {"foo": 0})
        self.assertEqual(phantoms, [])
        self.assertIn("0 observed calls", kept[0][2])
        self.assertIn("corroborated", kept[0][2])

    def test_rpc_entry_points_annotate_without_phantom(self):
        # observe.behavior has no RPC counts — an RPC-mapped symbol annotates
        # but never trips the phantom guard on missing data.
        kept, phantoms = miner.corroborate(
            [("internal/a.go", "peopleList")],
            lambda sym: [("rpc", "miniapp.people.list")], {})
        self.assertEqual(phantoms, [])
        self.assertIn("rpc:miniapp.people.list", kept[0][2])

    def test_notes_ride_candidate_evidence(self):
        notes = {("internal/b.go", "helperBar"): "runtime corroboration: n/a"}
        cands = miner.deadcode_candidates([("internal/b.go", "helperBar")], notes)
        self.assertIn("runtime corroboration: n/a", cands[0]["evidence"])

    def test_parse_entry_points(self):
        out = "  [rpc] miniapp.people.list\n        → peopleList (x.go:92)\n  [tool] wiki\n"
        self.assertEqual(miner.parse_entry_points(out),
                         [("rpc", "miniapp.people.list"), ("tool", "wiki")])


class ImpactContractTests(unittest.TestCase):
    """Every deadcode candidate must carry the finding-present contract, and the
    miner must close its own contracts from a fresh audit (the ledger showed 3
    applied deadcode candidates with no usefulness verdict — landed deletions
    nobody could distinguish from no-ops)."""

    def test_candidates_carry_finding_present_contract(self):
        cands = deadcode_candidates([("internal/a.go", "OldFunc")])
        contract = cands[0].get("impactContract")
        self.assertIsNotNone(contract)
        fid = cands[0]["source"].split(":", 1)[1]
        self.assertEqual(contract["metric"], f"deadcode.finding_present:{fid}")
        self.assertEqual(contract["direction"], "decrease")
        self.assertEqual((contract["baseline"], contract["target"]), (1, 0))

    def test_resolver_denies_credit_for_baselined_finding(self):
        """Baselining removes a finding from the audit's "+" block exactly like
        deletion does. Only one of those improved the code, and the file is
        editable by the very agent whose usefulness is being scored — so a
        baselined finding must read as still-present, not as a verified fix."""
        from deadcode_finding_miner import deadcode_impact_resolver, finding_ids

        suppressed = [("internal/c.go", "BaselinedFunc")]
        fid = deadcode_candidates(suppressed)[0]["source"].split(":", 1)[1]

        # Gone from the fresh audit (as a baseline entry always is) but present
        # in the checked-in baseline.
        resolve = deadcode_impact_resolver(finding_ids([]), {fid})
        observed, samples, note = resolve(f"deadcode.finding_present:{fid}")
        self.assertEqual((observed, samples), (1.0, 1))
        self.assertIn("BASELINE", note)

    def test_resolver_without_baseline_arg_keeps_prior_behaviour(self):
        """The baseline set is optional so an older caller cannot break."""
        from deadcode_finding_miner import deadcode_impact_resolver, finding_ids

        resolve = deadcode_impact_resolver(finding_ids([]))
        observed, _, note = resolve("deadcode.finding_present:deadbeef1234")
        self.assertEqual(observed, 0.0)
        self.assertIn("absent", note)

    def test_baseline_finding_ids_reads_the_checked_in_file(self):
        """Ids must hash the same "<file> :: <symbol>" shape the findings do, or
        suppression would never be recognised."""
        import hashlib
        import tempfile

        from deadcode_finding_miner import baseline_finding_ids

        with tempfile.TemporaryDirectory() as root:
            audit = os.path.join(root, "scripts", "audit")
            os.makedirs(audit)
            with open(os.path.join(audit, "deadcode-baseline.txt"), "w", encoding="utf-8") as fh:
                fh.write("# comment line\n\ninternal/c.go :: BaselinedFunc\n")
            want = hashlib.sha256(b"internal/c.go :: BaselinedFunc").hexdigest()[:12]
            self.assertEqual(baseline_finding_ids(root), {want})

    def test_baseline_finding_ids_tolerates_missing_file(self):
        from deadcode_finding_miner import baseline_finding_ids

        self.assertEqual(baseline_finding_ids("/nonexistent-root-xyz"), set())

    def test_resolver_closes_from_fresh_audit(self):
        from deadcode_finding_miner import deadcode_impact_resolver, finding_ids

        gone = [("internal/a.go", "DeletedFunc")]
        still = [("internal/b.go", "StillDeadFunc")]
        current = finding_ids(still)
        resolve = deadcode_impact_resolver(current)

        gone_fid = deadcode_candidates(gone)[0]["source"].split(":", 1)[1]
        observed, samples, note = resolve(f"deadcode.finding_present:{gone_fid}")
        self.assertEqual((observed, samples), (0.0, 1))
        self.assertIn("absent", note)

        still_fid = deadcode_candidates(still)[0]["source"].split(":", 1)[1]
        observed, _, note = resolve(f"deadcode.finding_present:{still_fid}")
        self.assertEqual(observed, 1.0)
        self.assertIn("still reported", note)

        # Another evaluator's namespace is not ours to guess.
        self.assertIsNone(resolve("health.finding_present:abc"))


class RunStatusTests(unittest.TestCase):
    """The lane used to leave no trace: the 2026-08-18 weekly unit failed on the
    deadcode tooling and nothing the operator reads recorded it. Both the
    failure path and the success path must drop a status file; dry runs must
    not touch it."""

    def test_failure_path_records_status(self):
        import deadcode_finding_miner as dfm

        with tempfile.TemporaryDirectory() as tmp:
            status_path = os.path.join(tmp, "status.json")
            original = dfm.miner_status_path
            dfm.miner_status_path = lambda: status_path
            try:
                rc = main(["--audit-output", os.path.join(tmp, "missing.txt"),
                           "--url", "http://127.0.0.1:1", "--token", "t"],
                          stdout=io.StringIO(), stderr=io.StringIO())
            finally:
                dfm.miner_status_path = original
            self.assertEqual(rc, 1)
            with open(status_path, encoding="utf-8") as handle:
                payload = json.load(handle)
            self.assertFalse(payload["ok"])
            self.assertIn("missing.txt", payload["error"])
            self.assertEqual(payload["planned"], 0)

    def test_dry_run_leaves_status_untouched(self):
        import deadcode_finding_miner as dfm

        with tempfile.TemporaryDirectory() as tmp:
            status_path = os.path.join(tmp, "status.json")
            audit = os.path.join(tmp, "audit.txt")
            with open(audit, "w", encoding="utf-8") as handle:
                handle.write(AUDIT_OUTPUT)
            original = dfm.miner_status_path
            dfm.miner_status_path = lambda: status_path
            try:
                rc = main(["--audit-output", audit, "--dry-run", "--json",
                           "--url", "http://127.0.0.1:1", "--token", "t"],
                          stdout=io.StringIO(), stderr=io.StringIO())
            finally:
                dfm.miner_status_path = original
            self.assertEqual(rc, 0)
            self.assertFalse(os.path.exists(status_path))
