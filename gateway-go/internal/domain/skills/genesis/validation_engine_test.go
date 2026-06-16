package genesis

import (
	"log/slog"
	"strings"
	"testing"
)

func TestSkillValidationEngineRejectsHeldOutRegression(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName:           "topsolar-db",
		ID:                  "safe-wrapper",
		RequiredSubstrings:  []string{"단일 bash block"},
		ForbiddenSubstrings: []string{"eval"},
		RequiredHeadings:    []string{"통합 실행 흐름"},
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase: %v", err)
	}
	engine := NewSkillValidationEngine(tr, slog.Default())
	original := "---\nname: topsolar-db\n---\n\n# Skill\n\n## 통합 실행 흐름\n- 항상 단일 bash block 사용\n"
	candidate := "# Skill\n\n## Procedure\n- eval 로 실행\n"

	result, err := engine.ValidateCandidate("topsolar-db", original, candidate)
	if err != nil {
		t.Fatalf("ValidateCandidate: %v", err)
	}
	if !result.Evaluated || result.Pass || !strings.Contains(result.Reason, "regressed validation cases") {
		t.Fatalf("expected held-out regression rejection, got %+v", result)
	}
	if result.OriginalPassed <= result.CandidatePassed {
		t.Fatalf("expected candidate to score worse than original, got %+v", result)
	}
}

func TestSkillValidationEnginePassesHeldOutImprovement(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName:          "deploy-helper",
		ID:                 "verification-required",
		RequiredSubstrings: []string{"verify health"},
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase: %v", err)
	}
	engine := NewSkillValidationEngine(tr, slog.Default())
	original := "---\nname: deploy-helper\n---\n\n# Deploy\n\n## Procedure\n- deploy\n"
	candidate := "# Deploy\n\n## Procedure\n- deploy\n- verify health\n"

	result, err := engine.ValidateCandidate("deploy-helper", original, candidate)
	if err != nil {
		t.Fatalf("ValidateCandidate: %v", err)
	}
	if !result.Evaluated || !result.Pass {
		t.Fatalf("expected held-out improvement to pass, got %+v", result)
	}
	if result.CandidateScore <= result.OriginalScore {
		t.Fatalf("expected candidate score to improve, got %+v", result)
	}
}

func TestSkillValidationEngineScoresDryRunReplay(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName:   "srv1-ops",
		ID:          "inspect-real-server",
		Description: "operator asked to inspect srv1 before improving",
		Replay: SkillReplayCaseRecord{
			Input:            "Tailscale SSH into srv1, inspect deneb-gateway, then improve from the real state.",
			Context:          []string{"Do not infer from local state only."},
			RequiredActions:  []string{"ssh srv1", "systemctl --user status deneb-gateway.service"},
			ForbiddenActions: []string{"assume local health is production health"},
			RequiredTools:    []string{"ssh"},
		},
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase: %v", err)
	}
	engine := NewSkillValidationEngine(tr, slog.Default())
	original := "---\nname: srv1-ops\n---\n\n# Ops\n\n## Procedure\n- Check local /health and summarize.\n"
	candidate := "# Ops\n\n## Procedure\n- ssh srv1\n- systemctl --user status deneb-gateway.service\n- Compare the real service state before proposing changes.\n"

	result, err := engine.ValidateCandidate("srv1-ops", original, candidate)
	if err != nil {
		t.Fatalf("ValidateCandidate: %v", err)
	}
	if !result.Evaluated || !result.Pass {
		t.Fatalf("expected replay improvement to pass, got %+v", result)
	}
	if result.OriginalPassed != 1 || result.CandidatePassed != 4 {
		t.Fatalf("expected replay assertions scored against skill body, got %+v", result)
	}
}

func TestSkillValidationEngineRejectsDryRunReplayRegression(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName: "srv1-ops",
		ID:        "inspect-real-server",
		Replay: SkillReplayCaseRecord{
			Input:            "Tailscale SSH into srv1, inspect deneb-gateway, then improve from the real state.",
			RequiredActions:  []string{"ssh srv1", "systemctl --user status deneb-gateway.service"},
			ForbiddenActions: []string{"assume local health is production health"},
			RequiredTools:    []string{"ssh"},
		},
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase: %v", err)
	}
	engine := NewSkillValidationEngine(tr, slog.Default())
	original := "---\nname: srv1-ops\n---\n\n# Ops\n\n## Procedure\n- ssh srv1\n- systemctl --user status deneb-gateway.service\n"
	candidate := "# Ops\n\n## Procedure\n- assume local health is production health\n"

	result, err := engine.ValidateCandidate("srv1-ops", original, candidate)
	if err != nil {
		t.Fatalf("ValidateCandidate: %v", err)
	}
	if !result.Evaluated || result.Pass || !strings.Contains(result.Reason, "regressed validation cases") {
		t.Fatalf("expected replay regression rejection, got %+v", result)
	}
	if result.CandidatePassed >= result.OriginalPassed {
		t.Fatalf("expected replay candidate to score worse than original, got %+v", result)
	}
}

func TestSkillValidationEngineDoesNotScoreReplayInputAsCandidateBehavior(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName: "srv1-ops",
		ID:        "input-is-not-answer",
		Replay: SkillReplayCaseRecord{
			Input:           "Run ssh srv1 and systemctl --user status deneb-gateway.service.",
			RequiredActions: []string{"ssh srv1", "systemctl --user status deneb-gateway.service"},
			RequiredTools:   []string{"ssh"},
		},
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase: %v", err)
	}
	engine := NewSkillValidationEngine(tr, slog.Default())
	original := "---\nname: srv1-ops\n---\n\n# Ops\n\n## Procedure\n- ssh srv1\n- systemctl --user status deneb-gateway.service\n"
	candidate := "# Ops\n\n## Procedure\n- Check status somehow.\n"

	result, err := engine.ValidateCandidate("srv1-ops", original, candidate)
	if err != nil {
		t.Fatalf("ValidateCandidate: %v", err)
	}
	if !result.Evaluated || result.Pass {
		t.Fatalf("replay input/context must not satisfy candidate behavior assertions, got %+v", result)
	}
}

func TestSkillValidationEngineScoresExpectedToolCallTrace(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName: "srv1-ops",
		ID:        "real-server-tool-trace",
		Replay: SkillReplayCaseRecord{
			Input: "Inspect srv1 before improving.",
			ExpectedToolCalls: []SkillReplayToolCallRecord{
				{Name: "exec", InputIncludes: []string{"ssh srv1"}},
				{Name: "exec", InputIncludes: []string{"systemctl --user status deneb-gateway.service"}},
			},
			ForbiddenToolCalls: []SkillReplayToolCallRecord{
				{Name: "exec", InputIncludes: []string{"rm -rf"}},
			},
			RequireOrder: true,
		},
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase: %v", err)
	}
	engine := NewSkillValidationEngine(tr, slog.Default())
	original := "---\nname: srv1-ops\n---\n\n# Ops\n\n## Procedure\n- exec: ssh srv1\n"
	candidate := "# Ops\n\n## Procedure\n- exec: ssh srv1\n- exec: systemctl --user status deneb-gateway.service\n"

	result, err := engine.ValidateCandidate("srv1-ops", original, candidate)
	if err != nil {
		t.Fatalf("ValidateCandidate: %v", err)
	}
	if !result.Evaluated || !result.Pass {
		t.Fatalf("expected tool-call trace improvement to pass, got %+v", result)
	}
}

func TestSkillValidationEngineRejectsToolCallTraceOrderRegression(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName: "srv1-ops",
		ID:        "real-server-tool-trace",
		Replay: SkillReplayCaseRecord{
			ExpectedToolCalls: []SkillReplayToolCallRecord{
				{Name: "exec", InputIncludes: []string{"ssh srv1"}},
				{Name: "exec", InputIncludes: []string{"systemctl --user status deneb-gateway.service"}},
			},
			RequireOrder: true,
		},
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase: %v", err)
	}
	engine := NewSkillValidationEngine(tr, slog.Default())
	original := "---\nname: srv1-ops\n---\n\n# Ops\n\n## Procedure\n- exec: ssh srv1\n- exec: systemctl --user status deneb-gateway.service\n"
	candidate := "# Ops\n\n## Procedure\n- exec: systemctl --user status deneb-gateway.service\n- exec: ssh srv1\n"

	result, err := engine.ValidateCandidate("srv1-ops", original, candidate)
	if err != nil {
		t.Fatalf("ValidateCandidate: %v", err)
	}
	if !result.Evaluated || result.Pass || !strings.Contains(result.Reason, "regressed validation cases") {
		t.Fatalf("expected ordered trace regression rejection, got %+v", result)
	}
}

func TestSkillValidationEngineScoresFixtureObservations(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName: "srv1-ops",
		ID:        "service-status-observation",
		Replay: SkillReplayCaseRecord{
			ExpectedToolCalls: []SkillReplayToolCallRecord{
				{
					Name:          "exec",
					InputIncludes: []string{"systemctl --user status deneb-gateway.service"},
					FixtureOutput: "Active: active (running)\nMain PID: 1234",
					FixtureError:  false,
				},
			},
			RequiredObservations:  []string{"active (running)"},
			ForbiddenObservations: []string{"서비스가 꺼져 있다"},
		},
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase: %v", err)
	}
	engine := NewSkillValidationEngine(tr, slog.Default())
	original := "---\nname: srv1-ops\n---\n\n# Ops\n\n## Procedure\n- exec: systemctl --user status deneb-gateway.service\n"
	candidate := "# Ops\n\n## Procedure\n- exec: systemctl --user status deneb-gateway.service\n- Verify the output includes active (running) before changing anything.\n"

	result, err := engine.ValidateCandidate("srv1-ops", original, candidate)
	if err != nil {
		t.Fatalf("ValidateCandidate: %v", err)
	}
	if !result.Evaluated || !result.Pass {
		t.Fatalf("expected fixture observation improvement to pass, got %+v", result)
	}
}

func TestSkillValidationEngineDoesNotScoreFixtureOutputAsCandidateBehavior(t *testing.T) {
	tr := newTestTracker(t)
	if err := tr.RecordSkillValidationCase(SkillValidationCaseRecord{
		SkillName: "srv1-ops",
		ID:        "fixture-is-not-answer",
		Replay: SkillReplayCaseRecord{
			ExpectedToolCalls: []SkillReplayToolCallRecord{
				{
					Name:          "exec",
					InputIncludes: []string{"systemctl --user status deneb-gateway.service"},
					FixtureOutput: "Active: active (running)",
				},
			},
			RequiredObservations: []string{"active (running)"},
		},
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase: %v", err)
	}
	engine := NewSkillValidationEngine(tr, slog.Default())
	original := "---\nname: srv1-ops\n---\n\n# Ops\n\n## Procedure\n- exec: systemctl --user status deneb-gateway.service\n- Verify active (running).\n"
	candidate := "# Ops\n\n## Procedure\n- exec: systemctl --user status deneb-gateway.service\n"

	result, err := engine.ValidateCandidate("srv1-ops", original, candidate)
	if err != nil {
		t.Fatalf("ValidateCandidate: %v", err)
	}
	if !result.Evaluated || result.Pass {
		t.Fatalf("fixture output must not satisfy candidate observation assertions, got %+v", result)
	}
}
