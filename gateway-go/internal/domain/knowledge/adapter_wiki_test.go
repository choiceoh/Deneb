package knowledge

import (
	"context"
	"path/filepath"
	"strings"
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

func TestWikiAdapterRecallCarriesLateContextAndTypedLocation(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { _ = store.Close() })
	page := wiki.NewPage("배포 결정", "시스템", nil)
	page.Meta.Updated = "2026-07-20"
	page.Body = "## 배경\n이전 방식\n\n## 결정\nORBIT-WIKI 적용\n\n## 검증\n스모크 통과\n"
	if err := store.WritePage("시스템/deploy.md", page); err != nil {
		t.Fatal(err)
	}

	hits, err := NewWikiAdapter(store).Recall(context.Background(), "ORBIT-WIKI", 1)
	if err != nil || len(hits) != 1 {
		t.Fatalf("Recall = %+v, %v", hits, err)
	}
	hit := hits[0]
	if !strings.Contains(hit.Context, "## 배경") || !strings.Contains(hit.Context, "## 검증") {
		t.Fatalf("late context = %q", hit.Context)
	}
	if hit.Provenance.Locator.StartLine <= 0 || hit.Provenance.Locator.ContextStartLine <= 0 {
		t.Fatalf("locator = %+v", hit.Provenance.Locator)
	}
	if hit.Time == 0 {
		t.Fatal("updated timestamp was not promoted into typed evidence")
	}
	if err := NewWikiAdapter(store).(SourceDescriber).Descriptor().Sync.Validate(); err != nil {
		t.Fatalf("wiki sync contract: %v", err)
	}
}

// TestWikiAdapterRecallUsesDefaultRerankPath pins that agent knowledge recall
// no longer forces SkipRerank — empty QueryOptions keep gated model rerank eligible.
func TestWikiAdapterRecallCarriesPersonEmails(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { _ = store.Close() })
	page := wiki.NewPage("이현성", "인물", nil)
	page.Meta.Emails = []string{"2555151@kia.com"}
	page.Body = "기아 광주시설관리팀"
	if err := store.WritePage("인물/이현성.md", page); err != nil {
		t.Fatal(err)
	}

	hits := testutil.Must(NewWikiAdapter(store).Recall(context.Background(), "이현성", 3))
	found := false
	for _, h := range hits {
		if h.Ref.ID == "인물/이현성.md" {
			found = true
			if h.Meta["emails"] != "2555151@kia.com" {
				t.Fatalf("emails meta = %q", h.Meta["emails"])
			}
		}
	}
	if !found {
		t.Fatalf("expected 인물 hit, got %+v", hits)
	}

	doc := testutil.Must(NewWikiAdapter(store).Read(context.Background(), "인물/이현성.md"))
	if doc.Meta["emails"] != "2555151@kia.com" {
		t.Fatalf("Read emails = %q", doc.Meta["emails"])
	}
}

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
