package knowledge

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestWikiAdapterRecordMarksSupersededPages(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { _ = store.Close() })

	old := wiki.NewPage("Old fact", "프로젝트", nil)
	old.Body = "old body"
	if err := store.WritePage("프로젝트/old-fact.md", old); err != nil {
		t.Fatalf("write old page: %v", err)
	}

	writer, ok := NewWikiAdapter(store).(Writer)
	if !ok {
		t.Fatal("wiki adapter should implement Writer")
	}
	// A NEW flat 프로젝트/<name>.md creation normalizes onto the 대표.md slot
	// (wiki-layout 불변식) — the supersede mark must point at the page that was
	// actually written.
	ref, err := writer.Record(context.Background(), RecordOptions{
		Page:       "프로젝트/new-fact.md",
		Title:      "New fact",
		Category:   "프로젝트",
		Body:       "모순/갱신: old fact를 대체한다.",
		Supersedes: []string{"프로젝트/old-fact.md"},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	want := wiki.RepPagePath("new-fact")
	if ref.ID != want {
		t.Fatalf("ref.ID = %q, want the layout slot %q", ref.ID, want)
	}
	if _, err := store.ReadPage(want); err != nil {
		t.Fatalf("normalized page missing: %v", err)
	}

	got := testutil.Must(store.ReadPage("프로젝트/old-fact.md"))
	if got.Meta.SupersededBy != want {
		t.Fatalf("SupersededBy = %q, want %q", got.Meta.SupersededBy, want)
	}
}

// TestWikiAdapterRecord_LegacyFlatUpdateStaysInPlace: only NEW page creations
// normalize onto the folder layout — an update of an EXISTING legacy flat
// 대표페이지 must keep writing in place (the migration tool owns relocation).
func TestWikiAdapterRecord_LegacyFlatUpdateStaysInPlace(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { _ = store.Close() })

	legacy := wiki.NewPage("레거시 프로젝트", "프로젝트", nil)
	legacy.Body = "기존 본문"
	if err := store.WritePage("프로젝트/레거시.md", legacy); err != nil {
		t.Fatal(err)
	}

	writer := NewWikiAdapter(store).(Writer)
	ref, err := writer.Record(context.Background(), RecordOptions{
		Page:     "프로젝트/레거시", // extensionless spelling of the same page
		Title:    "레거시 프로젝트",
		Category: "프로젝트",
		Body:     "갱신된 본문",
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if ref.ID != "프로젝트/레거시.md" {
		t.Fatalf("ref.ID = %q, want the legacy flat path", ref.ID)
	}
	got := testutil.Must(store.ReadPage("프로젝트/레거시.md"))
	if got.Body != "갱신된 본문" {
		t.Fatalf("legacy page not updated in place: %q", got.Body)
	}
	if pg, _ := store.ReadPage(wiki.RepPagePath("레거시")); pg != nil {
		t.Fatal("update of a legacy flat page must not mint a 대표.md sibling")
	}
}
