package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkSkillDeletedRoundtripIdempotentSorted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if got := LoadDeletedSkillNames(); got != nil {
		t.Fatalf("empty state should load nil, got %v", got)
	}
	for _, name := range []string{"web-fetch", "email-analysis", "web-fetch"} {
		if err := MarkSkillDeleted(name); err != nil {
			t.Fatalf("MarkSkillDeleted(%q): %v", name, err)
		}
	}
	names := LoadDeletedSkillNames()
	if len(names) != 2 {
		t.Fatalf("names = %v, want 2 distinct", names)
	}
	for _, want := range []string{"web-fetch", "email-analysis"} {
		if _, ok := names[want]; !ok {
			t.Errorf("missing %q in %v", want, names)
		}
	}

	// The tombstones drop matching entries via the shared exclusion filter.
	entries := []SkillEntry{
		{Skill: Skill{Name: "web-fetch", Source: SourceBundled}},
		{Skill: Skill{Name: "kept", Source: SourceBundled}},
	}
	filtered := FilterExcludedSkills(entries, names)
	if len(filtered) != 1 || filtered[0].Skill.Name != "kept" {
		t.Errorf("filtered = %+v, want only 'kept'", filtered)
	}
}

func TestLoadDeletedSkillNamesMalformedReadsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".deneb", "data", "deleted_skills.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadDeletedSkillNames(); got != nil {
		t.Errorf("malformed file should read as empty, got %v", got)
	}
}
