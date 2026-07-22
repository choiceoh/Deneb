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

// kagiItemsToResults drops related-search rows (t==1) and URL-less rows, and
// caps at count.
func TestKagiItemsToResultsFiltersAndCaps(t *testing.T) {
	items := []kagiSearchItem{
		{T: 0, URL: "https://a.example/", Title: "A", Snippet: "sa"},
		{T: 1, URL: ""},                  // related searches row — dropped
		{T: 0, URL: "", Title: "no url"}, // no URL — dropped
		{T: 0, URL: "https://b.example/", Title: "B", Snippet: "sb"},
		{T: 0, URL: "https://c.example/", Title: "C", Snippet: "sc"},
	}
	got := kagiItemsToResults(items, 2)
	if len(got) != 2 {
		t.Fatalf("want 2 results (capped), got %d: %v", len(got), got)
	}
	if got[0].URL != "https://a.example/" || got[1].URL != "https://b.example/" {
		t.Fatalf("unexpected results: %v", got)
	}
	// count<=0 means no cap.
	if all := kagiItemsToResults(items, 0); len(all) != 3 {
		t.Fatalf("want 3 uncapped results, got %d", len(all))
	}
}

func TestFormatKagiFastGPT(t *testing.T) {
	out := formatKagiFastGPT("The answer [1].", []kagiFastGPTReference{
		{Title: "Ref One", URL: "https://one.example/", Snippet: "snip"},
	})
	for _, want := range []string{"**Answer:**", "The answer [1].", "**References:**", "Ref One", "https://one.example/", "snip"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if empty := formatKagiFastGPT("", nil); empty != "No answer found." {
		t.Fatalf("empty case: %q", empty)
	}
}

func TestIsKagiSearchType(t *testing.T) {
	for _, ty := range []string{"fastgpt", "enrich_web", "enrich_news"} {
		if !isKagiSearchType(ty) {
			t.Errorf("%q should be a Kagi type", ty)
		}
	}
	for _, ty := range []string{"news", "scholar", "autocomplete", "search", ""} {
		if isKagiSearchType(ty) {
			t.Errorf("%q should NOT be a Kagi type", ty)
		}
	}
}

// Kagi typed search and summarize both return a user-facing (non-error) string
// when the key is missing, rather than failing the tool call.
func TestKagiModesReportMissingKey(t *testing.T) {
	t.Setenv("KAGI_API_KEY", "")
	t.Setenv("KAGI_API_TOKEN", "")

	typed, err := kagiTypedSearch(context.Background(), "fastgpt", "q", 5)
	if err != nil {
		t.Fatalf("kagiTypedSearch err: %v", err)
	}
	if !strings.Contains(typed, "KAGI_API_KEY") {
		t.Fatalf("expected missing-key notice, got:\n%s", typed)
	}

	summ, err := webSummarize(context.Background(), "https://example.com/")
	if err != nil {
		t.Fatalf("webSummarize err: %v", err)
	}
	if !strings.Contains(summ, "KAGI_API_KEY") {
		t.Fatalf("expected missing-key notice, got:\n%s", summ)
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
