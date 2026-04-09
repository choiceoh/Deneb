package skills

import (
	"strings"
	"testing"
)

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
		t.Errorf("got %d, want count 2", result.Count)
	}
}

func TestBuildSkillsPrompt_compactFallback(t *testing.T) {
	// Create many skills that exceed the full format budget.
	var manySkills []PromptSkill
	for range 200 {
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
		t.Errorf("got %d, want 1 visible skill", result.Count)
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
	if got := escapeXML(input); got != expected {
		t.Errorf("escapeXML(%q) = %q, want %q", input, got, expected)
	}
}

func TestBuildSkillsPrompt_categoryIncluded(t *testing.T) {
	skills := []PromptSkill{
		{Name: "tmux", Description: "Terminal multiplexer", FilePath: "~/skills/tmux/SKILL.md", Category: "devops"},
		{Name: "weather", Description: "Weather info", FilePath: "~/skills/weather/SKILL.md"},
	}
	result := BuildSkillsPrompt(skills, DefaultSkillsLimits())

	if !strings.Contains(result.Prompt, "<category>devops</category>") {
		t.Error("expected <category>devops</category> in prompt for tmux skill")
	}
	// Weather has no category — should not have a category tag.
	if strings.Contains(result.Prompt, "weather") && strings.Count(result.Prompt, "<category>") != 1 {
		t.Error("expected only 1 category tag (for tmux), weather should have none")
	}
}

func TestBuildSkillsPrompt_tagsAndRelatedSkills(t *testing.T) {
	skills := []PromptSkill{
		{
			Name:          "weather",
			Description:   "Weather info",
			FilePath:      "~/skills/integration/weather/SKILL.md",
			Category:      "integration",
			Tags:          []string{"weather", "forecast", "temperature"},
			RelatedSkills: []string{"morning-letter"},
		},
	}
	result := BuildSkillsPrompt(skills, DefaultSkillsLimits())

	if !strings.Contains(result.Prompt, "<tags>weather, forecast, temperature</tags>") {
		t.Error("expected <tags> in prompt")
	}
	if !strings.Contains(result.Prompt, "<related_skills>morning-letter</related_skills>") {
		t.Error("expected <related_skills> in prompt")
	}
}

func TestFormatSkillsListResponse_tagFilter(t *testing.T) {
	skills := []PromptSkill{
		{Name: "weather", Description: "Weather info", Category: "integration", Tags: []string{"forecast", "temperature"}},
		{Name: "github", Description: "GitHub CLI", Category: "coding", Tags: []string{"git", "PR"}},
	}

	// Query "forecast" should match weather via tag.
	result := FormatSkillsListResponse(skills, "forecast", "")
	if !strings.Contains(result, "weather") {
		t.Error("expected weather to match 'forecast' tag query")
	}
	if strings.Contains(result, "github") {
		t.Error("expected github to NOT match 'forecast' tag query")
	}

	// Query "PR" should match github via tag.
	result = FormatSkillsListResponse(skills, "PR", "")
	if !strings.Contains(result, "github") {
		t.Error("expected github to match 'PR' tag query")
	}
}
