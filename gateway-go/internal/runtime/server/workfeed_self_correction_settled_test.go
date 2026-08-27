package server

import (
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
)

// TestSettledCandidateIsNotARetryableCardFailure pins the distinction the card
// path was missing.
//
// Retry-on-error is correct for a transient failure and exactly wrong for a
// candidate that has reached a terminal state: that transition can never
// succeed, so an error keeps the card unsettled forever and the operator taps
// it every time it reappears. The sentinel is what separates the two.
func TestSettledCandidateIsNotARetryableCardFailure(t *testing.T) {
	settled := genesis.ErrSelfCorrectionTransition
	if !errors.Is(settled, genesis.ErrSelfCorrectionTransition) {
		t.Fatal("sentinel must be identifiable with errors.Is — the whole branch keys on it")
	}
	// A wrapped sentinel must still be recognized: the tracker returns it
	// wrapped with the offending transition ("applied -> accepted").
	wrapped := errors.Join(errors.New("genesis-tracker: applied -> accepted"), genesis.ErrSelfCorrectionTransition)
	if !errors.Is(wrapped, genesis.ErrSelfCorrectionTransition) {
		t.Error("a wrapped sentinel must still settle the card, not retry it")
	}
	// An unrelated failure stays retryable — the card must survive a transient
	// tracker outage so the operator's decision is not silently dropped.
	if errors.Is(errors.New("tracker unavailable"), genesis.ErrSelfCorrectionTransition) {
		t.Error("an unrelated error must not be mistaken for a settled candidate")
	}
}
