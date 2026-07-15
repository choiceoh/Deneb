package knowledge

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestWikiAdapterRecordUpdatesSupersededPageMeta(t *testing.T) {
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

// TestWikiAdapterRecallUsesDefaultRerankPath pins that agent knowledge recall
// no longer forces SkipRerank — empty QueryOptions keep gated model rerank eligible.
func TestWikiAdapterRecallUsesDefaultRerankPath(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { _ = store.Close() })

	page := wiki.NewPage("Contract", "프로젝트", []string{"계약"})
	page.Body = "계약 일정과 납기"
	if err := store.WritePage("프로젝트/contract.md", page); err != nil {
		t.Fatal(err)
	}

	ad := NewWikiAdapter(store)
	hits, err := ad.Recall(context.Background(), "계약", 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one wiki hit")
	}

	skip := testutil.Must(store.SearchWithOptions(context.Background(), "계약", 5, wiki.QueryOptions{SkipRerank: true}))
	if skip.Diagnostics.Rerank.Attempted {
		t.Fatal("SkipRerank must not attempt model rerank")
	}
	if skip.Diagnostics.Rerank.Reason != "deferred_to_query_plan" {
		t.Fatalf("SkipRerank diagnostics = %+v, want deferred placeholder", skip.Diagnostics.Rerank)
	}
	def := testutil.Must(store.SearchWithOptions(context.Background(), "계약", 5, wiki.QueryOptions{}))
	if def.Diagnostics.Rerank.Reason == "deferred_to_query_plan" {
		t.Fatalf("default options must invoke applyModelRerank, got %+v", def.Diagnostics.Rerank)
	}
}
