package briefcase

import (
	"errors"
	"strings"
	"testing"

	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
)

func TestScriptedUserSimulatorConsumesOnlyPublicHandoffInOrder(t *testing.T) {
	plan := UserSimulatorPlan{
		SchemaVersion: UserSimulatorPlanSchemaVersion,
		CaseID:        "case-1",
		FollowUps: []ScriptedFollowUp{
			{Cycle: 1, WhenVerdict: VerdictNeedsRevision, Message: "공개된 결과를 다시 검토해 주세요."},
			{Cycle: 2, WhenVerdict: VerdictNeedsRevision, Message: "남은 누락 사항을 확인하고 마무리해 주세요."},
		},
	}
	simulator, err := NewScriptedUserSimulator(plan, casepack.MaxFollowUpsV1)
	if err != nil {
		t.Fatal(err)
	}
	firewall, err := NewFeedbackFirewall(HiddenFeedbackInputs{}, FeedbackLimits{})
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := firewall.BuildHandoff(SimulatorHandoffInput{
		VerdictCategory: VerdictNeedsRevision,
		Recoverable:     true,
		ScoreBand:       ScoreBandMedium,
		VisibleTrajectorySummaries: []string{
			"초안이 생성되었습니다.",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := simulator.NextFeedback(t.Context(), 1, handoff)
	if err != nil || first != plan.FollowUps[0].Message {
		t.Fatalf("first feedback = %q, %v", first, err)
	}
	if _, err := simulator.NextFeedback(t.Context(), 1, handoff); !errors.Is(err, ErrInvalidUserSimulatorPlan) {
		t.Fatalf("duplicate cycle error = %v", err)
	}
	second, err := simulator.NextFeedback(t.Context(), 2, handoff)
	if err != nil || second != plan.FollowUps[1].Message {
		t.Fatalf("second feedback = %q, %v", second, err)
	}
	if _, err := simulator.NextFeedback(t.Context(), 3, handoff); !errors.Is(err, ErrInvalidUserSimulatorPlan) {
		t.Fatalf("exhausted budget error = %v", err)
	}
}

func TestScriptedUserSimulatorPlanFailsClosed(t *testing.T) {
	valid := UserSimulatorPlan{
		SchemaVersion: UserSimulatorPlanSchemaVersion,
		CaseID:        "case-1",
		FollowUps: []ScriptedFollowUp{
			{Cycle: 1, WhenVerdict: VerdictNeedsRevision, Message: "Try again."},
		},
	}
	tests := []struct {
		name   string
		mutate func(*UserSimulatorPlan)
		budget int
	}{
		{name: "schema", budget: 1, mutate: func(p *UserSimulatorPlan) { p.SchemaVersion = "v2" }},
		{name: "case", budget: 1, mutate: func(p *UserSimulatorPlan) { p.CaseID = "" }},
		{name: "coverage", budget: 2, mutate: func(*UserSimulatorPlan) {}},
		{name: "cycle", budget: 1, mutate: func(p *UserSimulatorPlan) { p.FollowUps[0].Cycle = 2 }},
		{name: "verdict", budget: 1, mutate: func(p *UserSimulatorPlan) { p.FollowUps[0].WhenVerdict = VerdictBlocked }},
		{name: "message", budget: 1, mutate: func(p *UserSimulatorPlan) { p.FollowUps[0].Message = "  " }},
		{name: "message too long", budget: 1, mutate: func(p *UserSimulatorPlan) { p.FollowUps[0].Message = strings.Repeat("x", defaultMaxFollowUpRunes+1) }},
		{name: "budget", budget: casepack.MaxFollowUpsV1 + 1, mutate: func(*UserSimulatorPlan) {}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := valid
			plan.FollowUps = append([]ScriptedFollowUp(nil), valid.FollowUps...)
			test.mutate(&plan)
			if _, err := NewScriptedUserSimulator(plan, test.budget); !errors.Is(err, ErrInvalidUserSimulatorPlan) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
