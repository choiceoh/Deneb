package wiki

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestCuesFrontmatterRoundtrip pins the cue-anchor field through render→parse:
// a page written with cues must come back with the same cues, and a page
// without cues must stay cue-free (no phantom field).
func TestCuesFrontmatterRoundtrip(t *testing.T) {
	page := &Page{
		Meta: Frontmatter{
			Title: "썬샤인에너지 선수금 입금 일정", Category: "거래",
			Summary: "EPC 선수금 5천만원, 6월 15일 입금 예정",
			Cues:    []string{"계약금", "착수금"},
		},
		Body: "EPC 선수금 5천만원. 6월 15일 입금 예정.",
	}
	parsed, err := ParsePage(page.Render())
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	if got := strings.Join(parsed.Meta.Cues, ","); got != "계약금,착수금" {
		t.Fatalf("cues roundtrip = %q, want %q", got, "계약금,착수금")
	}

	bare, err := ParsePage((&Page{Meta: Frontmatter{Title: "t", Category: "기타"}, Body: "b"}).Render())
	if err != nil {
		t.Fatalf("ParsePage bare: %v", err)
	}
	if len(bare.Meta.Cues) != 0 {
		t.Fatalf("bare page grew cues: %v", bare.Meta.Cues)
	}
}

// TestSearchFindsPageByCueOnly is the causal proof for cue anchors: the query
// vocabulary ("계약금") exists ONLY in the page's cues — title/summary/body/tags
// all say "선수금" — so a lexical hit is reachable exclusively through cue
// indexing (searchablePageFields). The control store holds the identical page
// WITHOUT cues and must not surface it for the same query.
func TestSearchFindsPageByCueOnly(t *testing.T) {
	ctx := context.Background()
	makePage := func(cues []string) *Page {
		return &Page{
			Meta: Frontmatter{
				ID: "sunshine-downpayment", Title: "썬샤인에너지 선수금 입금 일정", Category: "거래",
				Summary: "썬샤인에너지 EPC 선수금 5천만원, 6월 15일 입금 예정",
				Tags:    []string{"썬샤인에너지", "선수금"},
				Cues:    cues,
			},
			Body: "EPC 선수금 5천만원. 6월 15일 입금 예정. 담당은 이서연 과장.",
		}
	}
	newStore := func(t *testing.T, cues []string) *Store {
		t.Helper()
		dir := t.TempDir()
		store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		if err := store.WritePage("거래/sunshine-downpayment.md", makePage(cues)); err != nil {
			t.Fatalf("WritePage: %v", err)
		}
		return store
	}

	const query = "계약금 언제 들어오기로 했지"

	// With cues: the paraphrase reaches the page.
	withCues := newStore(t, []string{"계약금", "착수금"})
	results, err := withCues.Search(ctx, query, 5)
	if err != nil {
		t.Fatalf("Search(with cues): %v", err)
	}
	found := false
	for _, r := range results {
		if r.Path == "거래/sunshine-downpayment.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("cue-anchored page not found for paraphrase query; results=%v", results)
	}

	// Control — same page, no cues: the paraphrase must NOT reach it (proves the
	// hit above came from cue indexing, not some other field).
	withoutCues := newStore(t, nil)
	results, err = withoutCues.Search(ctx, query, 5)
	if err != nil {
		t.Fatalf("Search(control): %v", err)
	}
	for _, r := range results {
		if r.Path == "거래/sunshine-downpayment.md" {
			t.Fatalf("control page surfaced WITHOUT cues — the test no longer isolates cue indexing; results=%v", results)
		}
	}
}

// TestSemanticTextIncludesCues — cue anchors must fold into the embedded text
// so the semantic index also pulls the page toward the question vocabulary
// (and a cue change re-embeds the page via the contentHash cache key).
func TestSemanticTextIncludesCues(t *testing.T) {
	page := &Page{
		Meta: Frontmatter{Title: "제목", Summary: "요약", Cues: []string{"계약금", "착수금"}},
		Body: "본문",
	}
	text := semanticText(page)
	if !strings.Contains(text, "계약금") || !strings.Contains(text, "착수금") {
		t.Fatalf("semanticText missing cues: %q", text)
	}
	if semanticText(&Page{Meta: Frontmatter{Title: "제목"}, Body: "본문"}) == text {
		t.Fatal("cue-less page produced identical semantic text — cache key would not change on cue edits")
	}
}
