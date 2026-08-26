package genesis

import (
	"errors"
	"testing"
)

// A review aimed at an already-settled candidate must be distinguishable from a
// real failure. The heartbeat contract aborts the whole turn on a tool error, so
// one stale id carried in from memory used to cost every other candidate in that
// turn its review — 18 such attempts over 7 days, one id retried five times.
func TestRecordSelfCorrectionReviewMarksTerminalTransitions(t *testing.T) {
	tr := newTestTracker(t)

	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		ID: "sc-terminal", SkillName: "s", Scope: "skill", Evidence: "e",
		Title: "테스트 후보", ProposedChange: "무언가 고친다",
	}); err != nil {
		t.Fatalf("seed candidate: %v", err)
	}
	if _, err := tr.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID: "sc-terminal", Status: SelfCorrectionStatusApplied, ReviewNote: "고쳤다",
	}); err != nil {
		t.Fatalf("first review: %v", err)
	}

	_, err := tr.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID: "sc-terminal", Status: SelfCorrectionStatusAccepted, ReviewNote: "다시",
	})
	if err == nil {
		t.Fatal("applied -> accepted must not be recorded")
	}
	if !errors.Is(err, ErrSelfCorrectionTransition) {
		t.Fatalf("error must carry the sentinel so callers can soften it: %v", err)
	}
}

// An id that never existed stays a hard error — reviewing something imaginary is
// a different problem from reviewing something already settled.
func TestRecordSelfCorrectionReviewUnknownIDIsNotATransitionError(t *testing.T) {
	tr := newTestTracker(t)
	_, err := tr.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID: "sc-never", Status: SelfCorrectionStatusAccepted, ReviewNote: "n",
	})
	if err == nil {
		t.Fatal("unknown id must error")
	}
	if errors.Is(err, ErrSelfCorrectionTransition) {
		t.Fatal("unknown id must NOT be softened as an already-settled candidate")
	}
}
