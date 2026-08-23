package genesis

import (
	"context"
	"errors"
	"testing"
)

// The watch fires exactly once per NEW proposed candidate: the standing
// backlog baselines silently, a fresh candidate fires once (not again on
// repeat runs), a reviewed candidate leaves the sweep entirely, and a
// delivery failure stays pending until the next run succeeds.
func TestSelfCorrectionWatchFiresOnceOnNewCandidate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr := newTestTracker(t)
	task := &SelfCorrectionWatchTask{Tracker: tr}

	// Standing backlog: first run baselines without posting.
	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "skill", SkillName: "sk", Title: "backlog finding", Source: "watch-test",
	}); err != nil {
		t.Fatal(err)
	}
	var fired []string
	task.OnNew = func(rec SelfCorrectionCandidateRecord) error {
		fired = append(fired, rec.Title)
		return nil
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fired) != 0 {
		t.Fatalf("first run must baseline silently, fired=%v", fired)
	}

	// A NEW proposed candidate fires exactly once; a repeat run stays silent.
	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "skill", SkillName: "sk", Title: "fresh finding", Source: "watch-test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fired) != 1 || fired[0] != "fresh finding" {
		t.Fatalf("expected one fresh card, fired=%v", fired)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fired) != 1 {
		t.Fatalf("repeat run must stay silent, fired=%v", fired)
	}

	// A candidate reviewed before the sweep never surfaces — the card is for
	// decisions, not history.
	reviewed, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "skill", SkillName: "sk", Title: "already decided", Source: "watch-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID: reviewed.ID, Status: SelfCorrectionStatusRejected, Reviewer: "test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fired) != 1 {
		t.Fatalf("reviewed candidate must not fire, fired=%v", fired)
	}

	// A delivery failure leaves the candidate pending and retries next run.
	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "skill", SkillName: "sk", Title: "flaky delivery", Source: "watch-test",
	}); err != nil {
		t.Fatal(err)
	}
	deliveryFails := true
	task.OnNew = func(rec SelfCorrectionCandidateRecord) error {
		if rec.Title == "flaky delivery" && deliveryFails {
			deliveryFails = false
			return errors.New("feed down")
		}
		fired = append(fired, rec.Title)
		return nil
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fired) != 1 {
		t.Fatalf("failed delivery must not fire, fired=%v", fired)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fired) != 2 || fired[1] != "flaky delivery" {
		t.Fatalf("retry after failure must fire once, fired=%v", fired)
	}
}
