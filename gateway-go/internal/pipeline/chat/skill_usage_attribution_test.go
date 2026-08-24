package chat

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

type attributedCall struct {
	skill string
	attr  chatport.SkillUseAttribution
}

type fakeAttributingRecorder struct{ calls []attributedCall }

func (f *fakeAttributingRecorder) RecordSkillUse(string, string, bool, string, string) {
	f.calls = append(f.calls, attributedCall{skill: "(plain)"})
}

func (f *fakeAttributingRecorder) RecordSkillUseAttributed(_, skillName string, _ bool, _, _ string, attr chatport.SkillUseAttribution) {
	f.calls = append(f.calls, attributedCall{skill: skillName, attr: attr})
}

// seedResolvedSkills installs a snapshot so requires_tools is resolvable, and
// restores the previous one — the snapshot is process-wide.
func seedResolvedSkills(t *testing.T, entries ...skills.PromptSkill) {
	t.Helper()
	skillsCache.mu.Lock()
	prev := skillsCache.snapshot
	skillsCache.snapshot = &skills.FullSkillSnapshot{ResolvedSkills: entries}
	skillsCache.mu.Unlock()
	t.Cleanup(func() {
		skillsCache.mu.Lock()
		skillsCache.snapshot = prev
		skillsCache.mu.Unlock()
	})
}

// The core distinction: two identical failed turns, one where the skill's tool
// ran and one where it did not, must not be recorded the same way.
func TestSkillUseAttributionSeparatesExercisedFromIgnored(t *testing.T) {
	seedResolvedSkills(t, skills.PromptSkill{Name: "kb", RequiresTools: []string{"wiki"}})

	exercised := skillUseAttribution("kb", map[string]bool{"kb": true},
		&agent.AgentResult{ToolActivities: []agent.ToolActivity{{Name: "wiki"}}})
	if exercised.Exercised != chatport.SkillExercisedYes {
		t.Errorf("declared tool ran but attribution = %+v", exercised)
	}
	if exercised.Delivery != chatport.SkillDeliveryAutoLoad {
		t.Errorf("trigger-loaded skill recorded as %q", exercised.Delivery)
	}

	ignored := skillUseAttribution("kb", nil,
		&agent.AgentResult{ToolActivities: []agent.ToolActivity{{Name: "exec"}}})
	if ignored.Exercised != chatport.SkillExercisedNo {
		t.Errorf("no declared tool ran but attribution = %+v", ignored)
	}
	if ignored.Delivery != chatport.SkillDeliveryModelRead {
		t.Errorf("model-read skill recorded as %q", ignored.Delivery)
	}
}

// A skill declaring no tools has nothing to check against. Guessing either way
// would manufacture evidence, so it must stay unknown.
func TestSkillUseAttributionUnknownWithoutDeclaredTools(t *testing.T) {
	seedResolvedSkills(t, skills.PromptSkill{Name: "prose"})
	got := skillUseAttribution("prose", nil, &agent.AgentResult{
		ToolActivities: []agent.ToolActivity{{Name: "exec"}},
	})
	if got.Exercised != chatport.SkillExercisedUnknown {
		t.Errorf("toolless skill = %+v, want unknown", got)
	}
}

// A recorder that implements the attribution capability must receive it;
// the plain path is only for recorders that do not.
func TestRecordRunSkillUsagePrefersAttributedRecorder(t *testing.T) {
	seedResolvedSkills(t, skills.PromptSkill{Name: "kb", RequiresTools: []string{"wiki"}})
	rec := &fakeAttributingRecorder{}
	log := NewSkillConsultLog()
	log.Add("kb")

	recordRunSkillUsage(rec, log, &agent.AgentResult{
		Text:           "done",
		ToolActivities: []agent.ToolActivity{{Name: "exec"}},
	}, nil, "client:main", "m1", map[string]bool{"kb": true})

	if len(rec.calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(rec.calls), rec.calls)
	}
	c := rec.calls[0]
	if c.skill != "kb" {
		t.Fatalf("plain path taken despite attribution support: %+v", c)
	}
	if c.attr.Exercised != chatport.SkillExercisedNo || c.attr.Delivery != chatport.SkillDeliveryAutoLoad {
		t.Errorf("attribution = %+v, want auto-load/no", c.attr)
	}
}
