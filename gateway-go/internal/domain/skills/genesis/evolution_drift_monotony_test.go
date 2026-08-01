package genesis

import "testing"

// The monotony signal exists to catch DIVERSITY COLLAPSE — a loop that stopped
// considering anything but one lever. It froze L2 on 2026-07-31 against a loop
// that was rotating all three artifacts correctly, because it counted only
// adoptions and could not see the skip cycles in between. Separate tests: one
// loop over cases would stop at the first failure and hide the rest.

// rev builds one ledger row. Empty action = a cycle that adopted nothing.
func rev(action, artifact string) MetaRevisionRecord {
	return MetaRevisionRecord{Action: action, Artifact: artifact}
}

func TestMonotonyIgnoresHealthyRotationWithSkips(t *testing.T) {
	// The real 2026-07 pattern, newest first: producer adopts every third day
	// while evaluator and genesis look and correctly find nothing to do.
	revs := []MetaRevisionRecord{
		rev("", "skill-judge-system-prompt.md"),
		rev("auto_adopted", "evolve-system-prompt.md"),
		rev("", "genesis-system-prompt.md"),
		rev("", "skill-judge-system-prompt.md"),
		rev("adopted", "evolve-system-prompt.md"),
		rev("", "genesis-system-prompt.md"),
		rev("", "skill-judge-system-prompt.md"),
		rev("auto_adopted", "evolve-system-prompt.md"),
	}
	if _, _, streak := driftMetaCounts(revs); streak >= driftMonotonyStreak {
		t.Errorf("a rotation whose other lanes skipped is not collapse, got streak=%d", streak)
	}
}

func TestMonotonyStillFiresOnGenuineCollapse(t *testing.T) {
	// Nothing but one artifact in the window: the loop really did stop looking
	// anywhere else. This must still freeze.
	revs := []MetaRevisionRecord{
		rev("auto_adopted", "evolve-system-prompt.md"),
		rev("", "evolve-system-prompt.md"),
		rev("auto_adopted", "evolve-system-prompt.md"),
		rev("auto_adopted", "evolve-system-prompt.md"),
	}
	if _, _, streak := driftMetaCounts(revs); streak < driftMonotonyStreak {
		t.Errorf("single-artifact window is collapse, got streak=%d", streak)
	}
}

func TestMonotonyStreakSurvivesOwnProposalTrail(t *testing.T) {
	// The proposal record that precedes each adoption is the streak artifact's
	// own trail — it must not break the streak it belongs to.
	revs := []MetaRevisionRecord{
		rev("adopted", "evolve-system-prompt.md"),
		rev("", "evolve-system-prompt.md"),
		rev("adopted", "evolve-system-prompt.md"),
		rev("", "evolve-system-prompt.md"),
		rev("adopted", "evolve-system-prompt.md"),
	}
	if _, _, streak := driftMetaCounts(revs); streak < driftMonotonyStreak {
		t.Errorf("own proposal trail must not break the streak, got streak=%d", streak)
	}
}

func TestMonotonyStreakBrokenByRevert(t *testing.T) {
	// Pre-existing contract: a revert interrupts the unbroken-adoption reading.
	revs := []MetaRevisionRecord{
		rev("adopted", "evolve-system-prompt.md"),
		rev("auto_reverted", "evolve-system-prompt.md"),
		rev("adopted", "evolve-system-prompt.md"),
		rev("adopted", "evolve-system-prompt.md"),
	}
	if _, _, streak := driftMetaCounts(revs); streak >= driftMonotonyStreak {
		t.Errorf("a revert must interrupt the streak, got streak=%d", streak)
	}
}
