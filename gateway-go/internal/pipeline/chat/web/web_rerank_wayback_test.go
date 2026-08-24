package web

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

// Search+fetch only pulls the first few results, so the order decides which
// pages the model ever sees. Without the sidecar it must stay exactly as the
// provider returned it — an optional ranking pass may never reshuffle or drop.
func TestSearchRerankIsANoOpWithoutTheSidecar(t *testing.T) {
	SetSearchReranker(nil)
	results := []searchResult{
		{Title: "one", URL: "https://a.example"},
		{Title: "two", URL: "https://b.example"},
		{Title: "three", URL: "https://c.example"},
	}

	got := rerankSearchResults(context.Background(), "query", results)

	if len(got) != len(results) {
		t.Fatalf("results were dropped: %d → %d", len(results), len(got))
	}
	for i := range results {
		if got[i].URL != results[i].URL {
			t.Fatalf("order changed without a reranker at %d: %s", i, got[i].URL)
		}
	}
}

func TestSearchRerankSkipsListsTooShortToReorder(t *testing.T) {
	// Below three there is nothing a reorder can achieve that is worth a call.
	two := []searchResult{{URL: "https://a.example"}, {URL: "https://b.example"}}
	if got := rerankSearchResults(context.Background(), "query", two); len(got) != 2 {
		t.Fatalf("short list was altered: %v", got)
	}
}

func TestRerankDocumentPrefersTitleThenSnippet(t *testing.T) {
	both := searchRerankDocument(searchResult{Title: "Ownership", Description: "how borrowing works"})
	if !strings.HasPrefix(both, "Ownership") {
		t.Fatalf("title should lead the scored document: %q", both)
	}
	if !strings.Contains(both, "how borrowing works") {
		t.Fatalf("snippet was dropped: %q", both)
	}
	// A result missing one half must not produce a stray separator.
	if got := searchRerankDocument(searchResult{Title: "Only title"}); got != "Only title" {
		t.Fatalf("title-only document is malformed: %q", got)
	}
	if got := searchRerankDocument(searchResult{Description: "only snippet"}); got != "only snippet" {
		t.Fatalf("snippet-only document is malformed: %q", got)
	}
}

// Only "gone" means gone. Anything the stealth ladder or a retry could still
// recover must not be answered with a stale copy.
func TestArchiveIsOnlyForPagesThatAreActuallyGone(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		if !isGoneStatus(status) {
			t.Fatalf("status %d should be treated as gone", status)
		}
	}
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, 0} {
		if isGoneStatus(status) {
			t.Fatalf("status %d must stay with the live fetch path", status)
		}
	}
}

func TestFetchErrorCodeCarriesItsStatus(t *testing.T) {
	if got := statusFromFetchErrCode("http_404"); got != 404 {
		t.Fatalf("http_404 → %d", got)
	}
	if got := statusFromFetchErrCode("http_410"); got != 410 {
		t.Fatalf("http_410 → %d", got)
	}
	// Non-HTTP failures (DNS, TLS, timeout) say nothing about the page existing.
	for _, code := range []string{"timeout", "dns_error", "ssrf_blocked", "http_", "http_4o4"} {
		if got := statusFromFetchErrCode(code); got != 0 {
			t.Fatalf("%q should not yield a status, got %d", code, got)
		}
	}
}

// Reading an archived copy without being told is a correctness problem: the
// model would cite stale facts as current.
func TestArchiveSubstitutionIsDisclosedWithItsDate(t *testing.T) {
	note := waybackNote("https://example.com/gone", waybackSnapshot{
		URL: "https://web.archive.org/web/20240115120000/https://example.com/gone", Timestamp: "20240115120000",
	})
	if !strings.Contains(note, "Internet Archive") {
		t.Fatalf("substitution not disclosed: %q", note)
	}
	if !strings.Contains(note, "2024-01-15") {
		t.Fatalf("snapshot date missing, so the model cannot judge staleness: %q", note)
	}
	// A malformed timestamp still discloses the substitution.
	bare := waybackNote("https://example.com/gone", waybackSnapshot{URL: "x", Timestamp: "nonsense"})
	if !strings.Contains(bare, "Internet Archive") {
		t.Fatalf("substitution not disclosed without a date: %q", bare)
	}
}

// End-to-end wiring for the archive path: a gone page is looked up, the snapshot
// is fetched, and the substitution is disclosed. Exercised through the seams
// because a live 404-that-is-also-archived is situational — the whole point of
// this path is that it is a last resort the earlier ladders usually prevent.
func TestGonePageIsServedFromTheArchive(t *testing.T) {
	origFetch := fetchWithRetryFn
	origLookup := waybackLookupFn
	t.Cleanup(func() {
		fetchWithRetryFn = origFetch
		waybackLookupFn = origLookup
	})

	const dead = "https://example.com/removed"
	const snapshot = "https://web.archive.org/web/20240115120000/https://example.com/removed"

	fetchWithRetryFn = func(_ context.Context, u string, _ int64) (*media.FetchResult, error) {
		if u == dead {
			return nil, &media.MediaFetchError{Code: media.ErrHTTPError, Status: http.StatusNotFound, Message: "not found"}
		}
		if u != snapshot {
			t.Fatalf("unexpected fetch of %q", u)
		}
		return &media.FetchResult{
			Data:        []byte("<html><body><h1>Archived title</h1><p>" + strings.Repeat("archived body ", 40) + "</p></body></html>"),
			ContentType: "text/html",
			StatusCode:  http.StatusOK,
			FinalURL:    snapshot,
		}, nil
	}
	waybackLookupFn = func(context.Context, string) (waybackSnapshot, bool) {
		return waybackSnapshot{URL: snapshot, Timestamp: "20240115120000"}, true
	}

	out, err := webFetchViaStealth(context.Background(), nil, dead, 20000)
	if err != nil {
		t.Fatalf("archive recovery returned an error: %v", err)
	}
	if !strings.Contains(out.Content, "archived body") {
		t.Fatalf("archived content was not served:\n%s", out.Content)
	}
	if !strings.Contains(out.Content, "Archived: original "+dead) {
		t.Fatalf("substitution was not disclosed:\n%s", out.Content)
	}
	if !strings.Contains(out.Content, "2024-01-15") {
		t.Fatalf("snapshot date missing:\n%s", out.Content)
	}
	if out.Assess.HasError {
		t.Fatal("a recovered page was still reported as a failure")
	}
}

// A blocked page must go to the stealth ladder, not the archive: answering a
// 403 with a stale copy would hide a page that is very much still there.
func TestBlockedPageIsNotAnsweredFromTheArchive(t *testing.T) {
	origFetch := fetchWithRetryFn
	origLookup := waybackLookupFn
	t.Cleanup(func() {
		fetchWithRetryFn = origFetch
		waybackLookupFn = origLookup
	})

	fetchWithRetryFn = func(context.Context, string, int64) (*media.FetchResult, error) {
		return nil, &media.MediaFetchError{Code: media.ErrHTTPError, Status: http.StatusForbidden, Message: "blocked"}
	}
	waybackLookupFn = func(context.Context, string) (waybackSnapshot, bool) {
		t.Fatal("the archive was consulted for a page that is merely blocked")
		return waybackSnapshot{}, false
	}

	out, err := webFetchViaStealth(context.Background(), nil, "https://example.com/blocked", 20000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.Content, "http_403") {
		t.Fatalf("the original block was not reported:\n%s", out.Content)
	}
}
