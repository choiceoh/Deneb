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
