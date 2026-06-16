package genesis

import (
	"testing"
	"time"
)

func TestLivenessHeartbeat(t *testing.T) {
	tr := newTestTracker(t)

	// A clean snapshot before any activity.
	if snap := tr.LivenessSnapshot(); snap.LastReviewAt != 0 || snap.LastEvolveAt != 0 {
		t.Fatalf("fresh tracker should have empty liveness, got %+v", snap)
	}

	tr.RecordEvolutionActivity(SkillActivityReview, true, "")
	snap := tr.LivenessSnapshot()
	if snap.LastReviewAt == 0 || !snap.LastReviewOK {
		t.Fatalf("review heartbeat not recorded: %+v", snap)
	}
	tr.RecordEvolutionActivity(SkillActivityReviewAttempt, true, "")
	tr.RecordEvolutionActivity(SkillActivityReviewSkipped, true, "")
	tr.RecordEvolutionActivity(SkillActivityValidationRejected, true, "")
	snap = tr.LivenessSnapshot()
	if snap.ReviewAttempts != 1 || snap.ReviewSkips != 1 || snap.ValidationRejections != 1 {
		t.Fatalf("attempt/skip/rejection counters not recorded: %+v", snap)
	}

	tr.RecordEvolutionActivity(SkillActivityEvolve, false, "boom")
	snap = tr.LivenessSnapshot()
	if snap.LastEvolveAt == 0 {
		t.Fatalf("evolve heartbeat not recorded: %+v", snap)
	}
	if snap.LastError != "boom" || snap.LastErrorAt == 0 {
		t.Fatalf("failure detail not recorded: %+v", snap)
	}
	// Review timestamp must survive the later evolve update.
	if snap.LastReviewAt == 0 {
		t.Fatalf("review timestamp was clobbered: %+v", snap)
	}
}

func TestEvolveEventTrigger_FiresAtThresholdAndResets(t *testing.T) {
	tr := newTestTracker(t)
	fired := make(chan struct{}, 4)
	tr.SetEvolveTrigger(func() { fired <- struct{}{} }, 2, 0) // threshold 2, no minGap

	// First genesis: counter 1, no fire.
	if err := tr.LogGenesis("skill-one", "session", "k", "coding", "d1"); err != nil {
		t.Fatalf("LogGenesis: %v", err)
	}
	select {
	case <-fired:
		t.Fatal("trigger fired before threshold")
	case <-time.After(150 * time.Millisecond):
	}

	// Second genesis: counter reaches 2, fires.
	if err := tr.LogGenesis("skill-two", "session", "k", "coding", "d2"); err != nil {
		t.Fatalf("LogGenesis: %v", err)
	}
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("trigger did not fire at threshold")
	}

	// Counter must reset after firing.
	if snap := tr.LivenessSnapshot(); snap.GenesisSinceEvolve != 0 {
		t.Fatalf("counter not reset after fire: %d", snap.GenesisSinceEvolve)
	}
}

func TestEvolveEventTrigger_MinGapSuppresses(t *testing.T) {
	tr := newTestTracker(t)
	// Record a very recent evolve so minGap blocks the next fire.
	tr.RecordEvolutionActivity(SkillActivityEvolve, true, "")

	fired := make(chan struct{}, 2)
	tr.SetEvolveTrigger(func() { fired <- struct{}{} }, 1, time.Hour) // threshold 1, 1h gap

	if err := tr.LogGenesis("skill-x", "session", "k", "coding", "d"); err != nil {
		t.Fatalf("LogGenesis: %v", err)
	}
	select {
	case <-fired:
		t.Fatal("trigger fired despite minGap not elapsed")
	case <-time.After(200 * time.Millisecond):
	}
	// Counter keeps accumulating while suppressed (will fire once gap elapses).
	if snap := tr.LivenessSnapshot(); snap.GenesisSinceEvolve == 0 {
		t.Fatalf("counter should accumulate while gap suppresses, got 0")
	}
}
