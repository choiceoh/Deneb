package genesis

import (
	"testing"
	"time"
)

// "Quiet" only means the fix worked if the error stream kept running while this
// one signature went silent. Without that control every one of the 8 "verified"
// runtime impacts on 2026-08-01 was just hours-since-last-fire (52–68h) — a
// number a quiet weekend produces as readily as a repair.

func stateWith(sigs map[string]int64) *runtimeErrorState {
	st := &runtimeErrorState{Sigs: map[string]*runtimeErrorSigEntry{}}
	for sig, lastAt := range sigs {
		st.Sigs[sig] = &runtimeErrorSigEntry{LastAt: lastAt}
	}
	return st
}

func TestSiblingControlCountsOnlyOtherSignaturesThatFired(t *testing.T) {
	now := time.Now()
	since := now.Add(-48 * time.Hour)
	st := stateWith(map[string]int64{
		"fixed-sig": now.Add(-72 * time.Hour).UnixMilli(), // the one under test, quiet
		"noisy-sig": now.Add(-2 * time.Hour).UnixMilli(),  // stream alive
		"other-sig": now.Add(-6 * time.Hour).UnixMilli(),  // stream alive
		"stale-sig": now.Add(-96 * time.Hour).UnixMilli(), // older than the window
	})
	if got := activeSiblingSignatures(st, "fixed-sig", since); got != 2 {
		t.Errorf("want 2 live siblings, got %d", got)
	}
}

func TestSiblingControlExcludesTheSignatureUnderTest(t *testing.T) {
	now := time.Now()
	// Only the measured signature is live — it cannot be its own control.
	st := stateWith(map[string]int64{"fixed-sig": now.UnixMilli()})
	if got := activeSiblingSignatures(st, "fixed-sig", now.Add(-48*time.Hour)); got != 0 {
		t.Errorf("a signature must not control itself, got %d", got)
	}
}

func TestSiblingControlReportsDeadStream(t *testing.T) {
	// Everything went quiet: the gateway or the exercised path went idle, so a
	// quiet period proves nothing about the fix. Caller must leave it pending.
	now := time.Now()
	st := stateWith(map[string]int64{
		"fixed-sig": now.Add(-72 * time.Hour).UnixMilli(),
		"other-sig": now.Add(-80 * time.Hour).UnixMilli(),
	})
	if got := activeSiblingSignatures(st, "fixed-sig", now.Add(-48*time.Hour)); got != 0 {
		t.Errorf("a fully quiet stream offers no attribution, got %d", got)
	}
}

func TestSiblingControlHandlesMissingState(t *testing.T) {
	if got := activeSiblingSignatures(nil, "x", time.Now()); got != 0 {
		t.Errorf("nil state must not credit anything, got %d", got)
	}
}
