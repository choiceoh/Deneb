package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func seedSkillFile(t *testing.T, root, rel string) string {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("---\nname: x\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFindSkillFile_AllLayouts(t *testing.T) {
	root := t.TempDir()
	want := map[string]string{
		"flat-skill": seedSkillFile(t, root, "flat-skill/SKILL.md"),
		"cat-skill":  seedSkillFile(t, root, "productivity/cat-skill/SKILL.md"),
		"deep-skill": seedSkillFile(t, root, "genesis/productivity/deep-skill/SKILL.md"),
	}
	for name, wantPath := range want {
		got, ok := FindSkillFile(root, name)
		if !ok || got != wantPath {
			t.Errorf("FindSkillFile(%q) = %q, %v; want %q, true", name, got, ok, wantPath)
		}
	}
}

func TestFindSkillFile_MissAndEmptyInputs(t *testing.T) {
	root := t.TempDir()
	seedSkillFile(t, root, "genesis/productivity/present/SKILL.md")
	if p, ok := FindSkillFile(root, "absent"); ok {
		t.Errorf("expected miss for absent skill, got %q", p)
	}
	if _, ok := FindSkillFile("", "present"); ok {
		t.Error("expected miss for empty root")
	}
	if _, ok := FindSkillFile(root, ""); ok {
		t.Error("expected miss for empty name")
	}
	if _, ok := FindSkillFile(filepath.Join(root, "no-such-dir"), "present"); ok {
		t.Error("expected miss for nonexistent root")
	}
}
