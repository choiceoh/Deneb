package genesis

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

func writeAdoptFixture(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAdoptBundledSkill: a bundled skill (nested category layout, with a
// support file) is copied into the managed dir, parsed, and registered; the
// managed copy is what the evolver will rewrite.
func TestAdoptBundledSkill_CopiesNestedSkillWithSupportFilesAndRegisters(t *testing.T) {
	bundled := t.TempDir()
	managed := t.TempDir()
	skillDir := filepath.Join(bundled, "productivity", "contract-review")
	writeAdoptFixture(t, skillDir, `---
name: contract-review
version: "1.0.0"
category: productivity
description: "계약서 검토 체크리스트."
---

# 계약 검토
`)
	if err := os.MkdirAll(filepath.Join(skillDir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "references", "note.md"), []byte("참고"), 0o644); err != nil {
		t.Fatal(err)
	}

	catalog := skills.NewCatalog(nil)
	e := NewEvolver(nil, catalog, nil, "", nil)
	e.SetAdoptionDirs(bundled, managed)

	entry, err := e.adoptBundledSkill("contract-review")
	if err != nil {
		t.Fatalf("adoptBundledSkill: %v", err)
	}
	if entry.Skill.Source != skills.SourceManaged {
		t.Errorf("source = %q, want managed", entry.Skill.Source)
	}
	wantPath := filepath.Join(managed, "contract-review", "SKILL.md")
	if entry.Skill.FilePath != wantPath {
		t.Errorf("filePath = %q, want %q", entry.Skill.FilePath, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("managed SKILL.md missing: %v", err)
	}
	// Support files ride along — the evolved skill must stay self-contained.
	if _, err := os.Stat(filepath.Join(managed, "contract-review", "references", "note.md")); err != nil {
		t.Errorf("support file not copied: %v", err)
	}
	// Registered: the evolver's next catalog.Get must hit.
	if _, ok := catalog.Get("contract-review"); !ok {
		t.Error("adopted skill not registered in catalog")
	}
	if entry.Skill.Version != "1.0.0" || entry.Skill.Category != "productivity" {
		t.Errorf("frontmatter not parsed: %+v", entry.Skill)
	}

	// Idempotent-ish: a second adoption loads the existing managed copy.
	again, err := e.adoptBundledSkill("contract-review")
	if err != nil {
		t.Fatalf("second adoptBundledSkill: %v", err)
	}
	if again.Skill.FilePath != wantPath {
		t.Errorf("second adoption path = %q, want %q", again.Skill.FilePath, wantPath)
	}
}

// TestAdoptBundledSkill_Failures: no bundled dir configured, and a name that
// exists nowhere, both fail cleanly (and leave no half-copied dir behind).
func TestAdoptBundledSkill_Failures(t *testing.T) {
	catalog := skills.NewCatalog(nil)
	e := NewEvolver(nil, catalog, nil, "", nil)
	if _, err := e.adoptBundledSkill("anything"); err == nil {
		t.Error("expected error when no bundled dir configured")
	}

	bundled := t.TempDir()
	managed := t.TempDir()
	e.SetAdoptionDirs(bundled, managed)
	if _, err := e.adoptBundledSkill("no-such-skill"); err == nil {
		t.Error("expected error for a skill absent from the bundled dir")
	}
	if _, err := os.Stat(filepath.Join(managed, "no-such-skill")); !os.IsNotExist(err) {
		t.Errorf("failed adoption must not leave a dir behind: %v", err)
	}
}

// TestLoadSkillEntry_ViaAdoption: flat bundled layout (no category dir) is
// also locatable.
func TestAdoptBundledSkill_LoadsFlatLayoutSkill(t *testing.T) {
	bundled := t.TempDir()
	managed := t.TempDir()
	writeAdoptFixture(t, filepath.Join(bundled, "topsolar-db"), "---\nname: topsolar-db\nversion: \"1.1.1\"\n---\n\n# DB\n")

	e := NewEvolver(nil, skills.NewCatalog(nil), nil, "", nil)
	e.SetAdoptionDirs(bundled, managed)
	entry, err := e.adoptBundledSkill("topsolar-db")
	if err != nil {
		t.Fatalf("adoptBundledSkill(flat): %v", err)
	}
	if entry.Skill.Name != "topsolar-db" || entry.Skill.Version != "1.1.1" {
		t.Errorf("entry = %+v", entry.Skill)
	}
}
