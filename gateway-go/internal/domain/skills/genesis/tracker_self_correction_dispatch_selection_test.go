package genesis

import "testing"

func TestSelectSelfCorrectionDispatchCandidatePrioritizesReviewThenRecency(t *testing.T) {
	records := []SelfCorrectionCandidateRecord{
		{ID: "new-proposed", Scope: "code", Status: SelfCorrectionStatusProposed, Source: "health-finding:x", CreatedAt: 30},
		{ID: "old-accepted", Scope: "code", Status: SelfCorrectionStatusAccepted, Source: "tool-quality:x", CreatedAt: 10},
		{ID: "new-accepted", Scope: "code", Status: SelfCorrectionStatusAccepted, Source: "self-harness:x", CreatedAt: 20},
	}
	got, ok := SelectSelfCorrectionDispatchCandidate(records, nil)
	if !ok || got.ID != "new-accepted" {
		t.Fatalf("selected = %+v, %v", got, ok)
	}
	got, ok = SelectSelfCorrectionDispatchCandidate(records, []string{"new-accepted"})
	if !ok || got.ID != "old-accepted" {
		t.Fatalf("selected with exclusion = %+v, %v", got, ok)
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
