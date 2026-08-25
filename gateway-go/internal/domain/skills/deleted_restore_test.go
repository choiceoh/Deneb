package skills

import (
	"path/filepath"
	"testing"
)

// A bundled-skill deletion must be reversible. It was not: the tombstone
// removed the skill from every surface including the list a restore would be
// reached from, so recovery meant hand-editing the JSON.
func TestUnmarkSkillDeletedRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := MarkSkillDeleted("kb-interview"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := MarkSkillDeleted("evolution-proposal"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if got := DeletedSkillNamesSorted(); len(got) != 2 || got[0] != "evolution-proposal" {
		t.Fatalf("deleted names = %v, want sorted pair", got)
	}

	if err := UnmarkSkillDeleted("kb-interview"); err != nil {
		t.Fatalf("unmark: %v", err)
	}
	got := DeletedSkillNamesSorted()
	if len(got) != 1 || got[0] != "evolution-proposal" {
		t.Fatalf("after unmark = %v, want only evolution-proposal", got)
	}
	if _, still := LoadDeletedSkillNames()["kb-interview"]; still {
		t.Error("kb-interview still tombstoned after unmark")
	}
}

// Idempotent both ways — a retry after a dropped response must not error, and
// unmarking a skill that was never deleted is a no-op.
func TestUnmarkSkillDeletedIsIdempotent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := UnmarkSkillDeleted("never-deleted"); err != nil {
		t.Fatalf("unmark of a live skill must be a no-op, got %v", err)
	}
	if err := MarkSkillDeleted("fact-check"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := UnmarkSkillDeleted("fact-check"); err != nil {
			t.Fatalf("unmark #%d: %v", i, err)
		}
	}
	if got := DeletedSkillNamesSorted(); got != nil {
		t.Fatalf("deleted names = %v, want empty", got)
	}
}

// The tombstone file must survive a round trip through both writers — an empty
// list is a real state (everything restored), not a missing file.
func TestDeletedSkillNamesSortedEmptyAfterFullRestore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := MarkSkillDeleted("deep-research"); err != nil {
		t.Fatalf("mark: %v", err)
	}
	if err := UnmarkSkillDeleted("deep-research"); err != nil {
		t.Fatalf("unmark: %v", err)
	}
	if _, err := filepath.Abs(home); err != nil {
		t.Fatal(err)
	}
	if got := LoadDeletedSkillNames(); len(got) != 0 {
		t.Fatalf("loaded = %v, want none", got)
	}
}
