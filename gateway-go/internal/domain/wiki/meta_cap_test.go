package wiki

import (
	"path/filepath"
	"testing"
)

func TestRankRelated_PrefersRepThenPersonThenWork(t *testing.T) {
	in := []string{
		"기타/잡동사니.md",
		"프로젝트/foo/메일분석/m.md",
		"프로젝트/거래/joca.md",
		"시스템/vaultwarden.md",
		"인물/김대희.md",
		"프로젝트/foo/대표.md",
		"업무/바로.md",
	}
	got := RankRelated(in, 3)
	want := []string{"프로젝트/foo/대표.md", "인물/김대희.md", "업무/바로.md"}
	if !sameStringSlice(got, want) {
		t.Fatalf("RankRelated=%v want %v", got, want)
	}
}

func TestMergeRelatedAndTags_CapExistingFirst(t *testing.T) {
	related := mergeRelated(
		[]string{"프로젝트/a/대표.md", "인물/x.md", "업무/y.md", "기타/z.md"},
		[]string{"시스템/new.md"},
	)
	if !sameStringSlice(related, []string{"프로젝트/a/대표.md", "인물/x.md", "업무/y.md"}) {
		t.Fatalf("mergeRelated=%v", related)
	}

	tags := mergeTags([]string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}, []string{"j"})
	if len(tags) != MaxTagsPerPage || tags[0] != "a" || tags[7] != "h" {
		t.Fatalf("mergeTags=%v", tags)
	}
}

func TestMergeCues_DoesNotInheritTagCap(t *testing.T) {
	// Cues have their own cap (10). A tag-cap of 8 must not truncate them first.
	var existing, added []string
	for i := 0; i < 6; i++ {
		existing = append(existing, "cue-e"+string(rune('a'+i)))
		added = append(added, "cue-a"+string(rune('a'+i)))
	}
	got := mergeCues(existing, added)
	if len(got) != 10 {
		t.Fatalf("mergeCues len=%d want 10 (own cap), got %v", len(got), got)
	}
}

func TestCapPageRelations_TrimsOverflow(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	page := NewPage("기아", "프로젝트", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"})
	page.Meta.Related = []string{
		"기타/x.md", "인물/y.md", "프로젝트/kia/대표.md", "업무/z.md", "프로젝트/메일분석/m.md",
	}
	if err := store.WritePage("프로젝트/거래/기아.md", page); err != nil {
		t.Fatal(err)
	}
	n, err := store.CapPageRelations()
	if err != nil || n != 1 {
		t.Fatalf("CapPageRelations n=%d err=%v", n, err)
	}
	got, err := store.ReadPage("프로젝트/거래/기아.md")
	if err != nil {
		t.Fatal(err)
	}
	if !sameStringSlice(got.Meta.Related, []string{"프로젝트/kia/대표.md", "인물/y.md", "업무/z.md"}) {
		t.Errorf("related=%v", got.Meta.Related)
	}
	if len(got.Meta.Tags) != MaxTagsPerPage {
		t.Errorf("tags=%v", got.Meta.Tags)
	}
	n2, err := store.CapPageRelations()
	if err != nil || n2 != 0 {
		t.Fatalf("second CapPageRelations n=%d err=%v (want idempotent 0)", n2, err)
	}
}
