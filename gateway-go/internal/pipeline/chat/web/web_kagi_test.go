package web

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

// Serper is the default provider: when its key is set and the search succeeds,
// webSearch returns its results without touching Kagi.
func TestWebSearchPrefersSerperWhenKeyed(t *testing.T) {
	t.Setenv("SERPER_API_KEY", "serper-test")
	t.Setenv("KAGI_API_KEY", "kagi-test")

	origKagi := kagiSearchRawFn
	origSerper := serperSearchRawFn
	t.Cleanup(func() {
		kagiSearchRawFn = origKagi
		serperSearchRawFn = origSerper
	})

	serperSearchRawFn = func(context.Context, string, string, int) ([]searchResult, serperAnswerBox, string, error) {
		return []searchResult{{Title: "Serper Hit", URL: "https://serper.example/a", Description: "ok"}}, serperAnswerBox{}, "", nil
	}
	kagiSearchRawFn = func(context.Context, string, string, int) ([]searchResult, error) {
		t.Fatal("kagi should not run when Serper succeeds")
		return nil, nil
	}

	out, err := webSearch(context.Background(), "q", 3)
	if err != nil {
		t.Fatalf("webSearch: %v", err)
	}
	if !strings.Contains(out, "Serper Hit") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

// A Serper failure falls through to Kagi (the first fallback).
func TestWebSearchFallsThroughSerperFailureToKagi(t *testing.T) {
	t.Setenv("SERPER_API_KEY", "serper-test")
	t.Setenv("KAGI_API_KEY", "kagi-test")

	origKagi := kagiSearchRawFn
	origSerper := serperSearchRawFn
	t.Cleanup(func() {
		kagiSearchRawFn = origKagi
		serperSearchRawFn = origSerper
	})

	serperSearchRawFn = func(context.Context, string, string, int) ([]searchResult, serperAnswerBox, string, error) {
		return nil, serperAnswerBox{}, "", errors.New("serper HTTP 500")
	}
	kagiSearchRawFn = func(context.Context, string, string, int) ([]searchResult, error) {
		return []searchResult{{Title: "Kagi Hit", URL: "https://kagi.example/a", Description: "ok"}}, nil
	}

	out, err := webSearch(context.Background(), "q", 3)
	if err != nil {
		t.Fatalf("webSearch: %v", err)
	}
	if !strings.Contains(out, "Kagi Hit") || !strings.Contains(out, "https://kagi.example/a") {
		t.Fatalf("expected Kagi fallback, got:\n%s", out)
	}
}

// webSearchWithURLs falls through to Kagi organic results (no answer/knowledge
// links) when Serper fails.
func TestWebSearchWithURLsFallsThroughSerperToKagi(t *testing.T) {
	t.Setenv("SERPER_API_KEY", "serper-test")
	t.Setenv("KAGI_API_KEY", "kagi-test")

	origKagi := kagiSearchRawFn
	origSerper := serperSearchRawFn
	t.Cleanup(func() {
		kagiSearchRawFn = origKagi
		serperSearchRawFn = origSerper
	})

	serperSearchRawFn = func(context.Context, string, string, int) ([]searchResult, serperAnswerBox, string, error) {
		return nil, serperAnswerBox{}, "", errors.New("serper down")
	}
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

func TestNextSearchProviderOrder(t *testing.T) {
	t.Setenv("KAGI_API_KEY", "k")
	t.Setenv("SERPER_API_KEY", "s")
	t.Setenv("BRAVE_SEARCH_API_KEY", "b")
	// Serper's first fallback is Kagi.
	if got := nextSearchProvider("serper"); got != "kagi" {
		t.Fatalf("serper→ want kagi, got %q", got)
	}
	// Kagi's fallback is Brave.
	if got := nextSearchProvider("kagi"); got != "brave" {
		t.Fatalf("kagi→ want brave, got %q", got)
	}
	// Without Kagi, Serper falls straight to Brave.
	t.Setenv("KAGI_API_KEY", "")
	t.Setenv("KAGI_API_TOKEN", "")
	if got := nextSearchProvider("serper"); got != "brave" {
		t.Fatalf("serper→ (no kagi) want brave, got %q", got)
	}
	// Without Kagi or Brave, Kagi's slot falls to DuckDuckGo.
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	t.Setenv("BRAVE_API_KEY", "")
	if got := nextSearchProvider("kagi"); got != "duckduckgo" {
		t.Fatalf("kagi→ (no brave) want duckduckgo, got %q", got)
	}
}

// kagiFetchEscalationEligible fires only for "blocked/transient" fetch errors
// where content likely exists, never for permanent ones.
func TestKagiFetchEscalationEligible(t *testing.T) {
	eligible := []string{"http_403", "http_429", "http_500", "http_503", "timeout", "connection_refused", "connection_reset", "fetch_failed"}
	for _, code := range eligible {
		if !kagiFetchEscalationEligible(webFetchErr{Code: code}) {
			t.Errorf("code %q should be escalation-eligible", code)
		}
	}
	permanent := []string{"http_404", "http_401", "dns_failure", "ssrf_blocked", "tls_error", "redirect_loop", "content_too_large", "canceled", "unknown"}
	for _, code := range permanent {
		if kagiFetchEscalationEligible(webFetchErr{Code: code}) {
			t.Errorf("code %q should NOT be escalation-eligible", code)
		}
	}
}

// kagiExtractEscalate returns (,false) with no key rather than calling out.
func TestKagiExtractEscalateNoKey(t *testing.T) {
	t.Setenv("KAGI_API_KEY", "")
	t.Setenv("KAGI_API_TOKEN", "")
	if md, ok := kagiExtractEscalate(context.Background(), "https://example.com/"); ok || md != "" {
		t.Fatalf("expected empty/false without key, got ok=%v md=%q", ok, md)
	}
}

// When the free headless backends (sidecar, Jina) both yield nothing, the
// thin-content escalation falls through to Kagi Extract and accepts its richer
// markdown.
func TestEscalateThinContentFallsThroughToKagi(t *testing.T) {
	origBrowser := browserRenderFn
	origJina := jinaFetchFn
	origKagi := kagiExtractEscalateFn
	t.Cleanup(func() {
		browserRenderFn = origBrowser
		jinaFetchFn = origJina
		kagiExtractEscalateFn = origKagi
	})
	browserRenderFn = func(context.Context, string, int64) (*media.FetchResult, error) { return nil, nil }
	jinaFetchFn = func(context.Context, string, int64) (*media.FetchResult, error) { return nil, nil }
	kagiMarkdown := strings.Repeat("kagi rendered markdown content. ", 20)
	kagiExtractEscalateFn = func(context.Context, string) (string, bool) { return kagiMarkdown, true }

	meta := &webFetchMeta{URL: "https://spa.example/", ExtractChars: 10, Signals: []string{"js_required"}}
	content, ok := escalateThinContent(context.Background(), "https://spa.example/", 4096, nil, meta)
	if !ok || !strings.Contains(content, "kagi rendered markdown") {
		t.Fatalf("expected Kagi escalation content, ok=%v", ok)
	}
	found := false
	for _, s := range meta.Signals {
		if s == "escalated_kagi" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected escalated_kagi signal, got %v", meta.Signals)
	}
}
