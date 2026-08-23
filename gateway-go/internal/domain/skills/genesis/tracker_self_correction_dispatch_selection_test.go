package genesis

import (
	"fmt"
	"os"
	"strings"
	"testing"

	rsilifecycle "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/lifecycle"
)

func minerTestContract() *rsilifecycle.ImpactContract {
	return &rsilifecycle.ImpactContract{
		Metric: "health.finding_present:x", Direction: "decrease",
		Baseline: 1, Target: 0, MinSamples: 1,
	}
}

func TestNextSelfCorrectionDispatchCandidateSearchesBeyondRecentViewLimit(t *testing.T) {
	tracker := newTestTracker(t)
	appendFunnel(t, tracker.selfCorrectionPath, SelfCorrectionCandidateRecord{
		ID: "old-accepted", Scope: "code", Status: SelfCorrectionStatusAccepted,
		Source: "tool-quality:x", CreatedAt: 1, ImpactContract: minerTestContract(),
	})
	for i := range 501 {
		appendFunnel(t, tracker.selfCorrectionPath, SelfCorrectionCandidateRecord{
			ID: fmt.Sprintf("new-proposed-%03d", i), Scope: "code",
			Status: SelfCorrectionStatusProposed, Source: "health-finding:x", CreatedAt: int64(i + 2),
			ImpactContract: minerTestContract(),
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
		Source: "health-finding:x", CreatedAt: 1, ImpactContract: minerTestContract(),
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
		ImpactContract: minerTestContract(),
	}
	if !SelfCorrectionDispatchEligible(base) {
		t.Fatal("safe graduated candidate should dispatch")
	}
	noContract := base
	noContract.ImpactContract = nil
	if SelfCorrectionDispatchEligible(noContract) {
		t.Fatal("miner candidate without an impact contract must not dispatch")
	}
	tests := []SelfCorrectionCandidateRecord{
		{ID: "wrong-scope", Scope: "skill", Status: SelfCorrectionStatusProposed, Source: "health-finding:x"},
		{ID: "rejected", Scope: "code", Status: SelfCorrectionStatusRejected, Source: "health-finding:x"},
		{ID: "active", Scope: "code", Status: SelfCorrectionStatusAccepted, Source: "health-finding:x", DispatchPhase: selfCorrectionDispatchStarted},
		{ID: "staged", Scope: "code", Status: SelfCorrectionStatusProposed, Source: "novel-miner:x"},
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

func TestNextSelfCorrectionDispatchCandidatePrioritizesNegativeImpactFollowUp(t *testing.T) {
	tracker := newTestTracker(t)
	appendFunnel(t, tracker.selfCorrectionPath, dispatchImpactHistory(
		"no-effect-old", "health-finding:no-effect", selfCorrectionImpactNoEffect, 100,
	))
	appendFunnel(t, tracker.selfCorrectionPath, dispatchImpactHistory(
		"regressed-old", "health-finding:regressed", selfCorrectionImpactRegressed, 100,
	))
	for _, record := range []SelfCorrectionCandidateRecord{
		{
			ID: "newest-normal", Scope: "code", Status: SelfCorrectionStatusProposed,
			Source: "health-finding:normal", ProposedChange: "fix normal", CreatedAt: 300,
			ImpactContract: minerTestContract(),
		},
		{
			ID: "no-effect-follow-up", Scope: "code", Status: SelfCorrectionStatusProposed,
			Source: "health-finding:no-effect", ProposedChange: "fix no effect", CreatedAt: 200,
			ImpactContract: minerTestContract(),
		},
		{
			ID: "regressed-follow-up", Scope: "code", Status: SelfCorrectionStatusProposed,
			Source: "health-finding:regressed", ProposedChange: "fix regression", CreatedAt: 100,
			ImpactContract: minerTestContract(),
		},
	} {
		appendFunnel(t, tracker.selfCorrectionPath, record)
	}

	got, ok, err := tracker.NextSelfCorrectionDispatchCandidate(nil)
	if err != nil || !ok || got.ID != "regressed-follow-up" {
		t.Fatalf("selected = %+v, ok=%v, err=%v", got, ok, err)
	}
	got, ok, err = tracker.NextSelfCorrectionDispatchCandidate([]string{"regressed-follow-up"})
	if err != nil || !ok || got.ID != "no-effect-follow-up" {
		t.Fatalf("selected after regression exclusion = %+v, ok=%v, err=%v", got, ok, err)
	}
}

func TestNextSelfCorrectionDispatchCandidateRequiresStrategyShiftAfterRepeatedNegativeImpact(t *testing.T) {
	tracker := newTestTracker(t)
	source := "health-finding:repeat"
	first := dispatchImpactHistory("negative-1", source, selfCorrectionImpactNoEffect, 100)
	first.ProposedChange = "Narrow the cache key"
	second := dispatchImpactHistory("negative-2", source, selfCorrectionImpactRegressed, 200)
	second.ProposedChange = "Add a guard"
	appendFunnel(t, tracker.selfCorrectionPath, first)
	appendFunnel(t, tracker.selfCorrectionPath, second)
	appendFunnel(t, tracker.selfCorrectionPath, SelfCorrectionCandidateRecord{
		ID: "repeated-strategy", Scope: "code", Status: SelfCorrectionStatusProposed,
		Source: source, ProposedChange: "  ADD   A GUARD ", CreatedAt: 400,
		ImpactContract: minerTestContract(),
	})
	appendFunnel(t, tracker.selfCorrectionPath, SelfCorrectionCandidateRecord{
		ID: "missing-strategy", Scope: "code", Status: SelfCorrectionStatusProposed,
		Source: source, CreatedAt: 500, ImpactContract: minerTestContract(),
	})
	appendFunnel(t, tracker.selfCorrectionPath, SelfCorrectionCandidateRecord{
		ID: "fallback", Scope: "code", Status: SelfCorrectionStatusProposed,
		Source: "health-finding:fallback", ProposedChange: "fix fallback", CreatedAt: 300,
		ImpactContract: minerTestContract(),
	})

	got, ok, err := tracker.NextSelfCorrectionDispatchCandidate(nil)
	if err != nil || !ok || got.ID != "fallback" {
		t.Fatalf("repeated strategy selected = %+v, ok=%v, err=%v", got, ok, err)
	}

	appendFunnel(t, tracker.selfCorrectionPath, SelfCorrectionCandidateRecord{
		ID: "shifted-strategy", Scope: "code", Status: SelfCorrectionStatusProposed,
		Source: source, ProposedChange: "Replace the invalidation algorithm", CreatedAt: 600,
		ImpactContract: minerTestContract(),
	})
	got, ok, err = tracker.NextSelfCorrectionDispatchCandidate(nil)
	if err != nil || !ok || got.ID != "shifted-strategy" {
		t.Fatalf("shifted strategy not selected = %+v, ok=%v, err=%v", got, ok, err)
	}

	candidates, err := tracker.allSelfCorrectionCandidates()
	if err != nil {
		t.Fatal(err)
	}
	tally := tracker.tallyL4Candidates(candidates)
	if tally.strategyBlocked != 2 || tally.dispatchable != 2 {
		t.Fatalf("L4 impact policy tally = %+v, want two blocked and two dispatchable", tally)
	}
}

func TestLatestVerifiedImpactClearsNegativePriorityAndStrategyGate(t *testing.T) {
	tracker := newTestTracker(t)
	source := "health-finding:recovered"
	for _, record := range []SelfCorrectionCandidateRecord{
		dispatchImpactHistory("negative-1", source, selfCorrectionImpactNoEffect, 100),
		dispatchImpactHistory("negative-2", source, selfCorrectionImpactRegressed, 200),
		dispatchImpactHistory("verified", source, selfCorrectionImpactVerified, 300),
		{
			ID: "recovered-follow-up", Scope: "code", Status: SelfCorrectionStatusProposed,
			Source: source, ProposedChange: "same strategy", CreatedAt: 400,
			ImpactContract: minerTestContract(),
		},
		{
			ID: "newer-normal", Scope: "code", Status: SelfCorrectionStatusProposed,
			Source: "health-finding:normal", ProposedChange: "normal strategy", CreatedAt: 500,
			ImpactContract: minerTestContract(),
		},
	} {
		appendFunnel(t, tracker.selfCorrectionPath, record)
	}

	got, ok, err := tracker.NextSelfCorrectionDispatchCandidate(nil)
	if err != nil || !ok || got.ID != "newer-normal" {
		t.Fatalf("verified source retained stale negative priority: %+v, ok=%v, err=%v", got, ok, err)
	}
}

func TestSelfCorrectionDispatchWithheldAfterRepeatedFailures(t *testing.T) {
	tracker := newTestTracker(t)
	// A candidate an unattended coding session keeps failing to land (a doctrine-
	// conflicting or too-large fix). CanDispatch treats each "failed" phase as
	// re-eligible, so without the failure cap it would consume a coding session on
	// every tick. Once the count reaches the cap it is withheld entirely.
	appendFunnel(t, tracker.selfCorrectionPath, SelfCorrectionCandidateRecord{
		ID: "unwinnable", Scope: "code", Status: SelfCorrectionStatusAccepted,
		Source: "health-finding:x", CreatedAt: 200, ImpactContract: minerTestContract(),
	})
	appendSelfCorrectionDispatchFailures(t, tracker.selfCorrectionPath, "unwinnable", maxSelfCorrectionDispatchFailures)
	// A sibling still under the cap stays retryable.
	appendFunnel(t, tracker.selfCorrectionPath, SelfCorrectionCandidateRecord{
		ID: "retryable", Scope: "code", Status: SelfCorrectionStatusAccepted,
		Source: "health-finding:y", CreatedAt: 100, ImpactContract: minerTestContract(),
	})
	appendSelfCorrectionDispatchFailures(t, tracker.selfCorrectionPath, "retryable", maxSelfCorrectionDispatchFailures-1)

	got, ok, err := tracker.NextSelfCorrectionDispatchCandidate(nil)
	if err != nil || !ok || got.ID != "retryable" {
		t.Fatalf("selected = %+v, ok=%v, err=%v; want retryable (unwinnable withheld)", got, ok, err)
	}
	if got.DispatchFailures != maxSelfCorrectionDispatchFailures-1 {
		t.Fatalf("retryable DispatchFailures = %d, want %d", got.DispatchFailures, maxSelfCorrectionDispatchFailures-1)
	}
	// Excluding the retryable sibling leaves nothing dispatchable: the repeatedly
	// failing candidate is fully withheld, not merely deprioritized, so the L4
	// loop stops burning coding sessions on it.
	got, ok, err = tracker.NextSelfCorrectionDispatchCandidate([]string{"retryable"})
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("candidate still dispatchable after %d failures: %+v", maxSelfCorrectionDispatchFailures, got)
	}
}

func TestSelfCorrectionDispatchFailureCountIsIdempotentAndRestartSafe(t *testing.T) {
	tracker := newTestTracker(t)
	appendFunnel(t, tracker.selfCorrectionPath, SelfCorrectionCandidateRecord{
		ID: "cand", Scope: "code", Status: SelfCorrectionStatusAccepted,
		Source: "health-finding:x", CreatedAt: 1, ImpactContract: minerTestContract(),
	})
	appendSelfCorrectionDispatchFailures(t, tracker.selfCorrectionPath, "cand", maxSelfCorrectionDispatchFailures)
	// Replaying the latest attempt's terminal "failed" row (a retried ledger
	// write) must not double-count: samePhase guards the increment.
	appendFunnel(t, tracker.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: selfCorrectionTypeDispatch, ID: "cand",
		AttemptID:     fmt.Sprintf("cand-attempt-%d", maxSelfCorrectionDispatchFailures-1),
		DispatchPhase: selfCorrectionDispatchFailed,
	})

	assertFailures := func(tr *Tracker, label string) {
		t.Helper()
		candidates, err := tr.allSelfCorrectionCandidates()
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		found := false
		for _, record := range candidates {
			if record.ID != "cand" {
				continue
			}
			found = true
			if record.DispatchFailures != maxSelfCorrectionDispatchFailures {
				t.Fatalf("%s: DispatchFailures = %d, want %d (duplicate terminal row must not double-count)",
					label, record.DispatchFailures, maxSelfCorrectionDispatchFailures)
			}
		}
		if !found {
			t.Fatalf("%s: candidate cand not found", label)
		}
	}
	assertFailures(tracker, "in-memory fold")

	// A restart rebuilds the identical count purely from the append-only ledger:
	// the failure count is fold-derived, never carried in process state.
	restarted := &Tracker{logger: tracker.logger, selfCorrectionPath: tracker.selfCorrectionPath}
	assertFailures(restarted, "after restart")
}

// appendSelfCorrectionDispatchFailures appends failures distinct started→failed
// dispatch attempts for id, mirroring how coding-dispatch.sh records a candidate
// that repeatedly fails to land.
func appendSelfCorrectionDispatchFailures(t *testing.T, path, id string, failures int) {
	t.Helper()
	for i := range failures {
		attempt := fmt.Sprintf("%s-attempt-%d", id, i)
		appendFunnel(t, path, SelfCorrectionCandidateRecord{
			Type: selfCorrectionTypeDispatch, ID: id, AttemptID: attempt,
			DispatchPhase: selfCorrectionDispatchStarted,
		})
		appendFunnel(t, path, SelfCorrectionCandidateRecord{
			Type: selfCorrectionTypeDispatch, ID: id, AttemptID: attempt,
			DispatchPhase: selfCorrectionDispatchFailed,
		})
	}
}

func dispatchImpactHistory(id, source, status string, checkedAt int64) SelfCorrectionCandidateRecord {
	return SelfCorrectionCandidateRecord{
		ID: id, Scope: "code", Status: SelfCorrectionStatusApplied,
		Source: source, ProposedChange: "same strategy", CreatedAt: checkedAt - 1,
		DispatchPhase: selfCorrectionDispatchWatchPassed,
		ImpactResult:  &rsilifecycle.ImpactResult{Status: status, CheckedAt: checkedAt},
	}
}
