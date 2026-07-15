package genesis

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestNextSelfCorrectionDispatchCandidateSearchesBeyondRecentViewLimit(t *testing.T) {
	tracker := newTestTracker(t)
	appendFunnel(t, tracker.selfCorrectionPath, SelfCorrectionCandidateRecord{
		ID: "old-accepted", Scope: "code", Status: SelfCorrectionStatusAccepted,
		Source: "tool-quality:x", CreatedAt: 1,
	})
	for i := range 501 {
		appendFunnel(t, tracker.selfCorrectionPath, SelfCorrectionCandidateRecord{
			ID: fmt.Sprintf("new-proposed-%03d", i), Scope: "code",
			Status: SelfCorrectionStatusProposed, Source: "health-finding:x", CreatedAt: int64(i + 2),
		})
	}

	got, ok, err := tracker.NextSelfCorrectionDispatchCandidate(nil)
	if err != nil || !ok || got.ID != "old-accepted" {
		t.Fatalf("selected = %+v, ok=%v, err=%v", got, ok, err)
	}
	got, ok, err = tracker.NextSelfCorrectionDispatchCandidate([]string{"old-accepted"})
	if err != nil || !ok || got.ID != "new-proposed-500" {
		t.Fatalf("selected with exclusion = %+v, ok=%v, err=%v", got, ok, err)
	}
}

func TestSelfCorrectionSafetyDecisionsFailClosedOnCorruptLedger(t *testing.T) {
	tracker := newTestTracker(t)
	appendFunnel(t, tracker.selfCorrectionPath, SelfCorrectionCandidateRecord{
		ID: "safe", Scope: "code", Status: SelfCorrectionStatusAccepted,
		Source: "health-finding:x", CreatedAt: 1,
	})
	f, err := os.OpenFile(tracker.selfCorrectionPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{broken\n"); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	assertCorrupt := func(name string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "malformed=1") {
			t.Fatalf("%s error = %v, want ledger corruption", name, err)
		}
	}

	_, err = tracker.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID: "safe", Status: SelfCorrectionStatusRejected,
	})
	assertCorrupt("review", err)
	_, err = tracker.RecordSelfCorrectionDispatch(SelfCorrectionCandidateRecord{
		ID: "safe", DispatchPhase: selfCorrectionDispatchStarted, AttemptID: "attempt-1",
	})
	assertCorrupt("dispatch", err)

	got, ok, err := tracker.NextSelfCorrectionDispatchCandidate(nil)
	assertCorrupt("selection", err)
	if ok || got.ID != "" {
		t.Fatalf("selected = %+v, ok=%v, err=%v; want fail-closed corruption error", got, ok, err)
	}
}

func TestSelfCorrectionDispatchEligibleCentralizesReviewDeliveryAndSurfacePolicy(t *testing.T) {
	base := SelfCorrectionCandidateRecord{
		ID: "safe", Scope: "code", Status: SelfCorrectionStatusProposed,
		Source: "health-finding:x", ProposedChange: "narrow a gateway contract",
	}
	if !selfCorrectionDispatchEligible(base) {
		t.Fatal("safe graduated candidate should dispatch")
	}
	tests := []SelfCorrectionCandidateRecord{
		{ID: "wrong-scope", Scope: "skill", Status: SelfCorrectionStatusProposed, Source: "health-finding:x"},
		{ID: "rejected", Scope: "code", Status: SelfCorrectionStatusRejected, Source: "health-finding:x"},
		{ID: "active", Scope: "code", Status: SelfCorrectionStatusAccepted, Source: "health-finding:x", DispatchPhase: selfCorrectionDispatchStarted},
		{ID: "staged", Scope: "code", Status: SelfCorrectionStatusProposed, Source: "novel-miner:x"},
		{ID: "forbidden-prose", Scope: "code", Status: SelfCorrectionStatusAccepted, Source: "health-finding:x", ProposedChange: "relax validation_engine.go"},
	}
	for _, record := range tests {
		if selfCorrectionDispatchEligible(record) {
			t.Fatalf("candidate unexpectedly eligible: %+v", record)
		}
	}
	retry := base
	retry.DispatchPhase = selfCorrectionDispatchFailed
	if !selfCorrectionDispatchEligible(retry) {
		t.Fatal("failed candidate should be retryable before local residue checks")
	}
}
