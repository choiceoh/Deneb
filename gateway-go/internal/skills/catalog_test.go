package skills

import (
	"testing"
)

func TestResolveSkillKey(t *testing.T) {
	tests := []struct {
		name     string
		entry    SkillEntry
		expected string
	}{
		{
			name:     "uses skill name when no metadata",
			entry:    SkillEntry{Skill: Skill{Name: "weather"}},
			expected: "weather",
		},
		{
			name: "uses metadata skillKey when present",
			entry: SkillEntry{
				Skill:    Skill{Name: "weather"},
				Metadata: &DenebSkillMetadata{SkillKey: "custom-weather"},
			},
			expected: "custom-weather",
		},
		{
			name: "falls back to name when skillKey is empty",
			entry: SkillEntry{
				Skill:    Skill{Name: "github"},
				Metadata: &DenebSkillMetadata{},
			},
			expected: "github",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveSkillKey(tt.entry)
			if got != tt.expected {
				t.Errorf("ResolveSkillKey() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCatalog_RegisterAndList(t *testing.T) {
	c := NewCatalog(nil)

	c.Register(SkillEntry{Skill: Skill{Name: "weather", Source: SourceBundled}})
	c.Register(SkillEntry{Skill: Skill{Name: "github", Source: SourceWorkspace}})
	c.Register(SkillEntry{Skill: Skill{Name: "coding", Source: SourceManaged}})

	entries := c.List()
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// Should be sorted alphabetically.
	if entries[0].Skill.Name != "coding" {
		t.Errorf("expected first entry to be 'coding', got %q", entries[0].Skill.Name)
	}
	if entries[1].Skill.Name != "github" {
		t.Errorf("expected second entry to be 'github', got %q", entries[1].Skill.Name)
	}
}

func TestCatalog_BuildWorkspaceSnapshot(t *testing.T) {
	c := NewCatalog(nil)
	c.Register(SkillEntry{Skill: Skill{Name: "weather"}})
	c.Register(SkillEntry{Skill: Skill{Name: "github"}})
	c.Register(SkillEntry{Skill: Skill{Name: "coding"}})

	// nil filter = unrestricted.
	snap := c.BuildWorkspaceSnapshot(nil)
	if len(snap.Entries) != 3 {
		t.Errorf("nil filter should return all, got %d", len(snap.Entries))
	}

	// Empty filter = no skills.
	snap = c.BuildWorkspaceSnapshot([]string{})
	if len(snap.Entries) != 0 {
		t.Errorf("empty filter should return none, got %d", len(snap.Entries))
	}

	// Specific filter.
	snap = c.BuildWorkspaceSnapshot([]string{"weather", "coding"})
	if len(snap.Entries) != 2 {
		t.Errorf("filter [weather, coding] should return 2, got %d", len(snap.Entries))
	}
}

func TestNormalizeSkillFilter(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
		isNil    bool
	}{
		{"nil returns nil", nil, nil, true},
		{"empty returns empty", []string{}, []string{}, false},
		{"trims and dedupes", []string{" foo ", "bar", "foo"}, []string{"bar", "foo"}, false},
		{"filters empty strings", []string{"", " ", "a"}, []string{"a"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeSkillFilter(tt.input)
			if tt.isNil && got != nil {
				t.Errorf("expected nil, got %v", got)
				return
			}
			if !tt.isNil && got == nil && len(tt.expected) > 0 {
				t.Errorf("expected %v, got nil", tt.expected)
				return
			}
		})
	}
}

func TestMatchesSkillFilter(t *testing.T) {
	if !MatchesSkillFilter(nil, nil) {
		t.Error("nil vs nil should match")
	}
	if MatchesSkillFilter(nil, []string{}) {
		t.Error("nil vs empty should not match")
	}
	if !MatchesSkillFilter([]string{"a", "b"}, []string{"b", "a"}) {
		t.Error("same elements in different order should match")
	}
	if MatchesSkillFilter([]string{"a"}, []string{"a", "b"}) {
		t.Error("different lengths should not match")
	}
}

func TestParseFrontmatter(t *testing.T) {
	content := `---
name: test-skill
description: A test skill
user-invocable: true
---

# Test Skill
`
	fm := ParseFrontmatter(content)
	if fm["name"] != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", fm["name"])
	}
	if fm["description"] != "A test skill" {
		t.Errorf("expected description, got %q", fm["description"])
	}
}

func TestResolveSkillInvocationPolicy(t *testing.T) {
	fm := ParsedFrontmatter{
		"user-invocable":           "false",
		"disable-model-invocation": "true",
	}
	pol := ResolveSkillInvocationPolicy(fm)
	if pol.UserInvocable {
		t.Error("expected UserInvocable=false")
	}
	if !pol.DisableModelInvocation {
		t.Error("expected DisableModelInvocation=true")
	}

	// Defaults.
	pol = ResolveSkillInvocationPolicy(ParsedFrontmatter{})
	if !pol.UserInvocable {
		t.Error("default UserInvocable should be true")
	}
	if pol.DisableModelInvocation {
		t.Error("default DisableModelInvocation should be false")
	}
}

func TestNormalizeSafeBrewFormula(t *testing.T) {
	if got := normalizeSafeBrewFormula("ffmpeg"); got != "ffmpeg" {
		t.Errorf("expected 'ffmpeg', got %q", got)
	}
	if got := normalizeSafeBrewFormula("-bad"); got != "" {
		t.Errorf("expected empty for leading dash, got %q", got)
	}
	if got := normalizeSafeBrewFormula("../escape"); got != "" {
		t.Errorf("expected empty for path traversal, got %q", got)
	}
}

func TestNormalizeSafeDownloadURL(t *testing.T) {
	if got := normalizeSafeDownloadURL("https://example.com/file.tar.gz"); got == "" {
		t.Error("expected valid URL")
	}
	if got := normalizeSafeDownloadURL("ftp://bad.com/file"); got != "" {
		t.Errorf("expected empty for non-http scheme, got %q", got)
	}
	if got := normalizeSafeDownloadURL("has spaces"); got != "" {
		t.Errorf("expected empty for URL with spaces, got %q", got)
	}
}
