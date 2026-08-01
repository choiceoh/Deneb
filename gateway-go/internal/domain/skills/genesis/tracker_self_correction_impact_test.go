package genesis

import (
	"log/slog"
	"strings"
	"testing"

	rsilifecycle "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/lifecycle"
)

func TestSelfCorrectionImpactPersistsIndependentUsefulnessVerdict(t *testing.T) {
	tracker := newTestTracker(t)
	candidate := recordImpactCandidate(t, tracker)
	watchPassImpactCandidate(t, tracker, candidate.ID, "attempt-1")

	rows, err := tracker.RecentSelfCorrectionCandidates("", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := selfCorrectionImpactStatus(rows[0]); got != selfCorrectionImpactPending {
		t.Fatalf("impact before observation = %q, want pending", got)
	}

	impact := SelfCorrectionCandidateRecord{
		ID:        candidate.ID,
		AttemptID: "attempt-1",
		ImpactResult: &rsilifecycle.ImpactResult{
			Observed: 80,
			Samples:  12,
			Note:     "p95 improved after deploy",
		},
	}
	if _, err := tracker.RecordSelfCorrectionDispatch(impact); err != nil {
		t.Fatal(err)
	}
	// Exact retries are idempotent and survive a fresh read-side tracker.
	if _, err := tracker.RecordSelfCorrectionDispatch(impact); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	restarted := &Tracker{logger: slog.Default(), selfCorrectionPath: tracker.selfCorrectionPath}
	rows, err = restarted.RecentSelfCorrectionCandidates("", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ImpactResult == nil {
		t.Fatalf("impact result did not restore: %+v", rows)
	}
	if got := selfCorrectionImpactStatus(rows[0]); got != selfCorrectionImpactVerified {
		t.Fatalf("restored impact = %q, want verified", got)
	}
	if rows[0].Status != SelfCorrectionStatusApplied {
		t.Fatalf("safety review status changed: %q", rows[0].Status)
	}

	conflict := impact
	conflict.ImpactResult = &rsilifecycle.ImpactResult{Observed: 95, Samples: 12}
	if _, err := tracker.RecordSelfCorrectionDispatch(conflict); err == nil || !strings.Contains(err.Error(), "already terminal") {
		t.Fatalf("conflicting terminal result error = %v", err)
	}
}

func TestSelfCorrectionImpactClassifiesTargetBaselineAndGuardrails(t *testing.T) {
	contract := rsilifecycle.ImpactContract{
		Metric:     "runtime.agent_ms.p95",
		Direction:  selfCorrectionImpactDirectionDecrease,
		Baseline:   100,
		Target:     80,
		MinSamples: 10,
		Guardrails: []string{"error_rate"},
	}
	tests := []struct {
		name   string
		result rsilifecycle.ImpactResult
		want   string
	}{
		{name: "verified", result: rsilifecycle.ImpactResult{Observed: 79, Samples: 10}, want: selfCorrectionImpactVerified},
		{name: "partial is no effect", result: rsilifecycle.ImpactResult{Observed: 90, Samples: 10}, want: selfCorrectionImpactNoEffect},
		{name: "flat is no effect", result: rsilifecycle.ImpactResult{Observed: 100, Samples: 10}, want: selfCorrectionImpactNoEffect},
		{name: "primary regression", result: rsilifecycle.ImpactResult{Observed: 101, Samples: 10}, want: selfCorrectionImpactRegressed},
		{name: "guardrail regression", result: rsilifecycle.ImpactResult{Observed: 70, Samples: 10, GuardrailViolations: []string{"error_rate"}}, want: selfCorrectionImpactRegressed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := classifySelfCorrectionImpact(contract, tt.result)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("classify = %q, want %q", got, tt.want)
			}
		})
	}
	// Below MinSamples is now a recorded verdict rather than an error. Erroring
	// made the caller drop the observation, so the ledger kept no trace of WHY a
	// candidate had no usefulness answer — the collapse this axis exists to
	// avoid. Note 70 would otherwise clear the target: an unmeasured success
	// must not be scored as one.
	got, err := classifySelfCorrectionImpact(contract, rsilifecycle.ImpactResult{Observed: 70, Samples: 9})
	if err != nil {
		t.Fatalf("insufficient evidence must not fail the record: %v", err)
	}
	if got != selfCorrectionImpactInconclusive {
		t.Fatalf("insufficient sample result = %q, want inconclusive", got)
	}
}

func TestSelfCorrectionImpactRejectsInvalidContractAndUnsafeResult(t *testing.T) {
	tracker := newTestTracker(t)
	_, err := tracker.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Title:  "invalid impact",
		Source: "health-finding:test",
		ImpactContract: &rsilifecycle.ImpactContract{
			Metric: "runtime.agent_ms.p95", Direction: selfCorrectionImpactDirectionDecrease,
			Baseline: 100, Target: 100, MinSamples: 1,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "target must be below baseline") {
		t.Fatalf("invalid contract error = %v", err)
	}
	_, err = tracker.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Title: "too many guardrails", Source: "health-finding:test",
		ImpactContract: &rsilifecycle.ImpactContract{
			Metric: "runtime.agent_ms.p95", Direction: selfCorrectionImpactDirectionDecrease,
			Baseline: 100, Target: 80, MinSamples: 1,
			Guardrails: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "guardrails exceed") {
		t.Fatalf("excess guardrail error = %v", err)
	}

	candidate := recordImpactCandidate(t, tracker)
	if _, err := tracker.RecordSelfCorrectionDispatch(SelfCorrectionCandidateRecord{
		ID: candidate.ID, AttemptID: "attempt-1",
		ImpactResult: &rsilifecycle.ImpactResult{Observed: 80, Samples: 10},
	}); err == nil || !strings.Contains(err.Error(), "watch_passed") {
		t.Fatalf("pre-watch impact error = %v", err)
	}
	watchPassImpactCandidate(t, tracker, candidate.ID, "attempt-1")
	if _, err := tracker.RecordSelfCorrectionDispatch(SelfCorrectionCandidateRecord{
		ID: candidate.ID, AttemptID: "wrong-attempt",
		ImpactResult: &rsilifecycle.ImpactResult{Observed: 80, Samples: 10},
	}); err == nil || !strings.Contains(err.Error(), "attempt mismatch") {
		t.Fatalf("wrong attempt impact error = %v", err)
	}
	if _, err := tracker.RecordSelfCorrectionDispatch(SelfCorrectionCandidateRecord{
		ID: candidate.ID, AttemptID: "attempt-1",
		ImpactResult: &rsilifecycle.ImpactResult{
			Observed: 70, Samples: 10, GuardrailViolations: []string{"undeclared"},
		},
	}); err == nil || !strings.Contains(err.Error(), "undeclared") {
		t.Fatalf("undeclared guardrail error = %v", err)
	}
	if _, err := tracker.RecordSelfCorrectionDispatch(SelfCorrectionCandidateRecord{
		ID: candidate.ID, AttemptID: "attempt-1",
		ImpactResult: &rsilifecycle.ImpactResult{
			Observed: 70, Samples: 10,
			GuardrailViolations: []string{"error_rate", "undeclared-after-valid"},
		},
	}); err == nil || !strings.Contains(err.Error(), "undeclared-after-valid") {
		t.Fatalf("trailing undeclared guardrail error = %v", err)
	}
}

func recordImpactCandidate(t *testing.T, tracker *Tracker) SelfCorrectionCandidateRecord {
	t.Helper()
	candidate, err := tracker.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope:       "code",
		Title:       "reduce runtime p95",
		Source:      "health-finding:runtime-latency",
		TargetFiles: []string{"gateway-go/internal/runtime/server"},
		ImpactContract: &rsilifecycle.ImpactContract{
			Metric:              "runtime.agent_ms.p95",
			Direction:           selfCorrectionImpactDirectionDecrease,
			Baseline:            100,
			Target:              80,
			MinSamples:          10,
			ObservationWindowMs: 0,
			Guardrails:          []string{"error_rate"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID: candidate.ID, Status: SelfCorrectionStatusAccepted, Reviewer: "test",
	}); err != nil {
		t.Fatal(err)
	}
	return candidate
}

func TestSelfCorrectionImpactEnforcesObservationWindow(t *testing.T) {
	tracker := newTestTracker(t)
	candidate, err := tracker.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "code", Title: "wait for observation", Source: "health-finding:window",
		TargetFiles: []string{"gateway-go/internal/runtime/server"},
		ImpactContract: &rsilifecycle.ImpactContract{
			Metric: "runtime.agent_ms.p95", Direction: selfCorrectionImpactDirectionDecrease,
			Baseline: 100, Target: 80, MinSamples: 10, ObservationWindowMs: 60_000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID: candidate.ID, Status: SelfCorrectionStatusAccepted, Reviewer: "test",
	}); err != nil {
		t.Fatal(err)
	}
	watchPassImpactCandidate(t, tracker, candidate.ID, "attempt-window")
	if _, err := tracker.RecordSelfCorrectionDispatch(SelfCorrectionCandidateRecord{
		ID: candidate.ID, AttemptID: "attempt-window",
		ImpactResult: &rsilifecycle.ImpactResult{Observed: 80, Samples: 10},
	}); err == nil || !strings.Contains(err.Error(), "observation window has not elapsed") {
		t.Fatalf("early impact error = %v", err)
	}
}

func watchPassImpactCandidate(t *testing.T, tracker *Tracker, id, attemptID string) {
	t.Helper()
	for _, phase := range []string{
		selfCorrectionDispatchStarted,
		selfCorrectionDispatchMerged,
		selfCorrectionDispatchDeployed,
		selfCorrectionDispatchWatchPassed,
	} {
		if _, err := tracker.RecordSelfCorrectionDispatch(SelfCorrectionCandidateRecord{
			ID: id, AttemptID: attemptID, DispatchPhase: phase,
		}); err != nil {
			t.Fatalf("dispatch %s: %v", phase, err)
		}
	}
}
