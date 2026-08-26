package chat

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

func cardSkill() []skills.PromptSkill {
	return []skills.PromptSkill{{
		Name:        "deneb-ui-authoring",
		Description: "card grammar",
		Triggers:    []string{"카드"},
		Body:        "카드 문법 본문",
	}}
}

// The tail register keeps every earlier copy of an auto-loaded body on the
// wire, so re-injecting the same static document once per matching turn only
// grows the request (measured: 3 copies, 16KB of a 92KB request).
func TestAutoLoadedSkillIsNotSentTwiceInOneSession(t *testing.T) {
	params := RunParams{SessionKey: "client:main", Message: "카드로 정리해줘"}

	first, names, autoLoaded := buildSkillHints(params, "", cardSkill(), nil)
	if !strings.Contains(first, "카드 문법 본문") || len(names) != 1 || len(autoLoaded) != 1 {
		t.Fatalf("first turn must auto-load the body: %q %v %v", first, names, autoLoaded)
	}

	history := []llm.Message{{Role: "user", Content: llm.FlexibleFromValue(first)}}
	second, names2, autoLoaded2 := buildSkillHints(params, "", cardSkill(), skillBodiesInHistory(history))
	if second != "" {
		t.Fatalf("second turn must not repeat the body: %q", second)
	}
	// The skill still counts as loaded for this turn: its instructions are in
	// context, so the tools it declares (RequiresTools) must keep being
	// activated — dropping the skill entirely made morning_letter vanish while
	// the history still told the model to use it.
	if len(names2) != 1 || len(autoLoaded2) != 1 {
		t.Fatalf("skill must stay active for tool activation: %v %v", names2, autoLoaded2)
	}
}

func TestSkillBodiesInHistoryReadsTheMarkersBuildSkillHintsWrites(t *testing.T) {
	params := RunParams{SessionKey: "client:main", Message: "카드로 정리해줘"}
	hints, _, _ := buildSkillHints(params, "", cardSkill(), nil)

	present := skillBodiesInHistory([]llm.Message{{Role: "user", Content: llm.FlexibleFromValue(hints)}})

	if !present["deneb-ui-authoring"] {
		t.Fatalf("marker round-trip broken, parsed %v from:\n%s", present, hints)
	}
	if len(present) != 1 {
		t.Fatalf("unexpected extra names: %v", present)
	}
}
