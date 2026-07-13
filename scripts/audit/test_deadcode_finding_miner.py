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

    def test_clean_output_yields_nothing(self):
        self.assertEqual(parse_new_findings(CLEAN_OUTPUT), [])

    def test_dedup_and_sort_are_deterministic(self):
        dup = AUDIT_OUTPUT + "  + internal/pipeline/chat/run_orphan.go :: orphanHelper\n"
        self.assertEqual(parse_new_findings(dup), parse_new_findings(AUDIT_OUTPUT))


class CandidateTest(unittest.TestCase):
    def test_candidate_shape(self):
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

    def test_source_is_stable_and_distinct(self):
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

    def test_cap_limits_filing_plan(self):
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

    def test_real_run_refuses_to_file_blind(self):
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
