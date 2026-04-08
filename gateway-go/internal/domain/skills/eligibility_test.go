package skills

import (
	"testing"
)

func TestShouldIncludeSkill_basic(t *testing.T) {
	entry := SkillEntry{
		Skill: Skill{Name: "test", Source: SourceWorkspace},
	}
	ctx := EligibilityContext{
		EnvVars:      map[string]string{},
		SkillConfigs: map[string]SkillConfig{},
	}
	if !ShouldIncludeSkill(entry, ctx) {
		t.Error("expected basic skill to be included")
	}
}

func TestShouldIncludeSkill_disabled(t *testing.T) {
	f := false
	entry := SkillEntry{
		Skill: Skill{Name: "test", Source: SourceWorkspace},
	}
	ctx := EligibilityContext{
		EnvVars: map[string]string{},
		SkillConfigs: map[string]SkillConfig{
			"test": {Enabled: &f},
		},
	}
	if ShouldIncludeSkill(entry, ctx) {
		t.Error("expected disabled skill to be excluded")
	}
}

func TestShouldIncludeSkill_alwaysFlag(t *testing.T) {
	entry := SkillEntry{
		Skill: Skill{Name: "always-on", Source: SourceBundled},
		Metadata: &DenebSkillMetadata{
			Always: true,
			Requires: &SkillRequires{
				Bins: []string{"nonexistent-binary-xyz"},
			},
		},
	}
	ctx := EligibilityContext{
		EnvVars:      map[string]string{},
		SkillConfigs: map[string]SkillConfig{},
	}
	if !ShouldIncludeSkill(entry, ctx) {
		t.Error("expected always-on skill to be included despite missing binary")
	}
}

func TestShouldIncludeSkill_envRequired(t *testing.T) {
	entry := SkillEntry{
		Skill: Skill{Name: "needs-token", Source: SourceBundled},
		Metadata: &DenebSkillMetadata{
			PrimaryEnv: "GITHUB_TOKEN",
			Requires: &SkillRequires{
				Env: []string{"GITHUB_TOKEN"},
			},
		},
	}
	// No env var set.
	ctx := EligibilityContext{
		EnvVars:      map[string]string{},
		SkillConfigs: map[string]SkillConfig{},
	}
	if ShouldIncludeSkill(entry, ctx) {
		t.Error("expected skill to be excluded without GITHUB_TOKEN")
	}

	// Set env var.
	ctx.EnvVars["GITHUB_TOKEN"] = "ghp_xxx"
	if !ShouldIncludeSkill(entry, ctx) {
		t.Error("expected skill to be included with GITHUB_TOKEN set")
	}
}

func TestShouldIncludeSkill_apiKeyMatchesPrimaryEnv(t *testing.T) {
	entry := SkillEntry{
		Skill: Skill{Name: "needs-token", Source: SourceBundled},
		Metadata: &DenebSkillMetadata{
			PrimaryEnv: "GITHUB_TOKEN",
			Requires: &SkillRequires{
				Env: []string{"GITHUB_TOKEN"},
			},
		},
	}
	ctx := EligibilityContext{
		EnvVars: map[string]string{},
		SkillConfigs: map[string]SkillConfig{
			"needs-token": {APIKey: "ghp_secret"},
		},
	}
	if !ShouldIncludeSkill(entry, ctx) {
		t.Error("expected skill to be included when apiKey matches primaryEnv")
	}
}

func TestShouldIncludeSkill_bundledAllowlist(t *testing.T) {
	entry := SkillEntry{
		Skill: Skill{Name: "github", Source: SourceBundled},
	}
	ctx := EligibilityContext{
		EnvVars:      map[string]string{},
		SkillConfigs: map[string]SkillConfig{},
		AllowBundled: []string{"weather"},
	}
	if ShouldIncludeSkill(entry, ctx) {
		t.Error("expected bundled skill not in allowlist to be excluded")
	}

	ctx.AllowBundled = []string{"github", "weather"}
	if !ShouldIncludeSkill(entry, ctx) {
		t.Error("expected bundled skill in allowlist to be included")
	}
}

func TestShouldIncludeSkill_requiresTools(t *testing.T) {
	entry := SkillEntry{
		Skill: Skill{Name: "terminal-skill", Source: SourceWorkspace},
		Metadata: &DenebSkillMetadata{
			RequiresTools: []string{"exec", "terminal"},
		},
	}
	// Tools available — should be included.
	ctx := EligibilityContext{
		EnvVars:        map[string]string{},
		SkillConfigs:   map[string]SkillConfig{},
		AvailableTools: map[string]struct{}{"exec": {}, "terminal": {}, "read": {}},
	}
	if !ShouldIncludeSkill(entry, ctx) {
		t.Error("expected skill to be included when required tools are available")
	}

	// Missing tool — should be excluded.
	ctx.AvailableTools = map[string]struct{}{"exec": {}, "read": {}}
	if ShouldIncludeSkill(entry, ctx) {
		t.Error("expected skill to be excluded when required tool is missing")
	}
}

func TestShouldIncludeSkill_fallbackForTools(t *testing.T) {
	entry := SkillEntry{
		Skill: Skill{Name: "curl-search", Source: SourceWorkspace},
		Metadata: &DenebSkillMetadata{
			FallbackForTools: []string{"web_search"},
		},
	}
	// web_search available — fallback should be hidden.
	ctx := EligibilityContext{
		EnvVars:        map[string]string{},
		SkillConfigs:   map[string]SkillConfig{},
		AvailableTools: map[string]struct{}{"web_search": {}, "read": {}},
	}
	if ShouldIncludeSkill(entry, ctx) {
		t.Error("expected fallback skill to be hidden when target tool is available")
	}

	// web_search NOT available — fallback should show.
	ctx.AvailableTools = map[string]struct{}{"read": {}, "exec": {}}
	if !ShouldIncludeSkill(entry, ctx) {
		t.Error("expected fallback skill to show when target tool is unavailable")
	}
}

func TestFilterBySkillFilter(t *testing.T) {
	entries := []SkillEntry{
		{Skill: Skill{Name: "github"}},
		{Skill: Skill{Name: "weather"}},
		{Skill: Skill{Name: "coding-agent"}},
	}

	// nil filter = unrestricted.
	result := FilterBySkillFilter(entries, nil)
	if len(result) != 3 {
		t.Errorf("got %d, want 3", len(result))
	}

	// Empty filter = no skills.
	result = FilterBySkillFilter(entries, []string{})
	if len(result) != 0 {
		t.Errorf("got %d, want 0", len(result))
	}

	// Specific filter.
	result = FilterBySkillFilter(entries, []string{"github", "weather"})
	if len(result) != 2 {
		t.Errorf("got %d, want 2", len(result))
	}
}
