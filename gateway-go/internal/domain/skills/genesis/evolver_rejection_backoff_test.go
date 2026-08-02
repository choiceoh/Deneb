package genesis

import (
	"log/slog"
	"testing"
	"time"
)

// The thrash cooldown counts COMPLETED evolves, so a skill that only ever
// rejects never trips it: 2026-08-02 the nightly sweep burned 14 attempts on
// three skills in 2.5 hours, every one rejected on an immovable held-out score
// or a replay regression. The backoff is the missing guard — same coin, same
// slot, stop.

func backoffTracker(t *testing.T) *Tracker {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func rejectN(t *testing.T, tr *Tracker, skill, reason string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := tr.logEvolveRejectedWithProvenance(skill, reason, HarnessEditAudit{}, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBackoffCountsRecentSubstantiveRejections(t *testing.T) {
	tr := backoffTracker(t)
	rejectN(t, tr, "contract-review", "held-out selection rejected: no improvement", 3)
	if got := tr.RecentEvolveRejections("contract-review", 24*time.Hour); got != 3 {
		t.Errorf("want 3, got %d", got)
	}
	if got := tr.RecentEvolveRejections("other-skill", 24*time.Hour); got != 0 {
		t.Errorf("another skill's streak must not leak, got %d", got)
	}
}

func TestBackoffIgnoresInfrastructureRejections(t *testing.T) {
	// A provider blip must not lock a healthy skill out of its own repair lane.
	tr := backoffTracker(t)
	rejectN(t, tr, "contract-review", "judge error", 5)
	if got := tr.RecentEvolveRejections("contract-review", 24*time.Hour); got != 0 {
		t.Errorf("infra rejections are outages, not verdicts: got %d", got)
	}
}

func TestEvolutionSuppressedAfterRejectionStreak(t *testing.T) {
	tr := backoffTracker(t)
	e := &Evolver{tracker: tr}
	rejectN(t, tr, "topsolar-db", "behavioral replay regressed", evolutionRejectionBackoffMin)
	suppressed, reason := e.evolutionSuppressed("topsolar-db", time.Now())
	if !suppressed {
		t.Fatal("a full rejection streak must pause unattended attempts")
	}
	if reason == "" {
		t.Error("the pause must say why")
	}
}

func TestEvolutionNotSuppressedBelowStreak(t *testing.T) {
	tr := backoffTracker(t)
	e := &Evolver{tracker: tr}
	rejectN(t, tr, "topsolar-db", "behavioral replay regressed", evolutionRejectionBackoffMin-1)
	if suppressed, reason := e.evolutionSuppressed("topsolar-db", time.Now()); suppressed {
		t.Errorf("below the streak the lane must keep trying: %s", reason)
	}
}
