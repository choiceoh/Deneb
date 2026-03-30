package skills

import (
	"testing"
)

func TestShouldIncludeSkill_basic(t *testing.T) {
	entry := SkillEntry{
		Skill: Skill{Name: "test", Source: SourceWorkspace},
	}
	ctx := EligibilityContext{
		Platform:     "linux",
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
		Platform: "linux",
		EnvVars:  map[string]string{},
		SkillConfigs: map[string]SkillConfig{
			"test": {Enabled: &f},
		},
	}
	if ShouldIncludeSkill(entry, ctx) {
		t.Error("expected disabled skill to be excluded")
	}
}

func TestShouldIncludeSkill_osRestriction(t *testing.T) {
	entry := SkillEntry{
		Skill: Skill{Name: "linux-only", Source: SourceBundled},
		Metadata: &DenebSkillMetadata{
			OS: []string{"linux"},
		},
	}
	ctx := EligibilityContext{
		Platform:     "linux",
		EnvVars:      map[string]string{},
		SkillConfigs: map[string]SkillConfig{},
	}
	if !ShouldIncludeSkill(entry, ctx) {
		t.Error("expected linux-only skill to be included on linux")
	}

	ctx.Platform = "freebsd"
	if ShouldIncludeSkill(entry, ctx) {
		t.Error("expected linux-only skill to be excluded on freebsd")
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
		Platform:     "linux",
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
		Platform:     "linux",
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
		Platform: "linux",
		EnvVars:  map[string]string{},
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
		Platform:     "linux",
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

func TestFilterBySkillFilter(t *testing.T) {
	entries := []SkillEntry{
		{Skill: Skill{Name: "github"}},
		{Skill: Skill{Name: "weather"}},
		{Skill: Skill{Name: "coding-agent"}},
	}

	// nil filter = unrestricted.
	result := FilterBySkillFilter(entries, nil)
	if len(result) != 3 {
		t.Errorf("expected 3, got %d", len(result))
	}

	// Empty filter = no skills.
	result = FilterBySkillFilter(entries, []string{})
	if len(result) != 0 {
		t.Errorf("expected 0, got %d", len(result))
	}

	// Specific filter.
	result = FilterBySkillFilter(entries, []string{"github", "weather"})
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}
}
