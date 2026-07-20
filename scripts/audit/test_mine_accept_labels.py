"""Unit contracts for mine_accept_labels: the pure labeling rule and the
append-only candidate fold (last state per id, partial updates never erase
established provenance fields)."""

import unittest

import mine_accept_labels as mal

DAY_MS = 86_400_000


class DecideLabelTests(unittest.TestCase):
    def test_labels_cover_pr_lifecycle(self) -> None:
        now = 100 * DAY_MS
        cases = [
            ("MERGED", now - 5 * DAY_MS, False, "accepted"),
            ("MERGED", now - 5 * DAY_MS, True, "reverted"),
            ("MERGED", now - 1 * DAY_MS, False, "pending"),
            ("CLOSED", 0, False, "rejected"),
            ("OPEN", 0, False, "in-flight"),
            ("", 0, False, "unknown"),
        ]
        for state, merged, reverted, want in cases:
            got = mal.decide_label(state, merged, reverted, now, min_days=3)
            self.assertEqual(got, want, f"{state}/{merged}/{reverted}")


class FoldCandidatesTests(unittest.TestCase):
    def test_last_state_wins_and_partial_updates_keep_provenance(self) -> None:
        records = [
            {"id": "sc-1", "status": "proposed"},
            {"id": "sc-1", "status": "dispatched", "branch": "fix/x", "commitSha": "abc"},
            {"id": "sc-1", "status": "merged", "prNumber": 42},
            {"id": "sc-2", "status": "proposed"},  # never reached a commit — dropped
        ]
        folded = mal.fold_candidates(records)
        self.assertEqual(len(folded), 1)
        c = folded[0]
        self.assertEqual(
            (c["status"], c["branch"], c["commitSha"], c["prNumber"]),
            ("merged", "fix/x", "abc", 42),
        )


if __name__ == "__main__":
    unittest.main()
