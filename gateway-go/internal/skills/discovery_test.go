package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// isolatedConfig returns a DiscoverConfig that won't pick up skills from the
// real home directory (~/.deneb/skills, ~/.agents/skills).
func isolatedConfig(workspaceDir string) DiscoverConfig {
	return DiscoverConfig{
		WorkspaceDir:     workspaceDir,
		ManagedSkillsDir: filepath.Join(workspaceDir, ".empty-managed"),
	}
}

func TestDiscoverWorkspaceSkills_empty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	cfg := isolatedConfig(tmpDir)
	entries := DiscoverWorkspaceSkills(cfg)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestDiscoverWorkspaceSkills_singleSkill(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "skills", "test-skill")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: test-skill
description: A test skill
---
# Test Skill

This is a test skill.
`
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := isolatedConfig(tmpDir)
	entries := DiscoverWorkspaceSkills(cfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Skill.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", entries[0].Skill.Name)
	}
	if entries[0].Skill.Description != "A test skill" {
		t.Errorf("expected description 'A test skill', got %q", entries[0].Skill.Description)
	}
	if entries[0].Skill.Source != SourceWorkspace {
		t.Errorf("expected source workspace, got %q", entries[0].Skill.Source)
	}
}

func TestDiscoverWorkspaceSkills_precedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	bundledDir := filepath.Join(tmpDir, "bundled")
	workspaceDir := filepath.Join(tmpDir, "workspace")

	// Create bundled skill.
	bundledSkillDir := filepath.Join(bundledDir, "my-skill")
	os.MkdirAll(bundledSkillDir, 0o755)
	os.WriteFile(filepath.Join(bundledSkillDir, "SKILL.md"), []byte("---\nname: my-skill\ndescription: bundled version\n---\n"), 0o644)

	// Create workspace skill with same name.
	wsSkillDir := filepath.Join(workspaceDir, "skills", "my-skill")
	os.MkdirAll(wsSkillDir, 0o755)
	os.WriteFile(filepath.Join(wsSkillDir, "SKILL.md"), []byte("---\nname: my-skill\ndescription: workspace version\n---\n"), 0o644)

	cfg := isolatedConfig(workspaceDir)
	cfg.BundledSkillsDir = bundledDir
	entries := DiscoverWorkspaceSkills(cfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (merged), got %d", len(entries))
	}
	// Workspace should win over bundled.
	if entries[0].Skill.Description != "workspace version" {
		t.Errorf("expected workspace version to win, got %q", entries[0].Skill.Description)
	}
}

func TestDiscoverWorkspaceSkills_oversizedSkip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "skills", "big-skill")
	os.MkdirAll(skillsDir, 0o755)

	// Create an oversized SKILL.md (>256KB).
	bigContent := make([]byte, 300_000)
	for i := range bigContent {
		bigContent[i] = 'A'
	}
	os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), bigContent, 0o644)

	cfg := isolatedConfig(tmpDir)
	entries := DiscoverWorkspaceSkills(cfg)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries (oversized should be skipped), got %d", len(entries))
	}
}

func TestResolveNestedSkillsRoot(t *testing.T) {
	tmpDir := t.TempDir()

	// No nested skills/ directory — should return dir itself.
	result := resolveNestedSkillsRoot(tmpDir, 100)
	if result != tmpDir {
		t.Errorf("expected %q, got %q", tmpDir, result)
	}

	// Create nested skills/ with a skill.
	nested := filepath.Join(tmpDir, "skills", "foo")
	os.MkdirAll(nested, 0o755)
	os.WriteFile(filepath.Join(nested, "SKILL.md"), []byte("---\nname: foo\n---\n"), 0o644)

	result = resolveNestedSkillsRoot(tmpDir, 100)
	expected := filepath.Join(tmpDir, "skills")
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestIsPathInside(t *testing.T) {
	if !isPathInside("/a/b", "/a/b/c") {
		t.Error("expected /a/b/c inside /a/b")
	}
	if !isPathInside("/a/b", "/a/b") {
		t.Error("expected /a/b inside /a/b (same path)")
	}
	if isPathInside("/a/b", "/a/c") {
		t.Error("expected /a/c NOT inside /a/b")
	}
}

func TestDiscoverWorkspaceSkills_categoryFromFrontmatter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "skills", "my-tool")
	os.MkdirAll(skillsDir, 0o755)
	content := "---\nname: my-tool\nversion: \"1.0.0\"\ncategory: devops\ndescription: A tool\n---\n# Body\n"
	os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte(content), 0o644)

	cfg := isolatedConfig(tmpDir)
	entries := DiscoverWorkspaceSkills(cfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Skill.Category != "devops" {
		t.Errorf("expected category 'devops', got %q", entries[0].Skill.Category)
	}
	if entries[0].Skill.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", entries[0].Skill.Version)
	}
}

func TestDiscoverWorkspaceSkills_nestedCategoryDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()

	// Create nested category layout: skills/coding/my-agent/SKILL.md
	nestedSkillDir := filepath.Join(tmpDir, "skills", "coding", "my-agent")
	os.MkdirAll(nestedSkillDir, 0o755)
	content := "---\nname: my-agent\ndescription: An agent skill\n---\n# Body\n"
	os.WriteFile(filepath.Join(nestedSkillDir, "SKILL.md"), []byte(content), 0o644)

	cfg := isolatedConfig(tmpDir)
	entries := DiscoverWorkspaceSkills(cfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Skill.Name != "my-agent" {
		t.Errorf("expected name 'my-agent', got %q", entries[0].Skill.Name)
	}
	// Category should be the parent directory name "coding".
	if entries[0].Skill.Category != "coding" {
		t.Errorf("expected category 'coding' from directory, got %q", entries[0].Skill.Category)
	}
}

func TestDiscoverWorkspaceSkills_nestedCategoryOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()

	// Nested layout with frontmatter category override.
	nestedSkillDir := filepath.Join(tmpDir, "skills", "tools", "my-cli")
	os.MkdirAll(nestedSkillDir, 0o755)
	content := "---\nname: my-cli\ncategory: integration\ndescription: A CLI tool\n---\n"
	os.WriteFile(filepath.Join(nestedSkillDir, "SKILL.md"), []byte(content), 0o644)

	cfg := isolatedConfig(tmpDir)
	entries := DiscoverWorkspaceSkills(cfg)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	// Frontmatter category "integration" should override directory category "tools".
	if entries[0].Skill.Category != "integration" {
		t.Errorf("expected category 'integration' (frontmatter override), got %q", entries[0].Skill.Category)
	}
}

func TestDiscoverWorkspaceSkills_mixedFlatAndNested(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()

	// Flat skill: skills/flat-skill/SKILL.md
	flatDir := filepath.Join(tmpDir, "skills", "flat-skill")
	os.MkdirAll(flatDir, 0o755)
	os.WriteFile(filepath.Join(flatDir, "SKILL.md"), []byte("---\nname: flat-skill\ndescription: flat\n---\n"), 0o644)

	// Nested skill: skills/devops/nested-skill/SKILL.md
	nestedDir := filepath.Join(tmpDir, "skills", "devops", "nested-skill")
	os.MkdirAll(nestedDir, 0o755)
	os.WriteFile(filepath.Join(nestedDir, "SKILL.md"), []byte("---\nname: nested-skill\ndescription: nested\n---\n"), 0o644)

	cfg := isolatedConfig(tmpDir)
	entries := DiscoverWorkspaceSkills(cfg)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Find each by name.
	var flat, nested *SkillEntry
	for i := range entries {
		switch entries[i].Skill.Name {
		case "flat-skill":
			flat = &entries[i]
		case "nested-skill":
			nested = &entries[i]
		}
	}
	if flat == nil {
		t.Fatal("flat-skill not found")
	}
	if nested == nil {
		t.Fatal("nested-skill not found")
	}
	if flat.Skill.Category != "" {
		t.Errorf("flat skill should have empty category, got %q", flat.Skill.Category)
	}
	if nested.Skill.Category != "devops" {
		t.Errorf("nested skill should have category 'devops', got %q", nested.Skill.Category)
	}
}
