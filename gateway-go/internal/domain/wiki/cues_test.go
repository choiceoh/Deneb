package wiki

import (
	"context"
	"fmt"
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

// TestCuesNormalizedOnWrite — the write-time choke point must trim, drop
// empties, dedupe, and cap cues no matter which producer set them (the 10-cue
// cap used to hold only on the dreamer's merge branch).
func TestCuesNormalizedOnWrite(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cues := []string{" 계약금 ", "", "계약금", "  "}
	for i := 0; i < 12; i++ {
		cues = append(cues, fmt.Sprintf("진입표현%d", i))
	}
	page := &Page{Meta: Frontmatter{Title: "t", Category: "기타", Cues: cues}, Body: "b"}
	if err := store.WritePage("기타/cue-cap.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	got, err := store.ReadPage("기타/cue-cap.md")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(got.Meta.Cues) != 10 {
		t.Fatalf("cues not capped at write: %d %v", len(got.Meta.Cues), got.Meta.Cues)
	}
	if got.Meta.Cues[0] != "계약금" || got.Meta.Cues[1] != "진입표현0" {
		t.Fatalf("cues not trimmed/deduped in order: %v", got.Meta.Cues[:2])
	}
}

// TestMergePreservesCues — folding a duplicate page must union its cue anchors
// into the survivor; dropping them would kill the paraphrase queries the
// source page used to answer.
func TestMergePreservesCues(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	target := &Page{Meta: Frontmatter{Title: "선수금 일정", Category: "기타", Cues: []string{"계약금"}}, Body: "target"}
	source := &Page{Meta: Frontmatter{Title: "선수금 일정 (중복)", Category: "기타", Cues: []string{"착수금", "계약금"}}, Body: "source"}
	if err := store.WritePage("기타/target.md", target); err != nil {
		t.Fatalf("WritePage target: %v", err)
	}
	if err := store.WritePage("기타/source.md", source); err != nil {
		t.Fatalf("WritePage source: %v", err)
	}
	if _, err := store.MergePage("기타/target.md", "기타/source.md", "merged body", MergeOptions{}); err != nil {
		t.Fatalf("MergePage: %v", err)
	}
	got, err := store.ReadPage("기타/target.md")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	joined := strings.Join(got.Meta.Cues, ",")
	if joined != "계약금,착수금" {
		t.Fatalf("merged cues = %q, want 계약금,착수금 (union, dst priority, deduped)", joined)
	}
}

// TestSearchValidityBeforeTruncate — an archived page whose IDENTITY matches
// the query (boosted) must not crowd the current body-match page out of a
// small result window: validity demotion has to apply before the limit cut.
func TestSearchValidityBeforeTruncate(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	archived := &Page{
		Meta: Frontmatter{Title: "정산 프로세스 (구)", Category: "기타", Archived: true},
		Body: "낡은 절차 문서.",
	}
	current := &Page{
		Meta: Frontmatter{Title: "월말 마감 절차", Category: "기타"},
		Body: "정산 확정은 5영업일 안에 끝낸다.",
	}
	if err := store.WritePage("기타/old-settle.md", archived); err != nil {
		t.Fatalf("WritePage archived: %v", err)
	}
	if err := store.WritePage("기타/current-settle.md", current); err != nil {
		t.Fatalf("WritePage current: %v", err)
	}

	results, err := store.Search(context.Background(), "정산", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d (%v)", len(results), results)
	}
	if results[0].Path != "기타/current-settle.md" {
		t.Fatalf("top result = %s — archived identity match crowded out the current page (validity applied after truncation?)", results[0].Path)
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

// TestSearch_CueMatchDoesNotLeakCueTextAsSnippet: cues are retrieval anchors,
// not content — a cue-only match must find the page but the result snippet
// (recall evidence "match:", memory search) must come from visible fields.
func TestSearch_CueMatchDoesNotLeakCueTextAsSnippet(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	page := &Page{
		Meta: Frontmatter{
			ID: "dangjin-deal", Title: "당진 프로젝트 자금 흐름", Category: "프로젝트",
			Cues: []string{"계약금 선수금 회수 일정"},
		},
		Body: "당진 건 자금 흐름 정리. 1차 지급은 착공 후.",
	}
	if err := store.WritePage("프로젝트/당진/대표.md", page); err != nil {
		t.Fatal(err)
	}

	results, err := store.Search(context.Background(), "계약금", 5)
	if err != nil || len(results) == 0 {
		t.Fatalf("cue match must still find the page: %v (%d)", err, len(results))
	}
	if strings.Contains(results[0].Content, "계약금") {
		t.Fatalf("cue text leaked into the snippet: %q", results[0].Content)
	}
}
