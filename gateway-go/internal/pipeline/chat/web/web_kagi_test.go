package web

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Kagi is the highest-priority provider: when its key is set and the search
// succeeds, webSearch returns its results without touching Serper.
func TestWebSearchPrefersKagiWhenKeyed(t *testing.T) {
	t.Setenv("KAGI_API_KEY", "kagi-test")
	t.Setenv("SERPER_API_KEY", "serper-test")

	origKagi := kagiSearchRawFn
	origSerper := serperSearchRawFn
	t.Cleanup(func() {
		kagiSearchRawFn = origKagi
		serperSearchRawFn = origSerper
	})

	kagiSearchRawFn = func(context.Context, string, string, int) ([]searchResult, error) {
		return []searchResult{{Title: "Kagi Hit", URL: "https://kagi.example/a", Description: "ok"}}, nil
	}
	serperSearchRawFn = func(context.Context, string, string, int) ([]searchResult, serperAnswerBox, string, error) {
		t.Fatal("serper should not run when Kagi succeeds")
		return nil, serperAnswerBox{}, "", nil
	}

	out, err := webSearch(context.Background(), "q", 3)
	if err != nil {
		t.Fatalf("webSearch: %v", err)
	}
	if !strings.Contains(out, "Kagi Hit") || !strings.Contains(out, "https://kagi.example/a") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

// A Kagi failure falls through to the next keyed provider (Serper).
func TestWebSearchFallsThroughKagiFailureToSerper(t *testing.T) {
	t.Setenv("KAGI_API_KEY", "kagi-test")
	t.Setenv("SERPER_API_KEY", "serper-test")

	origKagi := kagiSearchRawFn
	origSerper := serperSearchRawFn
	t.Cleanup(func() {
		kagiSearchRawFn = origKagi
		serperSearchRawFn = origSerper
	})

	kagiSearchRawFn = func(context.Context, string, string, int) ([]searchResult, error) {
		return nil, errors.New("kagi HTTP 401")
	}
	serperSearchRawFn = func(context.Context, string, string, int) ([]searchResult, serperAnswerBox, string, error) {
		return []searchResult{{Title: "Serper Hit", URL: "https://serper.example/a", Description: "ok"}}, serperAnswerBox{}, "", nil
	}

	out, err := webSearch(context.Background(), "q", 3)
	if err != nil {
		t.Fatalf("webSearch: %v", err)
	}
	if !strings.Contains(out, "Serper Hit") {
		t.Fatalf("expected Serper fallback, got:\n%s", out)
	}
}

// webSearchWithURLs surfaces Kagi organic results (no answer/knowledge links).
func TestWebSearchWithURLsPrefersKagi(t *testing.T) {
	t.Setenv("KAGI_API_KEY", "kagi-test")
	t.Setenv("SERPER_API_KEY", "serper-test")

	origKagi := kagiSearchRawFn
	t.Cleanup(func() { kagiSearchRawFn = origKagi })

	kagiSearchRawFn = func(context.Context, string, string, int) ([]searchResult, error) {
		return []searchResult{{Title: "K", URL: "https://k.example/", Description: "d"}}, nil
	}

	out, results, answerLink, knowledgeLink, err := webSearchWithURLs(context.Background(), "q", 2)
	if err != nil {
		t.Fatalf("webSearchWithURLs: %v", err)
	}
	if answerLink != "" || knowledgeLink != "" {
		t.Fatalf("answerLink=%q knowledgeLink=%q, want empty for Kagi", answerLink, knowledgeLink)
	}
	if len(results) != 1 || results[0].URL != "https://k.example/" {
		t.Fatalf("results=%v", results)
	}
	if !strings.Contains(out, "https://k.example/") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

// kagiItemsToResults drops URL-less rows, maps snippet→Description, and caps at
// count.
func TestKagiItemsToResultsMapsFiltersAndCaps(t *testing.T) {
	items := []kagiV1SearchItem{
		{URL: "https://a.example/", Title: "A", Snippet: "sa"},
		{URL: "", Title: "no url"}, // no URL — dropped
		{URL: "https://b.example/", Title: "B", Snippet: "sb"},
		{URL: "https://c.example/", Title: "C", Snippet: "sc"},
	}
	got := kagiItemsToResults(items, 2)
	if len(got) != 2 {
		t.Fatalf("want 2 results (capped), got %d: %v", len(got), got)
	}
	if got[0].URL != "https://a.example/" || got[0].Description != "sa" {
		t.Fatalf("mapping wrong: %+v", got[0])
	}
	if got[1].URL != "https://b.example/" {
		t.Fatalf("unexpected results: %v", got)
	}
	// count<=0 means no cap.
	if all := kagiItemsToResults(items, 0); len(all) != 3 {
		t.Fatalf("want 3 uncapped results, got %d", len(all))
	}
}

// Extract returns a user-facing (non-error) string when the key is missing,
// rather than failing the tool call.
func TestWebKagiExtractReportsMissingKey(t *testing.T) {
	t.Setenv("KAGI_API_KEY", "")
	t.Setenv("KAGI_API_TOKEN", "")

	out, err := webKagiExtract(context.Background(), "https://example.com/")
	if err != nil {
		t.Fatalf("webKagiExtract err: %v", err)
	}
	if !strings.Contains(out, "KAGI_API_KEY") {
		t.Fatalf("expected missing-key notice, got:\n%s", out)
	}
}

func TestNextSearchProviderFromKagi(t *testing.T) {
	t.Setenv("KAGI_API_KEY", "k")
	t.Setenv("SERPER_API_KEY", "s")
	t.Setenv("BRAVE_SEARCH_API_KEY", "b")
	if got := nextSearchProvider("kagi"); got != "serper" {
		t.Fatalf("with serper keyed, want serper, got %q", got)
	}

	t.Setenv("SERPER_API_KEY", "")
	if got := nextSearchProvider("kagi"); got != "brave" {
		t.Fatalf("without serper, want brave, got %q", got)
	}

	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("BRAVE_API_KEY", "")
	if got := nextSearchProvider("kagi"); got != "duckduckgo" {
		t.Fatalf("without serper/brave, want duckduckgo, got %q", got)
	}
}
