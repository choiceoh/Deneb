package skills

import (
	"strings"
	"testing"
)

func TestBuildSkillsIndexRendersNamesOnly(t *testing.T) {
	in := []PromptSkill{{
		Name:          "release",
		Description:   "Release a new version",
		FilePath:      "/path/SKILL.md",
		Category:      "devops",
		Tags:          []string{"git", "tag"},
		RelatedSkills: []string{"landpr"},
		Body:          "SECRET PROCEDURE BODY",
	}}
	result := BuildSkillsIndex(in, DefaultSkillsLimits())

	for _, want := range []string{
		"<available_skills>",
		"- release",
		"</available_skills>",
	} {
		if !strings.Contains(result.Prompt, want) {
			t.Errorf("missing %q in index prompt: %s", want, result.Prompt)
		}
	}
	for _, forbid := range []string{"<skill>", "<name>", "<description>", "<location>", "/path/SKILL.md", "Release a new version", "devops", "git", "landpr", "SECRET PROCEDURE BODY"} {
		if strings.Contains(result.Prompt, forbid) {
			t.Errorf("%s leaked into name-only index: %s", forbid, result.Prompt)
		}
	}
	if result.Compact {
		t.Errorf("expected non-compact path for tiny input")
	}
	if result.Count != 1 {
		t.Errorf("count = %d, want 1", result.Count)
	}
}

func TestBuildSkillsIndex_EmptyReturnsEmpty(t *testing.T) {
	if got := BuildSkillsIndex(nil, DefaultSkillsLimits()); got.Prompt != "" {
		t.Errorf("nil input should yield empty prompt, got %q", got.Prompt)
	}
	if got := BuildSkillsIndex([]PromptSkill{}, DefaultSkillsLimits()); got.Prompt != "" {
		t.Errorf("empty slice should yield empty prompt, got %q", got.Prompt)
	}
}

func TestBuildSkillsIndexExcludesSkillsWithDisabledModelInvocation(t *testing.T) {
	in := []PromptSkill{
		{Name: "visible", FilePath: "/p1", Description: "shown"},
		{Name: "hidden", FilePath: "/p2", Description: "skip me", DisableModelInvocation: true},
	}
	result := BuildSkillsIndex(in, DefaultSkillsLimits())
	if !strings.Contains(result.Prompt, "visible") {
		t.Error("visible skill missing from index")
	}
	if strings.Contains(result.Prompt, "hidden") {
		t.Error("DisableModelInvocation skill leaked into index")
	}
}

func TestBuildSkillsIndexReturnsByteIdenticalOutputAcrossCalls(t *testing.T) {
	// The semi-static cache invariant relies on byte-identical output for
	// identical input. Two calls with the same skill list must produce the
	// same prompt bytes (no timestamps, no map iteration order, no random).
	in := []PromptSkill{
		{Name: "a", FilePath: "/p/a", Description: "first"},
		{Name: "b", FilePath: "/p/b", Description: "second"},
	}
	r1 := BuildSkillsIndex(in, DefaultSkillsLimits())
	r2 := BuildSkillsIndex(in, DefaultSkillsLimits())
	if r1.Prompt != r2.Prompt {
		t.Fatalf("non-deterministic output:\nr1=%q\nr2=%q", r1.Prompt, r2.Prompt)
	}
}

func TestBuildSkillsIndexTruncatesNameManifestAtBudget(t *testing.T) {
	longA := strings.Repeat("a", 200)
	longB := strings.Repeat("b", 200)
	in := []PromptSkill{
		{Name: longA, FilePath: "/p/a", Description: strings.Repeat("X", 1000)},
		{Name: longB, FilePath: "/p/b", Description: strings.Repeat("Y", 1000)},
	}
	limits := SkillsLimits{
		MaxSkillsInPrompt:    150,
		MaxSkillsPromptChars: 400,
	}
	result := BuildSkillsIndex(in, limits)
	if result.Compact || !result.Truncated {
		t.Errorf("expected canonical truncated manifest, got compact=%v truncated=%v len=%d", result.Compact, result.Truncated, len(result.Prompt))
	}
	if result.Count != 1 {
		t.Errorf("count = %d, want one name within budget", result.Count)
	}
	if !strings.Contains(result.Prompt, longA) || strings.Contains(result.Prompt, longB) {
		t.Errorf("budgeted manifest should keep only the deterministic prefix: %s", result.Prompt)
	}
}

func TestBuildSkillsIndexIgnoresDescriptionAndPathChanges(t *testing.T) {
	first := BuildSkillsIndex([]PromptSkill{{Name: "same", Description: "first", FilePath: "/one/SKILL.md"}}, DefaultSkillsLimits())
	second := BuildSkillsIndex([]PromptSkill{{Name: "same", Description: "completely different", FilePath: "/two/SKILL.md"}}, DefaultSkillsLimits())
	if first.Prompt != second.Prompt {
		t.Fatalf("description/path edits must not churn ambient manifest:\nfirst=%q\nsecond=%q", first.Prompt, second.Prompt)
	}
}

func TestBuildSkillsIndexFormatsMultilineNameOntoSingleLine(t *testing.T) {
	in := []PromptSkill{{
		Name:        "first line\nsecond  line",
		Description: "ignored",
		FilePath:    "/p/SKILL.md",
	}}
	result := BuildSkillsIndex(in, DefaultSkillsLimits())
	if !strings.Contains(result.Prompt, "- first line second line") {
		t.Errorf("multi-line name should flatten onto one entry line, got: %s", result.Prompt)
	}
}

func TestBuildSkillsIndexReturnsSmallerOutputThanFullPrompt(t *testing.T) {
	// Ambient discovery must remain strictly smaller than the full searchable
	// catalog; purpose and paths load just in time through the skills tool.
	in := []PromptSkill{{
		Name:          "release",
		Description:   "Release",
		FilePath:      "/p/SKILL.md",
		Category:      "devops",
		Tags:          []string{"git", "tag", "version"},
		RelatedSkills: []string{"landpr", "changelog"},
	}}
	full := BuildSkillsPrompt(in, DefaultSkillsLimits())
	idx := BuildSkillsIndex(in, DefaultSkillsLimits())
	if len(idx.Prompt) >= len(full.Prompt) {
		t.Errorf("index (%d) should be smaller than full (%d); P5 token-saving regressed",
			len(idx.Prompt), len(full.Prompt))
	}
}
