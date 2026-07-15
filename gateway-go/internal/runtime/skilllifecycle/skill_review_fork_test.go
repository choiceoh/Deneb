package skilllifecycle

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle/leafbind"
)

// The review runs with a dedicated lean system prompt instead of the full
// main-session assembly (context files, memory, skills index ≈ 50K tokens).
// Everything the run needs must therefore live in the system preamble or the
// review prompt itself.
func TestSkillReviewSystemPromptIncludesToolsWithinSizeBoundary(t *testing.T) {
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
// template must tell the reviewer to fetch it via the skills tool, and to fall
// back conservatively when the listing is empty/unavailable (nil snapshot
// after a restart, always-on skills missing from the discoverable list).
func TestBuildSkillReviewPromptIncludesIndexGuidanceAndFallback(t *testing.T) {
	prompt := buildSkillReviewPrompt("client:main", leafbind.SessionContext{AllText: "user: 테스트"}, "(none)")
	if !strings.Contains(prompt, "skills action=list") {
		t.Error("prompt missing skills-index guidance")
	}
	if !strings.Contains(prompt, "decide conservatively") {
		t.Error("prompt missing empty-listing fallback discipline")
	}
	if !strings.Contains(prompt, "user: 테스트") {
		t.Error("prompt missing embedded transcript")
	}
}

// The fork session key derives a persisted `system:skill-review:*` identity
// from arbitrary session keys — unsafe runes must be folded to '_'.
func TestSkillReviewSessionKeyNormalizesUnsafeRunes(t *testing.T) {
	cases := map[string]string{
		"client:main":     "system:skill-review:client:main",
		"cron:작업/1 테스트":   "system:skill-review:cron:작업_1_테스트",
		"a b\tc\nd(e)f.g": "system:skill-review:a_b_c_d_e_f_g",
	}
	for in, want := range cases {
		if got := skillReviewSessionKey(in); got != want {
			t.Errorf("skillReviewSessionKey(%q) = %q, want %q", in, got, want)
		}
	}
}
