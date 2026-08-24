package chat

import (
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

func TestRecordRunSkillUsageReturnsSuccessForCleanTurn(t *testing.T) {
	rec := &fakeUsageRecorder{}
	log := NewSkillConsultLog()
	log.Add("research-flow")
	recordRunSkillUsage(rec, log, &agent.AgentResult{
		Text:           "done",
		ToolActivities: []agent.ToolActivity{{Name: "skills"}, {Name: "read"}},
	}, nil, "client:main", "m1", nil)

	if len(rec.calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(rec.calls), rec.calls)
	}
	c := rec.calls[0]
	if c.skill != "research-flow" || !c.success || c.errMsg != "" || c.session != "client:main" || c.model != "m1" {
		t.Fatalf("unexpected call: %+v", c)
	}
}

func TestRecordRunSkillUsage_erroredTurnIsFailure(t *testing.T) {
	rec := &fakeUsageRecorder{}
	log := NewSkillConsultLog()
	log.Add("deploy-flow")
	recordRunSkillUsage(rec, log, &agent.AgentResult{
		Text:           "done",
		ToolActivities: []agent.ToolActivity{{Name: "skills"}, {Name: "exec", IsError: true}},
	}, nil, "client:main", "m1", nil)

	if len(rec.calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(rec.calls), rec.calls)
	}
	c := rec.calls[0]
	if c.success {
		t.Fatalf("turn with a tool error must record failure: %+v", c)
	}
	if c.errMsg == "" {
		t.Fatalf("failure should carry an error message naming the tool: %+v", c)
	}
}

func TestRecordRunSkillUsage_skillsToolErrorIsNotSkillFailure(t *testing.T) {
	// When the "skills" tool itself errors, the consult mechanism failed to load
	// the skill (a gateway path/catalog bug) — that is not the skill performing
	// badly, so the consulted skill must NOT be recorded as a failure. Otherwise
	// the evolver pins it below its success-rate threshold and re-evolves it
	// forever chasing a gateway error it cannot fix.
	// (Session key must be a REAL session: review-fork sessions
	// ("system:skill-review:*") are now excluded from recording entirely —
	// see TestRecordRunSkillUsageIgnoresReviewForks.)
	rec := &fakeUsageRecorder{}
	log := NewSkillConsultLog()
	log.Add("email-analysis")
	recordRunSkillUsage(rec, log, &agent.AgentResult{
		Text:           "done",
		ToolActivities: []agent.ToolActivity{{Name: "skills", IsError: true}},
	}, nil, "client:main", "m1", nil)

	if len(rec.calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(rec.calls), rec.calls)
	}
	c := rec.calls[0]
	if !c.success || c.errMsg != "" {
		t.Fatalf("a skills-tool error must not be attributed to the skill: %+v", c)
	}
}

func TestRecordRunSkillUsage_nonSkillsErrorStillFailsAlongsideSkills(t *testing.T) {
	// A genuine non-"skills" tool error is still a real failure even when the
	// skills tool also appears in the turn's activities.
	rec := &fakeUsageRecorder{}
	log := NewSkillConsultLog()
	log.Add("deploy-flow")
	recordRunSkillUsage(rec, log, &agent.AgentResult{
		Text:           "done",
		ToolActivities: []agent.ToolActivity{{Name: "skills", IsError: true}, {Name: "exec", IsError: true}},
	}, nil, "client:main", "m1", nil)

	if len(rec.calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(rec.calls), rec.calls)
	}
	if c := rec.calls[0]; c.success || c.errMsg == "" {
		t.Fatalf("a non-skills tool error must still record failure: %+v", c)
	}
}

func TestRecordRunSkillUsageIgnoresEmptyAndNilInputs(t *testing.T) {
	// Nil recorder must not panic.
	recordRunSkillUsage(nil, NewSkillConsultLog(), nil, nil, "s", "m1", nil)

	// Nothing consulted → no records.
	rec := &fakeUsageRecorder{}
	recordRunSkillUsage(rec, NewSkillConsultLog(), nil, nil, "s", "m1", nil)
	if len(rec.calls) != 0 {
		t.Fatalf("no-consult turn recorded %+v, want none", rec.calls)
	}

	// A skill is attributed only once per turn even if its consult is drained;
	// a second call with nothing new drains empty.
	log := NewSkillConsultLog()
	log.Add("once")
	recordRunSkillUsage(rec, log, &agent.AgentResult{Text: "done"}, nil, "s", "m1", nil)
	recordRunSkillUsage(rec, log, &agent.AgentResult{Text: "done"}, nil, "s", "m1", nil)
	if len(rec.calls) != 1 || rec.calls[0].skill != "once" {
		t.Fatalf("expected single attribution for 'once', got %+v", rec.calls)
	}
}
