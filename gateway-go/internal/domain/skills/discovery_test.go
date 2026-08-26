package skills

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

func TestDiscoverWorkspaceSkillsReturnsSingleWorkspaceSkill(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "skills", "test-skill")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: test-skill
description: A test skill
metadata: {"deneb":{"triggers":["테스트절차"]}}
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
		t.Fatalf("got %d, want 1 entry", len(entries))
	}
	if entries[0].Skill.Name != "test-skill" {
		t.Errorf("got %q, want name 'test-skill'", entries[0].Skill.Name)
	}
	if entries[0].Skill.Description != "A test skill" {
		t.Errorf("got %q, want description 'A test skill'", entries[0].Skill.Description)
	}
	if entries[0].Skill.Source != SourceWorkspace {
		t.Errorf("got %q, want source workspace", entries[0].Skill.Source)
	}
	if entries[0].Body != "# Test Skill\n\nThis is a test skill." {
		t.Errorf("instruction body = %q", entries[0].Body)
	}
}

func TestJITSkillInstructionBodyKeepsOnlyTriggeredOperationalPromptBody(t *testing.T) {
	triggered := `---
name: test
metadata: {"deneb":{"triggers":["테스트절차"]}}
---
# Procedure

Do the work.

## Changelog
- v1.0.1: history only
`
	if got := jitSkillInstructionBody(triggered); got != "# Procedure\n\nDo the work." {
		t.Fatalf("triggered operational body = %q", got)
	}
	if got := jitSkillInstructionBody("---\nname: plain\n---\n# Procedure"); got != "" {
		t.Fatalf("triggerless body retained: %q", got)
	}
	if got := jitSkillInstructionBody("---\nname: local\ntype: local\nmetadata: {\"deneb\":{\"triggers\":[\"로컬절차\"]}}\n---\n# Procedure"); got != "" {
		t.Fatalf("non-prompt body retained: %q", got)
	}
}

func TestDiscoverWorkspaceSkillsLoadsWorkspaceSkillOverBundled(t *testing.T) {
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
		t.Fatalf("got %d, want 1 entry (merged)", len(entries))
	}
	// Workspace should win over bundled.
	if entries[0].Skill.Description != "workspace version" {
		t.Errorf("got %q, want workspace version to win", entries[0].Skill.Description)
	}
}

func TestDiscoverWorkspaceSkillsIgnoresOversizedSkillFile(t *testing.T) {
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
		t.Fatalf("got %d, want 0 entries (oversized should be skipped)", len(entries))
	}
}

func TestResolveNestedSkillsRootReturnsNestedSkillsDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// No nested skills/ directory — should return dir itself.
	result := resolveNestedSkillsRoot(tmpDir, 100)
	if result != tmpDir {
		t.Errorf("got %q, want %q", result, tmpDir)
	}

	// Create nested skills/ with a skill.
	nested := filepath.Join(tmpDir, "skills", "foo")
	os.MkdirAll(nested, 0o755)
	os.WriteFile(filepath.Join(nested, "SKILL.md"), []byte("---\nname: foo\n---\n"), 0o644)

	result = resolveNestedSkillsRoot(tmpDir, 100)
	expected := filepath.Join(tmpDir, "skills")
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestIsPathInsideReturnsTrueForNestedAndSamePath(t *testing.T) {
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

func TestDiscoverWorkspaceSkillsReadsCategoryAndVersionFromFrontmatter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	skillsDir := filepath.Join(tmpDir, "skills", "my-tool")
	os.MkdirAll(skillsDir, 0o755)
	content := "---\nname: my-tool\nversion: \"1.0.0\"\ncategory: devops\ndescription: A tool\n---\n# Body\n"
	os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte(content), 0o644)

	cfg := isolatedConfig(tmpDir)
	entries := DiscoverWorkspaceSkills(cfg)
	if len(entries) != 1 {
		t.Fatalf("got %d, want 1 entry", len(entries))
	}
	if entries[0].Skill.Category != "devops" {
		t.Errorf("got %q, want category 'devops'", entries[0].Skill.Category)
	}
	if entries[0].Skill.Version != "1.0.0" {
		t.Errorf("got %q, want version '1.0.0'", entries[0].Skill.Version)
	}
}

func TestDiscoverWorkspaceSkillsReadsCategoryFromParentDirectory(t *testing.T) {
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
		t.Fatalf("got %d, want 1 entry", len(entries))
	}
	if entries[0].Skill.Name != "my-agent" {
		t.Errorf("got %q, want name 'my-agent'", entries[0].Skill.Name)
	}
	// Category should be the parent directory name "coding".
	if entries[0].Skill.Category != "coding" {
		t.Errorf("got %q, want category 'coding' from directory", entries[0].Skill.Category)
	}
}

func TestDiscoverWorkspaceSkillsReturnsFrontmatterCategoryOverridingDirectory(t *testing.T) {
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
		t.Fatalf("got %d, want 1 entry", len(entries))
	}
	// Frontmatter category "integration" should override directory category "tools".
	if entries[0].Skill.Category != "integration" {
		t.Errorf("got %q, want category 'integration' (frontmatter override)", entries[0].Skill.Category)
	}
}

func TestDiscoverWorkspaceSkillsLoadsMixedFlatAndNestedLayouts(t *testing.T) {
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
		t.Fatalf("got %d, want 2 entries", len(entries))
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

// Genesis output nests one level deeper than the standard walk
// (managed/genesis/<category>/<name>/SKILL.md). Discovery must reach it, or
// loop-generated skills vanish from the catalog at every restart.
func TestDiscoverWorkspaceSkillsReturnsGenesisDepthManagedSkill(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tmpDir := t.TempDir()
	managedDir := filepath.Join(tmpDir, "managed-skills")
	genSkillDir := filepath.Join(managedDir, "genesis", "productivity", "gen-skill")
	if err := os.MkdirAll(genSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: gen-skill
description: Generated by Propus
---
# Gen Skill
`
	if err := os.WriteFile(filepath.Join(genSkillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := DiscoverWorkspaceSkills(DiscoverConfig{
		WorkspaceDir:     filepath.Join(tmpDir, "ws"),
		ManagedSkillsDir: managedDir,
	})
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (genesis-depth skill)", len(entries))
	}
	e := entries[0]
	if e.Skill.Name != "gen-skill" {
		t.Errorf("got %q, want name 'gen-skill'", e.Skill.Name)
	}
	if e.Skill.Source != SourceManaged {
		t.Errorf("got %q, want source managed", e.Skill.Source)
	}
	if e.Skill.Category != "productivity" {
		t.Errorf("got %q, want category 'productivity' (genesis sub-dir)", e.Skill.Category)
	}
}

// Shadowing is normal (an evolved managed copy beating its bundled seed) but
// invisible, and an invisible override is a trap: the file you edit is not the
// file that runs. It also hid a real accident — a genesis `email-analysis`
// v0.1.0 shadowing the mature bundled v1.3.0.
func TestMergeSkillsNamesEveryShadow(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	merged := map[string]discoveredSkill{}
	mergeSkills(
		merged, log,
		[]discoveredSkill{{Name: "email-analysis", Source: SourceBundled, FilePath: "/repo/email-analysis/SKILL.md"}},
		[]discoveredSkill{{Name: "email-analysis", Source: SourceManaged, FilePath: "/state/email-analysis/SKILL.md"}},
		[]discoveredSkill{{Name: "solo", Source: SourceBundled, FilePath: "/repo/solo/SKILL.md"}},
	)

	if got := merged["email-analysis"].Source; got != SourceManaged {
		t.Errorf("우선순위가 뒤집힘: %v", got)
	}
	out := buf.String()
	if !strings.Contains(out, "email-analysis") || !strings.Contains(out, "/repo/email-analysis/SKILL.md") {
		t.Errorf("가려진 사본을 로그가 지목하지 않음: %q", out)
	}
	if strings.Contains(out, "solo") {
		t.Errorf("겹치지 않는 스킬까지 경고함: %q", out)
	}
}
