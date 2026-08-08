package wiki

import (
	"context"
	"path/filepath"
	"testing"
)

// The backfill contract: original results and their order are untouched;
// expansion hits only fill remaining slots, scored strictly below every
// original hit; a filled list or a disabled gate is byte-identical to today.
func TestBackfillWithExpansionContract(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// A page the ORIGINAL query cannot find (vocabulary gap): the user says
	// "독소조항", the page says "위약벌".
	mustWrite(t, store, "업무/계약-위약벌-검토.md", &Page{
		Meta: Frontmatter{ID: "penalty", Title: "계약 위약벌 검토"},
		Body: "위약벌 및 지체상금 조항 정리.",
	})

	expanded := 0
	store.SetQueryExpander(func(_ context.Context, intent string) []string {
		expanded++
		return []string{"위약벌"}
	})

	primary := []SearchResult{{Path: "업무/다른-페이지.md", Score: 0.9}}

	// Gate off → untouched, expander never called.
	t.Setenv("DENEB_WIKI_QUERY_EXPANSION", "")
	out := store.backfillWithExpansion(context.Background(), "독소조항 뭐 있었지", primary, 8)
	if len(out) != 1 || expanded != 0 {
		t.Fatalf("gate off must be inert: out=%d expanderCalls=%d", len(out), expanded)
	}

	// Gate on + under-filled → the gap page backfills BELOW the original hit.
	t.Setenv("DENEB_WIKI_QUERY_EXPANSION", "backfill")
	out = store.backfillWithExpansion(context.Background(), "독소조항 뭐 있었지", primary, 8)
	if len(out) != 2 || expanded != 1 {
		t.Fatalf("expected one backfilled hit: out=%d expanderCalls=%d", len(out), expanded)
	}
	if out[0].Path != "업무/다른-페이지.md" || out[0].Score != 0.9 {
		t.Errorf("original head must be untouched, got %+v", out[0])
	}
	if out[1].Path != "업무/계약-위약벌-검토.md" || out[1].Score >= out[0].Score {
		t.Errorf("backfilled hit must rank strictly below original, got %+v", out[1])
	}

	// A filled list never expands (structural p@1/r@8 no-regression on rich
	// queries — the 07-21 full-merge failure mode is unreachable).
	full := make([]SearchResult, 8)
	for i := range full {
		full[i] = SearchResult{Path: filepath.Join("업무", "p", string(rune('a'+i))+".md"), Score: 1.0 - float64(i)*0.05}
	}
	expanded = 0
	out = store.backfillWithExpansion(context.Background(), "독소조항", full, 8)
	if len(out) != 8 || expanded != 0 {
		t.Fatalf("filled list must be inert: out=%d expanderCalls=%d", len(out), expanded)
	}

	// Dedup: an expansion hit already present in the originals is not re-added.
	store.SetQueryExpander(func(_ context.Context, _ string) []string { return []string{"위약벌"} })
	primaryWithGap := []SearchResult{{Path: "업무/계약-위약벌-검토.md", Score: 0.8}}
	out = store.backfillWithExpansion(context.Background(), "독소조항", primaryWithGap, 8)
	if len(out) != 1 {
		t.Fatalf("duplicate path must not re-append, got %d results", len(out))
	}
}
