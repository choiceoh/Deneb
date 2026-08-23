package briefcase

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase/runcontract"
)

const supervisorTestDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func setRunProvenanceForTest(result *runcontract.RunResult) {
	result.Sampling = runcontract.SamplingProfile{Temperature: 0, TopP: 1}
	promptDigests := make([]string, 0, len(result.Episodes))
	for _, episode := range result.Episodes {
		if episode.SystemPromptSHA256 != "" {
			promptDigests = append(promptDigests, episode.SystemPromptSHA256)
		}
	}
	var err error
	result.SystemPromptSequenceSHA256, err = runcontract.SystemPromptSequenceDigest(promptDigests)
	if err != nil {
		panic(err)
	}
	result.ExecutionProfileSHA256, err = runcontract.ExecutionProfileDigest(
		result.Model,
		result.APIMode,
		result.ToolSchemaSHA256,
		result.EndpointSHA256,
		result.BuildSHA256,
		result.Sampling,
	)
	if err != nil {
		panic(err)
	}
}

func TestSupervisorEvaluateReturnsContinueThenPassWithHiddenDiagnostics(t *testing.T) {
	plan := signedSupervisorPlan(t, 2, 1, []SupervisorCheckpoint{
		{Cycle: 1, Checks: []Check{{ID: "final-answer", Type: CheckExactText, Weight: 1, ExpectedText: "final"}}},
		{Cycle: 2, Checks: []Check{{ID: "final-answer", Type: CheckExactText, Weight: 1, ExpectedText: "final"}}},
	})
	supervisor, err := NewSupervisor(plan)
	if err != nil {
		t.Fatal(err)
	}

	first := supervisorRun("run-1", "draft")
	public, err := supervisor.Evaluate(&first)
	if err != nil {
		t.Fatal(err)
	}
	if public.Decision != SupervisorContinue || !public.Recoverable || public.Score != 0 || public.BestCycle != 1 {
		t.Fatalf("first public result = %+v", public)
	}
	assertPublicDoesNotLeak(t, public)

	second := supervisorRun("run-2", "final")
	public, err = supervisor.Evaluate(&second)
	if err != nil {
		t.Fatal(err)
	}
	if public.Decision != SupervisorPass || public.Recoverable || public.Score != 1 || public.BestCycle != 2 || public.BestScore != 1 {
		t.Fatalf("second public result = %+v", public)
	}

	hidden := supervisor.HiddenDiagnostics()
	if !hidden.Terminal || hidden.PlanDigest != plan.PlanDigest || len(hidden.Cycles) != 2 {
		t.Fatalf("hidden diagnostics = %+v", hidden)
	}
	if hidden.Cycles[0].Report.Checks[0].ID != "final-answer" || hidden.Cycles[0].Reason != HiddenBelowThreshold || hidden.Cycles[1].Reason != HiddenPassed {
		t.Fatalf("hidden cycle diagnostics = %+v", hidden.Cycles)
	}
	// Defensive copy: caller mutation cannot alter supervisor state.
	hidden.Cycles[0].Report.Checks[0].ID = "tampered"
	if got := supervisor.HiddenDiagnostics().Cycles[0].Report.Checks[0].ID; got != "final-answer" {
		t.Fatalf("hidden diagnostic mutation leaked into supervisor: %q", got)
	}
	if _, err := supervisor.Evaluate(&second); !errors.Is(err, ErrSupervisorTerminal) {
		t.Fatalf("post-terminal error = %v", err)
	}
}

func TestSupervisorCanceledEvaluationDoesNotConsumeCheckpoint(t *testing.T) {
	plan := signedSupervisorPlan(t, 1, 1, []SupervisorCheckpoint{{
		Cycle: 1, Checks: []Check{{ID: "answer", Type: CheckExactText, Weight: 1, ExpectedText: "final"}},
	}})
	supervisor, err := NewSupervisor(plan)
	if err != nil {
		t.Fatal(err)
	}
	run := supervisorRun("run-1", "final")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := supervisor.EvaluateContext(ctx, &run); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled evaluation error = %v", err)
	}
	if hidden := supervisor.HiddenDiagnostics(); hidden.Terminal || len(hidden.Cycles) != 0 {
		t.Fatalf("canceled evaluation mutated supervisor: %+v", hidden)
	}
	public, err := supervisor.Evaluate(&run)
	if err != nil || public.Decision != SupervisorPass || public.Cycle != 1 {
		t.Fatalf("retry result = %+v err=%v", public, err)
	}
}

func TestSupervisorCriticalGateFailsImmediatelyAndPreservesBestScore(t *testing.T) {
	plan := signedSupervisorPlan(t, 3, 0.5, []SupervisorCheckpoint{
		{Cycle: 1, Checks: []Check{
			{ID: "unsafe", Type: CheckForbidden, Critical: true, Weight: 1, Needle: "unsafe"},
			{ID: "substance", Type: CheckContains, Weight: 9, Needle: "unsafe"},
		}},
		{Cycle: 2, Checks: []Check{{ID: "later", Type: CheckContains, Weight: 1, Needle: "ok"}}},
		{Cycle: 3, Checks: []Check{{ID: "last", Type: CheckContains, Weight: 1, Needle: "ok"}}},
	})
	supervisor, err := NewSupervisor(plan)
	if err != nil {
		t.Fatal(err)
	}
	run := supervisorRun("run-critical", "unsafe")
	public, err := supervisor.Evaluate(&run)
	if err != nil {
		t.Fatal(err)
	}
	if public.Decision != SupervisorFail || public.Recoverable || public.Score != 0.9 || public.BestCycle != 1 || public.BestScore != 0.9 {
		t.Fatalf("critical public result = %+v", public)
	}
	hidden := supervisor.HiddenDiagnostics()
	if hidden.Cycles[0].Reason != HiddenCriticalGate || hidden.Cycles[0].Report.CriticalPassed {
		t.Fatalf("critical diagnostics = %+v", hidden.Cycles[0])
	}
}

func TestSupervisorCycleLimitFailsAndPreservesEarliestBestTie(t *testing.T) {
	checkpoints := []SupervisorCheckpoint{
		{Cycle: 1, Checks: halfScoreChecks()},
		{Cycle: 2, Checks: halfScoreChecks()},
	}
	plan := signedSupervisorPlan(t, 2, 1, checkpoints)
	supervisor, err := NewSupervisor(plan)
	if err != nil {
		t.Fatal(err)
	}
	first := supervisorRun("run-1", "alpha")
	one, _ := supervisor.Evaluate(&first)
	if one.Decision != SupervisorContinue || one.BestCycle != 1 || one.BestScore != 0.5 {
		t.Fatalf("first = %+v", one)
	}
	second := supervisorRun("run-2", "alpha")
	two, _ := supervisor.Evaluate(&second)
	if two.Decision != SupervisorFail || two.Recoverable || two.Score != 0.5 || two.BestCycle != 1 || two.BestScore != 0.5 {
		t.Fatalf("second = %+v", two)
	}
	if got := supervisor.HiddenDiagnostics().Cycles[1].Reason; got != HiddenCycleLimit {
		t.Fatalf("last reason = %q", got)
	}
}

func TestSupervisorInvalidGradeAndRunMismatchFailClosed(t *testing.T) {
	statePlan := signedSupervisorPlan(t, 1, 1, []SupervisorCheckpoint{{
		Cycle:  1,
		Checks: []Check{{ID: "state", Type: CheckStateJSONEqual, Weight: 1, ExpectedState: json.RawMessage(`{"ok":true}`)}},
	}})
	supervisor, err := NewSupervisor(statePlan)
	if err != nil {
		t.Fatal(err)
	}
	run := supervisorRun("run-invalid", "answer")
	run.State = nil
	public, err := supervisor.Evaluate(&run)
	if err != nil {
		t.Fatal(err)
	}
	if public.Decision != SupervisorFail || public.Recoverable || supervisor.HiddenDiagnostics().Cycles[0].Reason != HiddenInvalidGrade {
		t.Fatalf("invalid grade result = %+v hidden=%+v", public, supervisor.HiddenDiagnostics())
	}

	mismatchPlan := signedSupervisorPlan(t, 1, 1, []SupervisorCheckpoint{{
		Cycle: 1, Checks: []Check{{ID: "answer", Type: CheckContains, Weight: 1, Needle: "ok"}},
	}})
	mismatchPlan.Fingerprint.Model = "expected-model"
	if _, err := SetSupervisorPlanDigest(&mismatchPlan); err != nil {
		t.Fatal(err)
	}
	supervisor, err = NewSupervisor(mismatchPlan)
	if err != nil {
		t.Fatal(err)
	}
	run = supervisorRun("run-mismatch", "ok")
	public, err = supervisor.Evaluate(&run)
	if err != nil {
		t.Fatal(err)
	}
	hidden := supervisor.HiddenDiagnostics()
	if public.Decision != SupervisorFail || hidden.Cycles[0].Reason != HiddenRunMismatch || len(hidden.Cycles[0].Report.Checks) != 0 {
		t.Fatalf("mismatch public=%+v hidden=%+v", public, hidden)
	}
}

func TestSupervisorPlanDigestMatchesAndValidationRejectsTamperedPlans(t *testing.T) {
	valid := signedSupervisorPlan(t, 1, 1, []SupervisorCheckpoint{{
		Cycle: 1, Checks: []Check{{ID: "answer", Type: CheckContains, Weight: 1, Needle: "ok"}},
	}})
	digest, err := SupervisorPlanDigest(valid)
	if err != nil || digest != valid.PlanDigest {
		t.Fatalf("digest = %q err=%v want=%q", digest, err, valid.PlanDigest)
	}

	tests := []struct {
		name string
		plan SupervisorPlan
	}{
		{name: "stale digest", plan: func() SupervisorPlan { p := valid; p.MaxCycles = 2; return p }()},
		{name: "missing checkpoint", plan: resignSupervisorPlan(t, func(p SupervisorPlan) SupervisorPlan { p.MaxCycles = 2; return p }(valid))},
		{name: "noncontiguous", plan: resignSupervisorPlan(t, func(p SupervisorPlan) SupervisorPlan { p.Checkpoints[0].Cycle = 2; return p }(cloneSupervisorPlan(valid)))},
		{name: "duplicate check", plan: resignSupervisorPlan(t, func(p SupervisorPlan) SupervisorPlan {
			p.Checkpoints[0].Checks = append(p.Checkpoints[0].Checks, p.Checkpoints[0].Checks[0])
			return p
		}(cloneSupervisorPlan(valid)))},
		{name: "bad expected state", plan: resignSupervisorPlan(t, func(p SupervisorPlan) SupervisorPlan {
			p.Checkpoints[0].Checks[0] = Check{ID: "state", Type: CheckStateJSONEqual, Weight: 1, ExpectedState: json.RawMessage(`{"a":1,"a":2}`)}
			return p
		}(cloneSupervisorPlan(valid)))},
		{name: "cumulative weight overflow", plan: resignSupervisorPlan(t, func(p SupervisorPlan) SupervisorPlan {
			p.Checkpoints[0].Checks = []Check{
				{ID: "one", Type: CheckExactText, Weight: 1e308},
				{ID: "two", Type: CheckExactText, Weight: 1e308},
			}
			return p
		}(cloneSupervisorPlan(valid)))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewSupervisor(tt.plan); !errors.Is(err, ErrInvalidSupervisorPlan) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSupervisorPlanValidationReturnsFirstViolationInContractOrder(t *testing.T) {
	valid := signedSupervisorPlan(t, 1, 1, []SupervisorCheckpoint{{
		Cycle: 1, Checks: []Check{{ID: "answer", Type: CheckContains, Weight: 1, Needle: "ok"}},
	}})
	tests := []struct {
		name string
		plan SupervisorPlan
		want string
	}{
		{
			name: "required identity precedes seed",
			plan: resignSupervisorPlan(t, func(plan SupervisorPlan) SupervisorPlan {
				plan.Fingerprint.CaseID = ""
				plan.Fingerprint.Seed = -1
				return plan
			}(cloneSupervisorPlan(valid))),
			want: "invalid briefcase supervisor plan: fingerprint.caseId is required",
		},
		{
			name: "fingerprint digests follow contract order",
			plan: resignSupervisorPlan(t, func(plan SupervisorPlan) SupervisorPlan {
				plan.Fingerprint.CasepackSHA256 = "bad"
				plan.Fingerprint.BuildSHA256 = "also-bad"
				return plan
			}(cloneSupervisorPlan(valid))),
			want: "invalid briefcase supervisor plan: fingerprint.casepackSha256 must be lowercase SHA-256",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSupervisor(tt.plan)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func signedSupervisorPlan(t *testing.T, maxCycles int, threshold float64, checkpoints []SupervisorCheckpoint) SupervisorPlan {
	t.Helper()
	plan := SupervisorPlan{
		SchemaVersion: SupervisorPlanSchemaVersion,
		Fingerprint: SupervisorFingerprint{
			CaseID: "case-1", CasepackSHA256: supervisorTestDigest, Seed: 7,
			Arm: string(runcontract.ArmMemoryAssisted), APIMode: "openai",
		},
		MaxCycles: maxCycles, PassThreshold: threshold, FeedbackDenyTokens: []string{"final"}, Checkpoints: checkpoints,
	}
	return resignSupervisorPlan(t, plan)
}

func resignSupervisorPlan(t *testing.T, plan SupervisorPlan) SupervisorPlan {
	t.Helper()
	if _, err := SetSupervisorPlanDigest(&plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func supervisorRun(runID, text string) runcontract.RunResult {
	result := runcontract.RunResult{
		SchemaVersion: "deneb-briefcase-run/v1",
		RunID:         runID, CaseID: "case-1", CasepackSHA256: supervisorTestDigest,
		Seed: 7, Model: "test-model", ProviderModel: "served-test-model", APIMode: "openai",
		Arm:        runcontract.ArmMemoryAssisted,
		RecallMode: "enabled", ToolSchemaSHA256: supervisorTestDigest,
		EndpointSHA256: supervisorTestDigest, BuildSHA256: supervisorTestDigest,
		ExecutionProfileSHA256: supervisorTestDigest,
		Episodes: []runcontract.EpisodeResult{{
			EpisodeID: "episode", Text: text, Model: "test-model", ProviderModel: "served-test-model", StopReason: "end_turn",
			SystemPromptSHA256: supervisorTestDigest,
		}},
		State: json.RawMessage(`{"ok":true}`),
	}
	setRunProvenanceForTest(&result)
	return result
}

func halfScoreChecks() []Check {
	return []Check{
		{ID: "alpha", Type: CheckContains, Weight: 1, Needle: "alpha"},
		{ID: "beta", Type: CheckContains, Weight: 1, Needle: "beta"},
	}
}

func assertPublicDoesNotLeak(t *testing.T, public SupervisorPublicResult) {
	t.Helper()
	data, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"final-answer", "expectedText", "checks", "report", "below_threshold"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public result leaked hidden diagnostic %q: %s", forbidden, text)
		}
	}
}
