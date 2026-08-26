package chat

import (
	"strings"
	"testing"
)

// The ambient skill catalog is filtered against the WHOLE registry
// (run_exec_skills.go: availableToolNames returns tools.SortedNames()), so a
// restricted preset that still allows the skills tool trigger-matches skills
// whose procedure needs tools it cannot reach. Injecting one hands the model a
// procedure it cannot follow — and #4783 now records a delivered-but-unfollowed
// skill as that SKILL's failure, so any preset that both allows skills and
// records usage would charge its own restriction to the skill.
func TestSkillNeedingUnavailableToolsIsNotInjected(t *testing.T) {
	params := RunParams{SessionKey: "client:main", Message: "계약서 검토"}

	// contract-review requires graphify; self-review allows the skills tool but
	// reaches only fetch_tools/skills/skill_lifecycle.
	out, names, loaded := buildSkillHints(params, "self-review", hintSkills(), nil)
	if out != "" || len(names) != 0 || len(loaded) != 0 {
		t.Fatalf("unrunnable skill must not be injected: %q %v %v", out, names, loaded)
	}
}

func TestToollessSkillSurvivesAScopedPreset(t *testing.T) {
	params := RunParams{SessionKey: "client:main", Message: "회의록 정리해줘"}

	out, names, _ := buildSkillHints(params, "self-review", hintSkills(), nil)
	if !strings.Contains(out, "meeting-minutes") || len(names) == 0 {
		t.Fatalf("a skill that declares no tools stays available: %q %v", out, names)
	}
}

func TestUnrestrictedPresetKeepsEverySkill(t *testing.T) {
	params := RunParams{SessionKey: "client:main", Message: "계약서 검토"}

	if out, _, _ := buildSkillHints(params, "", hintSkills(), nil); !strings.Contains(out, "contract-review") {
		t.Fatalf("no allow-list means no restriction: %q", out)
	}
}
