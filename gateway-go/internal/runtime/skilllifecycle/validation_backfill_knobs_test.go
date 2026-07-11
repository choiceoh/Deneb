package skilllifecycle

import (
	"testing"
	"time"
)

// Archive-sweep knobs: env overrides with defaults preserved and junk rejected.
func TestBackfillKnobs(t *testing.T) {
	t.Setenv("DENEB_VALIDATION_BACKFILL_WINDOW_DAYS", "")
	if backfillWindow() != 7*24*time.Hour {
		t.Fatalf("default window = %v", backfillWindow())
	}
	t.Setenv("DENEB_VALIDATION_BACKFILL_WINDOW_DAYS", "90")
	if backfillWindow() != 90*24*time.Hour {
		t.Fatalf("archive window = %v", backfillWindow())
	}
	t.Setenv("DENEB_VALIDATION_BACKFILL_SESSIONS_PER_SKILL", "10")
	if backfillSessionsPerSkill() != 10 {
		t.Fatalf("sessions = %d", backfillSessionsPerSkill())
	}
	t.Setenv("DENEB_VALIDATION_BACKFILL_TARGET_CASES", "junk")
	if backfillTargetUniqueCases() != validationBackfillTargetUniqueCases {
		t.Fatalf("junk override accepted: %d", backfillTargetUniqueCases())
	}
}
