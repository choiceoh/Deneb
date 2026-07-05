package server

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
)

// The review runs with a dedicated lean system prompt instead of the full
// main-session assembly (context files, memory, skills index ≈ 50K tokens).
// Everything the run needs must therefore live in the system preamble or the
// review prompt itself.
func TestSkillReviewSystemPrompt_SelfContained(t *testing.T) {
	for _, want := range []string{"fetch_tools", "skills", "skill_lifecycle", "propose"} {
		if !strings.Contains(skillReviewSystemPrompt, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	if len(skillReviewSystemPrompt) > 2000 {
		t.Errorf("system prompt should stay lean, got %d bytes", len(skillReviewSystemPrompt))
	}
}

// Without the main system prompt the skill index is no longer preloaded — the
// template must tell the reviewer to fetch it via the skills tool.
func TestBuildSkillReviewPrompt_PointsAtSkillIndex(t *testing.T) {
	prompt := buildSkillReviewPrompt("client:main", genesis.SessionContext{AllText: "user: 테스트"}, "(none)")
	if !strings.Contains(prompt, "skills action=list") {
		t.Error("prompt missing skills-index guidance")
	}
	if !strings.Contains(prompt, "user: 테스트") {
		t.Error("prompt missing embedded transcript")
	}
}
