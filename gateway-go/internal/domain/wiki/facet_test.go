package wiki

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// facet-anchored test page: the query vocabulary (거래처 "금호타이어", 현장
// "수산리") exists ONLY in frontmatter identity metadata — title/summary/body/
// tags all describe the deal without naming the counterparty or site — so a
// lexical hit is reachable exclusively through the hidden facet field
// (facetText). Mirrors cues_test.go's causal-proof structure.
func facetTestPage(withFacet bool) *Page {
	page := &Page{
		Meta: Frontmatter{
			ID: "gunsan-rooftop-epc", Title: "군산 루프탑 EPC 계약 진행",
			Category: "프로젝트",
			Summary:  "루프탑 EPC 본계약 협상, 8월 착공 목표",
			Tags:     []string{"EPC", "루프탑"},
		},
		Body: "본계약 조건 협상 중. 8월 착공 목표로 인허가 서류 준비.",
	}
	if withFacet {
		page.Meta.Code = "pl3-kmh-epc-001"
		page.Meta.Client = "금호타이어"
		page.Meta.Sites = []string{"전북 군산시 옥구읍 수산리"}
	}
	return page
}

func newFacetTestStore(t *testing.T, withFacet bool) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.WritePage("프로젝트/gunsan-rooftop-epc.md", facetTestPage(withFacet)); err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	return store
}

// TestSearchReturnsPageByFacetOnly is the causal proof for the facet field:
// counterparty and site queries reach a page whose prose never names them, and
// the control (identical page without Client/Sites/Code) must not surface.
func TestSearchReturnsPageByFacetOnly(t *testing.T) {
	ctx := context.Background()
	const pagePath = "프로젝트/gunsan-rooftop-epc.md"

	// Query vocabulary is fully disjoint from the page's prose fields (title/
	// summary/body/tags) — in a tiny corpus the rarity gate is off, so ANY
	// shared token (e.g. "계약") would let the control match and break the
	// causal isolation.
	withFacet := newFacetTestStore(t, true)
	for _, query := range []string{"금호타이어 근황 알려줘", "수산리 요즘 어때"} {
		results, err := withFacet.Search(ctx, query, 5)
		if err != nil {
			t.Fatalf("Search(%q): %v", query, err)
		}
		found := false
		for _, r := range results {
			if r.Path == pagePath {
				found = true
				// Hidden contract: facet metadata must never surface as the
				// match snippet (same rule as cue anchors).
				if strings.Contains(r.Content, "금호타이어") || strings.Contains(r.Content, "수산리") {
					t.Fatalf("facet metadata leaked into snippet for %q: %q", query, r.Content)
				}
			}
		}
		if !found {
			t.Fatalf("facet-anchored page not found for %q; results=%v", query, results)
		}
	}

	// Control — same page, no facet metadata: the counterparty query must NOT
	// reach it (proves the hits above came from facet indexing).
	control := newFacetTestStore(t, false)
	results, err := control.Search(ctx, "금호타이어 근황 알려줘", 5)
	if err != nil {
		t.Fatalf("Search(control): %v", err)
	}
	for _, r := range results {
		if r.Path == pagePath {
			t.Fatalf("control page surfaced WITHOUT facet metadata — the test no longer isolates facet indexing; results=%v", results)
		}
	}
}

// TestFacetBoostZeroDisables pins the A/B lever: DENEB_WIKI_FACET_BOOST=0 must
// omit the facet field entirely, restoring the pre-facet baseline index.
func TestFacetBoostZeroDisables(t *testing.T) {
	t.Setenv("DENEB_WIKI_FACET_BOOST", "0")
	store := newFacetTestStore(t, true)
	results, err := store.Search(context.Background(), "금호타이어 근황 알려줘", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.Path == "프로젝트/gunsan-rooftop-epc.md" {
			t.Fatalf("facet field still indexed at boost 0; results=%v", results)
		}
	}
}
