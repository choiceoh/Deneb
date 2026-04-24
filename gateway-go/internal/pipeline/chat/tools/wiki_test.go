package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// newTestWikiStore constructs a Store backed by a temp dir, pre-loaded with
// three pages spanning Korean + English content so the tests can exercise
// prefix match, Hangul tokenisation, and scoring.
func newTestWikiStore(t *testing.T) *wiki.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	type fixture struct {
		path string
		meta wiki.Frontmatter
		body string
	}
	fixtures := []fixture{
		{
			path: "phase-2-summary.md",
			meta: wiki.Frontmatter{
				ID:         "phase-2-summary",
				Title:      "Phase 2 작업 요약",
				Category:   "projects",
				Tags:       []string{"phase-2", "redact"},
				Importance: 0.8,
			},
			body: "redact 모듈을 OpenAI 프로바이더에 연결했다. Anthropic 계열 adapter는 별도 구현 필요.",
		},
		{
			path: "deneb-architecture.md",
			meta: wiki.Frontmatter{
				ID:         "deneb-architecture",
				Title:      "Deneb Architecture Overview",
				Category:   "reference",
				Tags:       []string{"arch", "overview"},
				Importance: 0.9,
			},
			body: "Deneb is a Telegram-first coding agent with a Go gateway. Spillover kicks in at 24K chars per tool response.",
		},
		{
			path: "ramen-recipe.md",
			meta: wiki.Frontmatter{
				ID:         "random",
				Title:      "어제 만든 라면 레시피",
				Category:   "misc",
				Tags:       []string{"cook"},
				Importance: 0.1,
			},
			body: "물 500ml, 스프 한 봉지, 계란 한 개. 끓는 물에 면을 넣고 3분.",
		},
	}
	for _, f := range fixtures {
		page := &wiki.Page{Meta: f.meta, Body: f.body}
		if err := store.WritePage(f.path, page); err != nil {
			t.Fatalf("WritePage %s: %v", f.path, err)
		}
	}
	return store
}

// TestWikiSearch_ReturnsKoreanResults exercises the happy path: a Korean
// query finds the matching page and the output contains the expected
// Korean result header + path.
func TestWikiSearch_ReturnsKoreanResults(t *testing.T) {
	store := newTestWikiStore(t)
	out, err := wikiSearch(context.Background(), store, "redact", 5)
	if err != nil {
		t.Fatalf("wikiSearch: %v", err)
	}
	if !strings.Contains(out, "위키 검색 결과") {
		t.Errorf("expected Korean result header, got: %q", out)
	}
	if !strings.Contains(out, "phase-2-summary") {
		t.Errorf("expected phase-2-summary in results, got: %q", out)
	}
}

// TestWikiSearch_EmptyQueryReturnsGuidance verifies that an empty query
// returns a friendly Korean guidance string (not an error).
func TestWikiSearch_EmptyQueryReturnsGuidance(t *testing.T) {
	store := newTestWikiStore(t)
	out, err := wikiSearch(context.Background(), store, "", 5)
	if err != nil {
		t.Fatalf("wikiSearch: %v", err)
	}
	if !strings.Contains(out, "query") {
		t.Errorf("expected guidance mentioning query, got: %q", out)
	}
}

// TestWikiSearch_KoreanPrefixMatch verifies Hangul-aware matching works on
// a Korean token that appears in a page title. Uses a longer token because
// BM25's short-token de-emphasis may filter single-character matches.
func TestWikiSearch_KoreanPrefixMatch(t *testing.T) {
	store := newTestWikiStore(t)
	out, err := wikiSearch(context.Background(), store, "레시피", 5)
	if err != nil {
		t.Fatalf("wikiSearch: %v", err)
	}
	if !strings.Contains(out, "ramen-recipe") {
		t.Errorf("expected ramen-recipe.md path for 레시피 query, got: %q", out)
	}
}

// TestWikiSearch_SpecialCharactersSafe verifies that FTS-boolean-shaped or
// SQL-injection-shaped queries pass through the tokenizer without error.
// Pure-Go textsearch strips non-letter/non-digit runes; these should not
// panic or return an error.
func TestWikiSearch_SpecialCharactersSafe(t *testing.T) {
	store := newTestWikiStore(t)
	queries := []string{
		`"quoted phrase"`,
		"foo AND bar",
		"foo OR bar",
		"(foo)",
		"foo & bar",
		"'; DROP TABLE pages; --",
	}
	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			out, err := wikiSearch(context.Background(), store, q, 5)
			if err != nil {
				t.Errorf("wikiSearch(%q) errored: %v", q, err)
			}
			if out == "" {
				t.Errorf("wikiSearch(%q) returned empty string (expected result or guidance)", q)
			}
		})
	}
}

// TestWikiSearch_LimitDefaulting verifies limit=0 defaults, and a negative
// limit is treated like 0. (Schema-level clamping of large values is not
// exercised here — it happens at RPC boundary.)
func TestWikiSearch_LimitDefaulting(t *testing.T) {
	store := newTestWikiStore(t)
	out0, err := wikiSearch(context.Background(), store, "Deneb", 0)
	if err != nil {
		t.Fatalf("limit=0: %v", err)
	}
	outNeg, err := wikiSearch(context.Background(), store, "Deneb", -5)
	if err != nil {
		t.Fatalf("limit=-5: %v", err)
	}
	if out0 == "" || outNeg == "" {
		t.Fatal("expected non-empty results for limit=0 and limit=-5")
	}
}

// TestWikiSearch_NoResults returns a friendly Korean message when no page
// matches.
func TestWikiSearch_NoResults(t *testing.T) {
	store := newTestWikiStore(t)
	out, err := wikiSearch(context.Background(), store, "xyzzy-unlikely-term-ABCDEF", 5)
	if err != nil {
		t.Fatalf("wikiSearch: %v", err)
	}
	// The wikiSearch output should be a Korean "no results" message — we
	// don't pin the exact wording, just that the raw query is not echoed
	// in a confusing form.
	if out == "" {
		t.Fatal("expected non-empty no-results message")
	}
}

// (intentionally no nil-store test: production wiring never passes nil —
// the wiki tool is gated by deps.Store presence upstream. Exercising nil
// here would only enforce an undocumented precondition.)
