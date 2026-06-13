package chat

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
)

type usageCall struct {
	session, skill, errMsg string
	success                bool
}

type fakeUsageRecorder struct{ calls []usageCall }

func (f *fakeUsageRecorder) RecordSkillUse(sessionKey, skillName string, success bool, errMsg string) {
	f.calls = append(f.calls, usageCall{sessionKey, skillName, errMsg, success})
}

func TestRecordTurnSkillUsage_cleanTurnIsSuccess(t *testing.T) {
	rec := &fakeUsageRecorder{}
	log := NewSkillConsultLog()
	log.Add("research-flow")
	recordTurnSkillUsage(rec, log, []agent.ToolActivity{{Name: "skills"}, {Name: "read"}}, "client:main")

	if len(rec.calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(rec.calls), rec.calls)
	}
	c := rec.calls[0]
	if c.skill != "research-flow" || !c.success || c.errMsg != "" || c.session != "client:main" {
		t.Fatalf("unexpected call: %+v", c)
	}
}

func TestRecordTurnSkillUsage_erroredTurnIsFailure(t *testing.T) {
	rec := &fakeUsageRecorder{}
	log := NewSkillConsultLog()
	log.Add("deploy-flow")
	recordTurnSkillUsage(rec, log, []agent.ToolActivity{{Name: "skills"}, {Name: "exec", IsError: true}}, "client:main")

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

func TestRecordTurnSkillUsage_skillsToolErrorIsNotSkillFailure(t *testing.T) {
	// When the "skills" tool itself errors, the consult mechanism failed to load
	// the skill (a gateway path/catalog bug) — that is not the skill performing
	// badly, so the consulted skill must NOT be recorded as a failure. Otherwise
	// the evolver pins it below its success-rate threshold and re-evolves it
	// forever chasing a gateway error it cannot fix.
	rec := &fakeUsageRecorder{}
	log := NewSkillConsultLog()
	log.Add("email-analysis")
	recordTurnSkillUsage(rec, log, []agent.ToolActivity{{Name: "skills", IsError: true}}, "system:skill-review:cron:x")

	if len(rec.calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(rec.calls), rec.calls)
	}
	c := rec.calls[0]
	if !c.success || c.errMsg != "" {
		t.Fatalf("a skills-tool error must not be attributed to the skill: %+v", c)
	}
}

func TestRecordTurnSkillUsage_nonSkillsErrorStillFailsAlongsideSkills(t *testing.T) {
	// A genuine non-"skills" tool error is still a real failure even when the
	// skills tool also appears in the turn's activities.
	rec := &fakeUsageRecorder{}
	log := NewSkillConsultLog()
	log.Add("deploy-flow")
	recordTurnSkillUsage(rec, log, []agent.ToolActivity{{Name: "skills", IsError: true}, {Name: "exec", IsError: true}}, "client:main")

	if len(rec.calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(rec.calls), rec.calls)
	}
	if c := rec.calls[0]; c.success || c.errMsg == "" {
		t.Fatalf("a non-skills tool error must still record failure: %+v", c)
	}
}

func TestRecordTurnSkillUsage_noOps(t *testing.T) {
	// Nil recorder must not panic.
	recordTurnSkillUsage(nil, NewSkillConsultLog(), nil, "s")

	// Nothing consulted → no records.
	rec := &fakeUsageRecorder{}
	recordTurnSkillUsage(rec, NewSkillConsultLog(), []agent.ToolActivity{{Name: "read"}}, "s")
	if len(rec.calls) != 0 {
		t.Fatalf("no-consult turn recorded %+v, want none", rec.calls)
	}

	// A skill is attributed only once per turn even if its consult is drained;
	// a second call with nothing new drains empty.
	log := NewSkillConsultLog()
	log.Add("once")
	recordTurnSkillUsage(rec, log, nil, "s")
	recordTurnSkillUsage(rec, log, nil, "s")
	if len(rec.calls) != 1 || rec.calls[0].skill != "once" {
		t.Fatalf("expected single attribution for 'once', got %+v", rec.calls)
	}
}
