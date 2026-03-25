package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverWorkspaceSkills_empty(t *testing.T) {
	tmpDir := t.TempDir()
	entries := DiscoverWorkspaceSkills(DiscoverConfig{
		WorkspaceDir: tmpDir,
	})
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestDiscoverWorkspaceSkills_singleSkill(t *testing.T) {
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

	entries := DiscoverWorkspaceSkills(DiscoverConfig{
		WorkspaceDir: tmpDir,
	})
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

	entries := DiscoverWorkspaceSkills(DiscoverConfig{
		WorkspaceDir:     workspaceDir,
		BundledSkillsDir: bundledDir,
	})
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (merged), got %d", len(entries))
	}
	// Workspace should win over bundled.
	if entries[0].Skill.Description != "workspace version" {
		t.Errorf("expected workspace version to win, got %q", entries[0].Skill.Description)
	}
}

func TestDiscoverWorkspaceSkills_oversizedSkip(t *testing.T) {
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "skills", "big-skill")
	os.MkdirAll(skillsDir, 0o755)

	// Create an oversized SKILL.md (>256KB).
	bigContent := make([]byte, 300_000)
	for i := range bigContent {
		bigContent[i] = 'A'
	}
	os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), bigContent, 0o644)

	entries := DiscoverWorkspaceSkills(DiscoverConfig{
		WorkspaceDir: tmpDir,
	})
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
