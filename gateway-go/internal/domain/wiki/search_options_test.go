package wiki

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSearchWithOptionsReportsStagesAndBM25Explanation(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()
	mustWrite(t, store, "projects/a.md", &Page{
		Meta: Frontmatter{ID: "a", Title: "계약 일정", Category: "projects"}, Body: "계약 일정 검토 내용입니다.",
	})

	bm25, err := store.SearchWithOptions(context.Background(), "계약 일정", 5, QueryOptions{Mode: SearchModeBM25, Explain: true})
	if err != nil {
		t.Fatalf("BM25 search: %v", err)
	}
	if len(bm25.Results) != 1 || bm25.Diagnostics.Mode != SearchModeBM25 || bm25.Diagnostics.Fusion != "bm25" {
		t.Fatalf("BM25 report = %+v", bm25)
	}
	explain := bm25.Results[0].Explain
	if explain == nil || explain.BM25 == nil || explain.BM25.Rank != 1 || explain.Semantic != nil || explain.FinalScore != bm25.Results[0].Score {
		t.Fatalf("BM25 explanation = %+v", explain)
	}

	for _, mode := range []SearchMode{SearchModeSemantic, SearchModeHybrid, SearchModeFull} {
		report, searchErr := store.SearchWithOptions(context.Background(), "계약 일정", 5, QueryOptions{Mode: mode})
		if searchErr != nil {
			t.Fatalf("mode %s: %v", mode, searchErr)
		}
		if report.Diagnostics.Mode != mode || report.Diagnostics.SemanticAvailable {
			t.Fatalf("mode %s diagnostics = %+v", mode, report.Diagnostics)
		}
		if mode == SearchModeSemantic {
			if report.Diagnostics.Fusion != "semantic" || len(report.Results) != 0 {
				t.Fatalf("semantic-only degraded report = %+v", report)
			}
		} else if report.Diagnostics.Fusion != "bm25-fallback" || len(report.Results) != 1 {
			t.Fatalf("mode %s fallback report = %+v", mode, report)
		}
	}
}

func TestIntentRerankOnlyReordersAdmittedCandidates(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()
	mustWrite(t, store, "projects/a.md", &Page{
		Meta: Frontmatter{ID: "a", Title: "계약 일정", Category: "projects"}, Body: "계약 일정 검토 내용입니다.",
	})
	mustWrite(t, store, "projects/z.md", &Page{
		Meta: Frontmatter{ID: "z", Title: "대한전선 계약 일정", Category: "projects"}, Body: "계약 일정 검토 내용입니다.",
	})
	// This page matches the intent but not the base query's required AND
	// terms. Intent must never use it as a newly admitted candidate.
	mustWrite(t, store, "projects/intent-only.md", &Page{
		Meta: Frontmatter{ID: "intent-only", Title: "대한전선 별도 메모", Category: "projects"}, Body: "다른 공급 이슈입니다.",
	})

	report, err := store.SearchWithOptions(context.Background(), "계약 일정", 2, QueryOptions{
		Mode: SearchModeBM25, Explain: true, Intent: "대한전선", ForceIntent: true,
	})
	if err != nil {
		t.Fatalf("SearchWithOptions: %v", err)
	}
	if !report.Diagnostics.IntentApplied || len(report.Results) != 2 || report.Results[0].Path != "projects/z.md" {
		t.Fatalf("intent report = %+v", report)
	}
	for _, result := range report.Results {
		if result.Path == "projects/intent-only.md" {
			t.Fatalf("intent admitted a new candidate: %+v", report.Results)
		}
	}
	if report.Results[0].Explain == nil || report.Results[0].Explain.Intent == nil || report.Results[0].Explain.IntentBonus <= 0 {
		t.Fatalf("intent explanation = %+v", report.Results[0].Explain)
	}
}

func TestShouldIntentRerankOnlyRunsForAmbiguousOrWeakResults(t *testing.T) {
	strong := []SearchResult{{Score: 0.9}, {Score: 0.7}}
	ambiguous := []SearchResult{{Score: 0.9}, {Score: 0.85}}
	weak := []SearchResult{{Score: 0.5}, {Score: 0.2}}
	options := QueryOptions{Intent: "full intent"}
	if shouldIntentRerank(strong, options) {
		t.Fatal("clear strong ranking should skip intent work")
	}
	if !shouldIntentRerank(ambiguous, options) || !shouldIntentRerank(weak, options) {
		t.Fatal("ambiguous or weak ranking should enable intent reranking")
	}
	options.ForceIntent = true
	if !shouldIntentRerank(strong, options) {
		t.Fatal("forced diagnostic rerank should override the ambiguity gate")
	}
}
