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
	if _, err := writer.Record(context.Background(), RecordOptions{
		Page:       "프로젝트/new-fact.md",
		Title:      "New fact",
		Category:   "프로젝트",
		Body:       "모순/갱신: old fact를 대체한다.",
		Supersedes: []string{"프로젝트/old-fact.md"},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got := testutil.Must(store.ReadPage("프로젝트/old-fact.md"))
	if got.Meta.SupersededBy != "프로젝트/new-fact.md" {
		t.Fatalf("SupersededBy = %q, want 프로젝트/new-fact.md", got.Meta.SupersededBy)
	}
}
