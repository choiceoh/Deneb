package chat

import (
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
)

type usageCall struct {
	session, skill, errMsg, model string
	success                       bool
}

type fakeUsageRecorder struct{ calls []usageCall }

func (f *fakeUsageRecorder) RecordSkillUse(sessionKey, skillName string, success bool, errMsg, model string) {
	f.calls = append(f.calls, usageCall{sessionKey, skillName, errMsg, model, success})
}

func TestRecordRunSkillUsageReturnsSuccessForCleanRun(t *testing.T) {
	rec := &fakeUsageRecorder{}
	log := NewSkillConsultLog()
	log.Add("research-flow")
	recordRunSkillUsage(rec, log, &agent.AgentResult{
		Text:           "done",
		StopReason:     "end_turn",
		ToolActivities: []agent.ToolActivity{{Name: "skills"}, {Name: "read"}},
	}, nil, "client:main", "m1")

	if len(rec.calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(rec.calls), rec.calls)
	}
	c := rec.calls[0]
	if c.skill != "research-flow" || !c.success || c.errMsg != "" || c.session != "client:main" || c.model != "m1" {
		t.Fatalf("unexpected call: %+v", c)
	}
}

func TestRecordRunSkillUsage_LaterToolErrorIsFailure(t *testing.T) {
	rec := &fakeUsageRecorder{}
	log := NewSkillConsultLog()
	log.Add("deploy-flow")
	recordRunSkillUsage(rec, log, &agent.AgentResult{
		Text:           "recovered answer",
		StopReason:     "end_turn",
		ToolActivities: []agent.ToolActivity{{Name: "skills"}, {Name: "read"}, {Name: "exec", IsError: true}},
	}, nil, "client:main", "m1")

	if len(rec.calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(rec.calls), rec.calls)
	}
	c := rec.calls[0]
	if c.success {
		t.Fatalf("run with a later tool error must record failure: %+v", c)
	}
	if c.errMsg == "" {
		t.Fatalf("failure should carry an error message naming the tool: %+v", c)
	}
}

func TestRecordRunSkillUsage_SkillsToolErrorIsNotSkillFailure(t *testing.T) {
	// When the "skills" tool itself errors, the consult mechanism failed to load
	// the skill (a gateway path/catalog bug) — that is not the skill performing
	// badly, so the consulted skill must NOT be recorded as a failure. Otherwise
	// the evolver pins it below its success-rate threshold and re-evolves it
	// forever chasing a gateway error it cannot fix.
	// (Session key must be a REAL session: review-fork sessions
	// ("system:skill-review:*") are now excluded from recording entirely —
	// see TestRecordTurnSkillUsageIgnoresReviewForks.)
	rec := &fakeUsageRecorder{}
	log := NewSkillConsultLog()
	log.Add("email-analysis")
	recordRunSkillUsage(rec, log, &agent.AgentResult{
		Text:           "done",
		StopReason:     "end_turn",
		ToolActivities: []agent.ToolActivity{{Name: "skills", IsError: true}},
	}, nil, "client:main", "m1")

	if len(rec.calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(rec.calls), rec.calls)
	}
	c := rec.calls[0]
	if !c.success || c.errMsg != "" {
		t.Fatalf("a skills-tool error must not be attributed to the skill: %+v", c)
	}
}

func TestRecordRunSkillUsage_NonSkillsErrorStillFailsAlongsideSkills(t *testing.T) {
	// A genuine non-"skills" tool error is still a real failure even when the
	// skills tool also appears in the turn's activities.
	rec := &fakeUsageRecorder{}
	log := NewSkillConsultLog()
	log.Add("deploy-flow")
	recordRunSkillUsage(rec, log, &agent.AgentResult{
		Text:           "done",
		StopReason:     "end_turn",
		ToolActivities: []agent.ToolActivity{{Name: "skills", IsError: true}, {Name: "exec", IsError: true}},
	}, nil, "client:main", "m1")

	if len(rec.calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(rec.calls), rec.calls)
	}
	if c := rec.calls[0]; c.success || c.errMsg == "" {
		t.Fatalf("a non-skills tool error must still record failure: %+v", c)
	}
}

func TestRecordRunSkillUsage_MissingFinalDeliverableIsFailure(t *testing.T) {
	rec := &fakeUsageRecorder{}
	log := NewSkillConsultLog()
	log.Add("report-flow")
	recordRunSkillUsage(rec, log, &agent.AgentResult{StopReason: "end_turn"}, nil, "client:main", "m1")

	if len(rec.calls) != 1 || rec.calls[0].success || rec.calls[0].errMsg != "run failed: no final deliverable" {
		t.Fatalf("missing final deliverable was not recorded as failure: %+v", rec.calls)
	}
}

func TestRecordRunSkillUsage_RunErrorIsFailure(t *testing.T) {
	rec := &fakeUsageRecorder{}
	log := NewSkillConsultLog()
	log.Add("report-flow")
	recordRunSkillUsage(rec, log, nil, errors.New("cancelled"), "client:main", "m1")

	if len(rec.calls) != 1 || rec.calls[0].success || rec.calls[0].errMsg != "run failed: cancelled" {
		t.Fatalf("run error was not recorded as failure: %+v", rec.calls)
	}
}

func TestRecordRunSkillUsageIgnoresEmptyAndNilInputs(t *testing.T) {
	// Nil recorder must not panic.
	recordRunSkillUsage(nil, NewSkillConsultLog(), nil, nil, "s", "m1")

	// Nothing consulted → no records.
	rec := &fakeUsageRecorder{}
	recordRunSkillUsage(rec, NewSkillConsultLog(), &agent.AgentResult{Text: "done"}, nil, "s", "m1")
	if len(rec.calls) != 0 {
		t.Fatalf("no-consult turn recorded %+v, want none", rec.calls)
	}

	// A skill is attributed only once per run even if consulted repeatedly.
	log := NewSkillConsultLog()
	log.Add("once")
	log.Add("once")
	recordRunSkillUsage(rec, log, &agent.AgentResult{Text: "done"}, nil, "s", "m1")
	recordRunSkillUsage(rec, log, &agent.AgentResult{Text: "done"}, nil, "s", "m1")
	if len(rec.calls) != 1 || rec.calls[0].skill != "once" {
		t.Fatalf("expected single attribution for 'once', got %+v", rec.calls)
	}
}
