package prompt

import (
	"strings"
	"testing"
)

func staticPromptFor(preset string) string {
	params := SystemPromptParams{
		ToolPreset: preset,
		ToolDefs:   []ToolDef{{Name: "read"}, {Name: "grep"}},
	}
	eager := toolNameSet{"read": {}, "grep": {}}
	return buildStaticPrompt(params, eager, eager)
}

// A spawned sub-agent reports to its parent as tool text — the card grammar is
// dead weight there, and a card it authored would surface only as quoted text.
func TestRichAnswerContractIsSkippedForSpawnPresets(t *testing.T) {
	main := staticPromptFor("")
	if !strings.Contains(main, "### Rich answers") {
		t.Fatal("the normal run must keep the rich-answer contract")
	}

	for _, preset := range []string{"researcher", "implementer", "verifier"} {
		sub := staticPromptFor(preset)
		if strings.Contains(sub, "### Rich answers") {
			t.Fatalf("%s must not get the card grammar", preset)
		}
		if !strings.Contains(sub, "부모 에이전트") {
			t.Fatalf("%s must be told where its reply goes:\n%s", preset, sub)
		}
		if len(sub) >= len(main) {
			t.Fatalf("%s prompt (%d) should be smaller than main (%d)", preset, len(sub), len(main))
		}
	}
}
