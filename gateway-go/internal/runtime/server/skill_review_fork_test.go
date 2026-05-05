package server

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
)

func TestBuildSkillReviewPromptPinsHermesDecisionOrderAndTargetSession(t *testing.T) {
	prompt := buildSkillReviewPrompt("telegram:direct:42", genesis.SessionContext{
		Turns: 3,
		ToolActivities: []genesis.ToolActivity{
			{Name: "read"},
			{Name: "exec", IsError: true},
			{Name: "exec"},
		},
		AllText: "user: scope correction\nassistant: fixed it",
	})

	for _, want := range []string{
		"Target session key: telegram:direct:42",
		"Tool summary: exec:2 (1 errors), read:1",
		"User corrections about style",
		"Check whether an existing skill already covers",
		"Record exactly one lifecycle decision",
		"route=genesis with sessionKey=telegram:direct:42",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestSkillReviewSessionKeySanitizesUnsafeCharacters(t *testing.T) {
	got := skillReviewSessionKey("telegram direct/42")
	want := "system:skill-review:telegram_direct_42"
	if got != want {
		t.Fatalf("skillReviewSessionKey() = %q, want %q", got, want)
	}
}
