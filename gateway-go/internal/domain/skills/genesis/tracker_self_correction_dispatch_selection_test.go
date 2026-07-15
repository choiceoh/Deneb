package genesis

import (
	"fmt"
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

func TestSelfCorrectionDispatchEligibleCentralizesReviewDeliveryAndSurfacePolicy(t *testing.T) {
	base := SelfCorrectionCandidateRecord{
		ID: "safe", Scope: "code", Status: SelfCorrectionStatusProposed,
		Source: "health-finding:x", ProposedChange: "narrow a gateway contract",
	}
	if !SelfCorrectionDispatchEligible(base) {
		t.Fatal("safe graduated candidate should dispatch")
	}
	tests := []SelfCorrectionCandidateRecord{
		{ID: "wrong-scope", Scope: "skill", Status: SelfCorrectionStatusProposed, Source: "health-finding:x"},
		{ID: "rejected", Scope: "code", Status: SelfCorrectionStatusRejected, Source: "health-finding:x"},
		{ID: "active", Scope: "code", Status: SelfCorrectionStatusAccepted, Source: "health-finding:x", DispatchPhase: selfCorrectionDispatchStarted},
		{ID: "staged", Scope: "code", Status: SelfCorrectionStatusProposed, Source: "runtime-error:x"},
		{ID: "forbidden-prose", Scope: "code", Status: SelfCorrectionStatusAccepted, Source: "health-finding:x", ProposedChange: "relax validation_engine.go"},
	}
	for _, record := range tests {
		if SelfCorrectionDispatchEligible(record) {
			t.Fatalf("candidate unexpectedly eligible: %+v", record)
		}
	}
	retry := base
	retry.DispatchPhase = selfCorrectionDispatchFailed
	if !SelfCorrectionDispatchEligible(retry) {
		t.Fatal("failed candidate should be retryable before local residue checks")
	}
}
