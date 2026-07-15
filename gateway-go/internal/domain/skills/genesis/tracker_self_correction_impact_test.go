package genesis

import (
	"log/slog"
	"strings"
	"testing"
)

func TestSelfCorrectionImpactPersistsIndependentUsefulnessVerdict(t *testing.T) {
	tracker := newTestTracker(t)
	candidate := recordImpactCandidate(t, tracker)
	watchPassImpactCandidate(t, tracker, candidate.ID, "attempt-1")

	rows, err := tracker.RecentSelfCorrectionCandidates("", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := SelfCorrectionImpactStatus(rows[0]); got != SelfCorrectionImpactPending {
		t.Fatalf("impact before observation = %q, want pending", got)
	}

	impact := SelfCorrectionCandidateRecord{
		ID:        candidate.ID,
		AttemptID: "attempt-1",
		ImpactResult: &SelfCorrectionImpactResult{
			Observed: 80,
			Samples:  12,
			Note:     "p95 improved after deploy",
		},
	}
	if _, err := tracker.RecordSelfCorrectionImpact(impact); err != nil {
		t.Fatal(err)
	}
	// Exact retries are idempotent and survive a fresh read-side tracker.
	if _, err := tracker.RecordSelfCorrectionImpact(impact); err != nil {
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
	if got := SelfCorrectionImpactStatus(rows[0]); got != SelfCorrectionImpactVerified {
		t.Fatalf("restored impact = %q, want verified", got)
	}
	if rows[0].Status != SelfCorrectionStatusApplied {
		t.Fatalf("safety review status changed: %q", rows[0].Status)
	}

	conflict := impact
	conflict.ImpactResult = &SelfCorrectionImpactResult{Observed: 95, Samples: 12}
	if _, err := tracker.RecordSelfCorrectionImpact(conflict); err == nil || !strings.Contains(err.Error(), "already terminal") {
		t.Fatalf("conflicting terminal result error = %v", err)
	}
}

func TestSelfCorrectionImpactClassifiesTargetBaselineAndGuardrails(t *testing.T) {
	contract := SelfCorrectionImpactContract{
		Metric:     "runtime.agent_ms.p95",
		Direction:  SelfCorrectionImpactDirectionDecrease,
		Baseline:   100,
		Target:     80,
		MinSamples: 10,
		Guardrails: []string{"error_rate"},
	}
	tests := []struct {
		name   string
		result SelfCorrectionImpactResult
		want   string
	}{
		{name: "verified", result: SelfCorrectionImpactResult{Observed: 79, Samples: 10}, want: SelfCorrectionImpactVerified},
		{name: "partial is no effect", result: SelfCorrectionImpactResult{Observed: 90, Samples: 10}, want: SelfCorrectionImpactNoEffect},
		{name: "flat is no effect", result: SelfCorrectionImpactResult{Observed: 100, Samples: 10}, want: SelfCorrectionImpactNoEffect},
		{name: "primary regression", result: SelfCorrectionImpactResult{Observed: 101, Samples: 10}, want: SelfCorrectionImpactRegressed},
		{name: "guardrail regression", result: SelfCorrectionImpactResult{Observed: 70, Samples: 10, GuardrailViolations: []string{"error_rate"}}, want: SelfCorrectionImpactRegressed},
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
	if _, err := classifySelfCorrectionImpact(contract, SelfCorrectionImpactResult{Observed: 70, Samples: 9}); err == nil {
		t.Fatal("insufficient sample result was accepted")
	}
}

func TestSelfCorrectionImpactRejectsInvalidContractAndUnsafeResult(t *testing.T) {
	tracker := newTestTracker(t)
	_, err := tracker.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Title:  "invalid impact",
		Source: "health-finding:test",
		ImpactContract: &SelfCorrectionImpactContract{
			Metric: "runtime.agent_ms.p95", Direction: SelfCorrectionImpactDirectionDecrease,
			Baseline: 100, Target: 100, MinSamples: 1,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "target must be below baseline") {
		t.Fatalf("invalid contract error = %v", err)
	}
	_, err = tracker.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Title: "too many guardrails", Source: "health-finding:test",
		ImpactContract: &SelfCorrectionImpactContract{
			Metric: "runtime.agent_ms.p95", Direction: SelfCorrectionImpactDirectionDecrease,
			Baseline: 100, Target: 80, MinSamples: 1,
			Guardrails: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "guardrails exceed") {
		t.Fatalf("excess guardrail error = %v", err)
	}

	candidate := recordImpactCandidate(t, tracker)
	if _, err := tracker.RecordSelfCorrectionImpact(SelfCorrectionCandidateRecord{
		ID: candidate.ID, AttemptID: "attempt-1",
		ImpactResult: &SelfCorrectionImpactResult{Observed: 80, Samples: 10},
	}); err == nil || !strings.Contains(err.Error(), "watch_passed") {
		t.Fatalf("pre-watch impact error = %v", err)
	}
	watchPassImpactCandidate(t, tracker, candidate.ID, "attempt-1")
	if _, err := tracker.RecordSelfCorrectionImpact(SelfCorrectionCandidateRecord{
		ID: candidate.ID, AttemptID: "wrong-attempt",
		ImpactResult: &SelfCorrectionImpactResult{Observed: 80, Samples: 10},
	}); err == nil || !strings.Contains(err.Error(), "attempt mismatch") {
		t.Fatalf("wrong attempt impact error = %v", err)
	}
	if _, err := tracker.RecordSelfCorrectionImpact(SelfCorrectionCandidateRecord{
		ID: candidate.ID, AttemptID: "attempt-1",
		ImpactResult: &SelfCorrectionImpactResult{
			Observed: 70, Samples: 10, GuardrailViolations: []string{"undeclared"},
		},
	}); err == nil || !strings.Contains(err.Error(), "undeclared") {
		t.Fatalf("undeclared guardrail error = %v", err)
	}
	if _, err := tracker.RecordSelfCorrectionImpact(SelfCorrectionCandidateRecord{
		ID: candidate.ID, AttemptID: "attempt-1",
		ImpactResult: &SelfCorrectionImpactResult{
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
		ImpactContract: &SelfCorrectionImpactContract{
			Metric:              "runtime.agent_ms.p95",
			Direction:           SelfCorrectionImpactDirectionDecrease,
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
		ImpactContract: &SelfCorrectionImpactContract{
			Metric: "runtime.agent_ms.p95", Direction: SelfCorrectionImpactDirectionDecrease,
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
	if _, err := tracker.RecordSelfCorrectionImpact(SelfCorrectionCandidateRecord{
		ID: candidate.ID, AttemptID: "attempt-window",
		ImpactResult: &SelfCorrectionImpactResult{Observed: 80, Samples: 10},
	}); err == nil || !strings.Contains(err.Error(), "observation window has not elapsed") {
		t.Fatalf("early impact error = %v", err)
	}
}

func watchPassImpactCandidate(t *testing.T, tracker *Tracker, id, attemptID string) {
	t.Helper()
	for _, phase := range []string{
		selfCorrectionDispatchStarted,
		SelfCorrectionDispatchMerged,
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
