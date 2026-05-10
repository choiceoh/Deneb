package skills

import (
	"strings"
	"testing"
)

func TestBuildSkillsIndex_OmitsExtraMetadata(t *testing.T) {
	in := []PromptSkill{{
		Name:          "release",
		Description:   "Release a new version",
		FilePath:      "/path/SKILL.md",
		Category:      "devops",
		Tags:          []string{"git", "tag"},
		RelatedSkills: []string{"landpr"},
	}}
	result := BuildSkillsIndex(in, DefaultSkillsLimits())

	for _, want := range []string{
		"<name>release</name>",
		"<description>Release a new version</description>",
		"<location>/path/SKILL.md</location>",
	} {
		if !strings.Contains(result.Prompt, want) {
			t.Errorf("missing %q in index prompt: %s", want, result.Prompt)
		}
	}
	for _, forbid := range []string{"<category>", "<tags>", "<related_skills>"} {
		if strings.Contains(result.Prompt, forbid) {
			t.Errorf("%s leaked into index format (P5 strips it): %s", forbid, result.Prompt)
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

func TestBuildSkillsIndex_DisabledModelInvocationExcluded(t *testing.T) {
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

func TestBuildSkillsIndex_ByteStableAcrossCalls(t *testing.T) {
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

func TestBuildSkillsIndex_FallsBackToCompactWhenIndexExceedsBudget(t *testing.T) {
	// Long descriptions push the index past a tight budget; the builder
	// should fall back to formatSkillsCompact (name + location only).
	long := strings.Repeat("X", 1000)
	in := []PromptSkill{
		{Name: "alpha", FilePath: "/p/a", Description: long},
		{Name: "beta", FilePath: "/p/b", Description: long},
	}
	limits := SkillsLimits{
		MaxSkillsInPrompt:    150,
		MaxSkillsPromptChars: 600, // tight enough to force compact fallback
	}
	result := BuildSkillsIndex(in, limits)
	if !result.Compact {
		t.Errorf("expected compact fallback, got Compact=false; len=%d", len(result.Prompt))
	}
	if strings.Contains(result.Prompt, "<description>") {
		t.Errorf("compact fallback should drop descriptions; got: %s", result.Prompt)
	}
}

func TestBuildSkillsIndex_SmallerThanFull(t *testing.T) {
	// P5 invariant: index is strictly smaller than the full format for any
	// skill that carries category/tags/related_skills. If this regresses,
	// P5's primary value (semi-static token reduction) is gone.
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
