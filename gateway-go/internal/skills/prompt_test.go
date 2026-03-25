package skills

import (
	"strings"
	"testing"
)

func TestBuildSkillsPrompt_empty(t *testing.T) {
	result := BuildSkillsPrompt(nil, DefaultSkillsLimits())
	if result.Prompt != "" {
		t.Errorf("expected empty prompt, got %q", result.Prompt)
	}
	if result.Count != 0 {
		t.Errorf("expected count 0, got %d", result.Count)
	}
}

func TestBuildSkillsPrompt_fullFormat(t *testing.T) {
	skills := []PromptSkill{
		{Name: "github", Description: "GitHub CLI operations", FilePath: "~/skills/github/SKILL.md"},
		{Name: "weather", Description: "Weather info", FilePath: "~/skills/weather/SKILL.md"},
	}
	result := BuildSkillsPrompt(skills, DefaultSkillsLimits())

	if !strings.Contains(result.Prompt, "<available_skills>") {
		t.Error("expected <available_skills> tag")
	}
	if !strings.Contains(result.Prompt, "github") {
		t.Error("expected github in prompt")
	}
	if !strings.Contains(result.Prompt, "GitHub CLI operations") {
		t.Error("expected description in full format")
	}
	if result.Compact {
		t.Error("expected full format, not compact")
	}
	if result.Truncated {
		t.Error("expected no truncation")
	}
	if result.Count != 2 {
		t.Errorf("expected count 2, got %d", result.Count)
	}
}

func TestBuildSkillsPrompt_compactFallback(t *testing.T) {
	// Create many skills that exceed the full format budget.
	var manySkills []PromptSkill
	for i := 0; i < 200; i++ {
		manySkills = append(manySkills, PromptSkill{
			Name:        strings.Repeat("x", 50),
			Description: strings.Repeat("description text ", 20),
			FilePath:    strings.Repeat("/path/to/skill/", 5) + "SKILL.md",
		})
	}
	limits := DefaultSkillsLimits()
	limits.MaxSkillsPromptChars = 5000

	result := BuildSkillsPrompt(manySkills, limits)
	if !result.Compact {
		t.Error("expected compact format")
	}
	if !result.Truncated {
		t.Error("expected truncation")
	}
}

func TestBuildSkillsPrompt_disabledModelInvocationExcluded(t *testing.T) {
	skills := []PromptSkill{
		{Name: "visible", Description: "visible", FilePath: "/a/SKILL.md"},
		{Name: "hidden", Description: "hidden", FilePath: "/b/SKILL.md", DisableModelInvocation: true},
	}
	result := BuildSkillsPrompt(skills, DefaultSkillsLimits())
	if result.Count != 1 {
		t.Errorf("expected 1 visible skill, got %d", result.Count)
	}
	if strings.Contains(result.Prompt, "hidden") {
		t.Error("expected hidden skill to be excluded from prompt")
	}
}

func TestCompactSkillPaths(t *testing.T) {
	skills := []PromptSkill{
		{Name: "test", FilePath: "/tmp/other/SKILL.md"},
	}
	result := CompactSkillPaths(skills)
	// /tmp/other should not be compacted (not under home).
	if result[0].FilePath != "/tmp/other/SKILL.md" {
		t.Errorf("non-home path should not be compacted: %q", result[0].FilePath)
	}
}

func TestEscapeXml(t *testing.T) {
	input := `<test & "thing" 'here'>`
	expected := "&lt;test &amp; &quot;thing&quot; &apos;here&apos;&gt;"
	if got := escapeXml(input); got != expected {
		t.Errorf("escapeXml(%q) = %q, want %q", input, got, expected)
	}
}

func TestBuildTruncationNote(t *testing.T) {
	// No truncation, no compact.
	note := BuildTruncationNote(PromptResult{}, 10)
	if note != "" {
		t.Errorf("expected empty note, got %q", note)
	}

	// Truncated + compact.
	note = BuildTruncationNote(PromptResult{Truncated: true, Compact: true, Count: 5}, 20)
	if !strings.Contains(note, "5 of 20") {
		t.Errorf("expected '5 of 20' in note, got %q", note)
	}
	if !strings.Contains(note, "compact format") {
		t.Errorf("expected 'compact format' in note, got %q", note)
	}
}
