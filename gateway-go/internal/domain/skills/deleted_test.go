package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMarkSkillDeletedRoundtripIdempotentSorted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if got := LoadDeletedSkillNames(); got != nil {
		t.Fatalf("empty state should load nil, got %v", got)
	}
	for _, name := range []string{"web-fetch", "email-analysis", "web-fetch"} {
		if err := MarkSkillDeleted(name, "테스트", time.Now()); err != nil {
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

// A tombstone must record WHY and WHEN. Without it, six skills — including the
// RSI loop's own evolution-proposal and skill-factory — sat suppressed for five
// weeks and the only way to date the suppression was the file's mtime.
func TestMarkSkillDeletedRecordsReasonAndTime(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	at := time.Date(2026, 8, 26, 7, 39, 0, 0, time.UTC)

	if err := MarkSkillDeleted("fact-check", "저빈도라 잠시 숨김", at); err != nil {
		t.Fatal(err)
	}
	got := LoadDeletedSkills()
	if len(got) != 1 || got[0].Name != "fact-check" {
		t.Fatalf("tombstone = %+v", got)
	}
	if got[0].Reason != "저빈도라 잠시 숨김" || got[0].At != "2026-08-26T07:39:00Z" {
		t.Errorf("사유·시각이 기록되지 않음: %+v", got[0])
	}

	// An unexplained deletion still gets a clock and a placeholder reason.
	if err := MarkSkillDeleted("kb-interview", "  ", at); err != nil {
		t.Fatal(err)
	}
	for _, d := range LoadDeletedSkills() {
		if d.Name == "kb-interview" && (d.Reason == "" || d.At == "") {
			t.Errorf("사유 없는 삭제가 아무 흔적도 안 남김: %+v", d)
		}
	}

	// Filtering still works on names, and unmarking removes the whole entry.
	if _, ok := LoadDeletedSkillNames()["fact-check"]; !ok {
		t.Error("이름 필터가 깨짐")
	}
	if err := UnmarkSkillDeleted("fact-check"); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadDeletedSkillNames()["fact-check"]; ok {
		t.Error("해제 후에도 남음")
	}
}

// Files written before tombstones carried metadata are bare name strings —
// they must keep suppressing, or an upgrade silently un-hides them.
func TestLoadDeletedSkillsReadsLegacyNameList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".deneb", "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"skills":["deep-research","fact-check"]}`
	if err := os.WriteFile(filepath.Join(dir, "deleted_skills.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	names := LoadDeletedSkillNames()
	if len(names) != 2 {
		t.Fatalf("레거시 파일이 안 읽힘: %v", names)
	}
	got := LoadDeletedSkills()
	if len(got) != 2 || got[0].Name != "deep-research" || got[0].Reason != "" {
		t.Errorf("레거시 항목 해석이 다름: %+v", got)
	}
}
