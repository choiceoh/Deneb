package chat

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

// The read fallback promises a procedure ("필요한 절차만 로드하라"). A skill the
// JIT loader produced no body for has none to give: a frontmatter-only
// SKILL.md returns its own frontmatter, and a local/system skill's body is
// withheld on purpose because it is something to invoke, not to follow.
func TestSkillHintsDropMatchesWithNoLoadableBody(t *testing.T) {
	params := RunParams{SessionKey: "client:test", Message: "빈본문프로브 실행해줘"}
	resolved := []skills.PromptSkill{{
		Name: "empty-probe", Description: "빈 본문 프로브",
		Triggers: []string{"빈본문프로브"}, Body: "",
	}}

	block, hinted, autoLoaded := buildSkillHints(params, "", resolved, nil)
	if block != "" {
		t.Errorf("a bodyless skill was advertised:\n%s", block)
	}
	// It must not be recorded as injected either — otherwise a skill that could
	// never have been followed is blamed for being ignored.
	if len(hinted) != 0 || len(autoLoaded) != 0 {
		t.Errorf("hinted=%v autoLoaded=%v, want both empty", hinted, autoLoaded)
	}
}

// A skill that DOES have a body still gets injected — the drop must not swallow
// the working case.
func TestSkillHintsStillInjectSkillsThatHaveBodies(t *testing.T) {
	params := RunParams{SessionKey: "client:test", Message: "빈본문프로브 실행해줘"}
	resolved := []skills.PromptSkill{{
		Name: "real-probe", Description: "본문 있는 프로브",
		Triggers: []string{"빈본문프로브"}, Body: "1. 첫 단계\n2. 둘째 단계\n",
	}}

	block, hinted, autoLoaded := buildSkillHints(params, "", resolved, nil)
	if !strings.Contains(block, "첫 단계") {
		t.Errorf("a skill with a body was not injected:\n%s", block)
	}
	if len(hinted) != 1 || len(autoLoaded) != 1 {
		t.Errorf("hinted=%v autoLoaded=%v, want one each", hinted, autoLoaded)
	}
}

// A mix keeps the good one and drops the empty one, rather than dropping the
// whole block.
func TestSkillHintsKeepTheUsableHalfOfAMixedMatch(t *testing.T) {
	params := RunParams{SessionKey: "client:test", Message: "빈본문프로브 실행해줘"}
	resolved := []skills.PromptSkill{
		{Name: "empty-probe", Triggers: []string{"빈본문프로브"}, Body: "   "},
		{Name: "real-probe", Triggers: []string{"빈본문프로브"}, Body: "1. 첫 단계\n"},
	}

	block, hinted, _ := buildSkillHints(params, "", resolved, nil)
	if strings.Contains(block, "empty-probe") {
		t.Errorf("bodyless skill survived a mixed match:\n%s", block)
	}
	if !strings.Contains(block, "첫 단계") {
		t.Errorf("usable skill was dropped with it:\n%s", block)
	}
	if len(hinted) != 1 || hinted[0] != "real-probe" {
		t.Errorf("hinted = %v, want [real-probe]", hinted)
	}
}
