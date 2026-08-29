package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

func TestFetchCacheEvictsLRUAndExpiresEntries(t *testing.T) {
	defaults := NewFetchCache()
	if defaults.maxSize != fetchCacheDefaultMaxSize || defaults.ttl != fetchCacheDefaultTTL {
		t.Fatalf("default cache config = size %d ttl %s", defaults.maxSize, defaults.ttl)
	}
	if got, ok := defaults.Get("missing"); ok || got != "" {
		t.Fatalf("missing Get = (%q,%v)", got, ok)
	}

	cache := NewFetchCacheWithTTL(2, time.Hour)
	cache.Put("a", "one")
	cache.Put("b", "two")
	cache.Put("a", "one-refreshed") // refresh moves a to newest
	cache.Put("c", "three")         // b is now the oldest and must be evicted
	if got, ok := cache.Get("a"); !ok || got != "one-refreshed" {
		t.Fatalf("refreshed a = (%q,%v)", got, ok)
	}
	if got, ok := cache.Get("b"); ok || got != "" {
		t.Fatalf("evicted b = (%q,%v)", got, ok)
	}
	if got, ok := cache.Get("c"); !ok || got != "three" {
		t.Fatalf("new c = (%q,%v)", got, ok)
	}
	if len(cache.items) != 2 || cache.order.Len() != 2 {
		t.Fatalf("cache indexes diverged: items=%d order=%d", len(cache.items), cache.order.Len())
	}

	expiring := NewFetchCacheWithTTL(1, 5*time.Millisecond)
	expiring.Put("short", "lived")
	time.Sleep(10 * time.Millisecond)
	if got, ok := expiring.Get("short"); ok || got != "" {
		t.Fatalf("expired entry = (%q,%v)", got, ok)
	}
	if len(expiring.items) != 0 || expiring.order.Len() != 0 {
		t.Fatalf("lazy expiry left stale indexes: items=%d order=%d", len(expiring.items), expiring.order.Len())
	}
	// Removing an absent key is an idempotent internal cleanup operation.
	expiring.mu.Lock()
	expiring.removeLocked("short")
	expiring.mu.Unlock()

	fallback := NewFetchCacheWithTTL(0, time.Second)
	if fallback.maxSize != fetchCacheDefaultMaxSize {
		t.Fatalf("nonpositive max size = %d", fallback.maxSize)
	}
}

func TestFetchCacheConcurrentCapacityAndIntegrity(t *testing.T) {
	cache := NewFetchCacheWithTTL(8, time.Hour)
	const writers = 64
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			key := fmt.Sprintf("url-%d", i%16)
			cache.Put(key, fmt.Sprintf("value-%d", i))
			_, _ = cache.Get(key)
		}(i)
	}
	close(start)
	wg.Wait()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.items) > cache.maxSize || cache.order.Len() != len(cache.items) {
		t.Fatalf("concurrent cache integrity: size=%d max=%d order=%d", len(cache.items), cache.maxSize, cache.order.Len())
	}
	seen := make(map[string]bool)
	for e := cache.order.Front(); e != nil; e = e.Next() {
		key, ok := e.Value.(string)
		if !ok || seen[key] || cache.items[key] == nil || cache.items[key].element != e {
			t.Fatalf("invalid LRU element key=%q ok=%v seen=%v item=%#v", key, ok, seen[key], cache.items[key])
		}
		seen[key] = true
	}
}

func TestContentClassificationReturnsTypeAndDocumentName(t *testing.T) {
	cases := []struct {
		contentType string
		url         string
		want        fetchedContentType
	}{
		{"text/html; charset=utf-8", "https://example.com", contentTypeHTML},
		{"application/xhtml+xml", "https://example.com", contentTypeHTML},
		{"application/json", "https://example.com/data", contentTypeJSON},
		{"application/activity+json", "https://example.com/data", contentTypeJSON},
		{"application/pdf", "https://example.com/download", contentTypeDocument},
		{"application/octet-stream", "https://example.com/report.DOCX?token=x", contentTypeDocument},
		{"text/plain", "https://example.com/report.csv", contentTypeDocument},
		{"text/plain", "https://example.com/readme.txt", contentTypePlain},
		{"application/octet-stream", "https://example.com/no-extension", contentTypePlain},
	}
	for _, tc := range cases {
		if got := classifyContentType(tc.contentType, tc.url); got != tc.want {
			t.Errorf("classifyContentType(%q,%q) = %q, want %q", tc.contentType, tc.url, got, tc.want)
		}
	}

	names := map[string]string{
		"https://example.com/path/report.pdf?download=1": "report.pdf",
		"https://example.com/path/":                      "document",
		"https://example.com":                            "example.com",
		"":                                               "document",
		"   ":                                            "document",
		"relative/file.csv?x=1":                          "file.csv",
	}
	for input, want := range names {
		if got := documentName(input); got != want {
			t.Errorf("documentName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestProcessFetchedContentPlainJSONAndHTMLFallback(t *testing.T) {
	meta := &webFetchMeta{URL: "https://example.com", OrigChars: 5000}
	plain := processFetchedContent(context.Background(), "plain body", []byte("plain body"), "text/plain", meta.URL, nil, meta)
	if plain != "plain body" {
		t.Fatalf("plain processed = %q", plain)
	}

	jsonRaw := `{"b":2,"a":[1,true]}`
	pretty := processFetchedContent(context.Background(), jsonRaw, []byte(jsonRaw), "application/json", meta.URL, nil, meta)
	if pretty != "{\n  \"a\": [\n    1,\n    true\n  ],\n  \"b\": 2\n}" {
		t.Fatalf("JSON pretty output = %q", pretty)
	}
	invalid := `{not-json`
	if got := processJSON(invalid); got != invalid {
		t.Fatalf("invalid JSON changed to %q", got)
	}

	extractor := &LocalAIExtractor{state: localAIUnavailable, probeAt: time.Now()}
	html := `<!doctype html><html lang="en"><head><title>Example</title><meta name="description" content="summary"></head><body><nav>noise</nav><main><h1>Heading</h1><p>Main article paragraph with enough useful words.</p></main><footer>footer noise</footer></body></html>`
	htmlMeta := &webFetchMeta{URL: meta.URL, OrigChars: len(html)}
	converted := processFetchedContent(context.Background(), html, []byte(html), "text/html", meta.URL, extractor, htmlMeta)
	if !strings.Contains(converted, "Heading") || !strings.Contains(converted, "Main article") {
		t.Fatalf("HTML conversion lost content: %q", converted)
	}
	if strings.Contains(converted, "footer noise") || strings.Contains(converted, "noise") {
		t.Fatalf("HTML conversion retained noise: %q", converted)
	}
	if htmlMeta.Title != "Example" || htmlMeta.Description != "summary" || htmlMeta.Language != "en" {
		t.Fatalf("HTML metadata = %#v", htmlMeta)
	}
}

func TestFormatFetchResultIncludesOnlyMeaningfulMetadata(t *testing.T) {
	meta := webFetchMeta{
		Title:        "Title",
		Description:  "Description",
		Author:       "Author",
		SiteName:     "Site",
		URL:          "https://example.com/original",
		FinalURL:     "https://example.com/final",
		CanonicalURL: "https://example.com/canonical",
		Language:     "ko",
		Published:    "2026-07-11",
		StatusCode:   200,
		FetchMs:      120,
		Provider:     "stealth",
		WordCount:    42,
		Truncated:    true,
		Signals:      []string{"article", "json_ld"},
	}
	got := formatFetchResult(meta, "body")
	for _, want := range []string{
		"<metadata>", "Title: Title", "Description: Description", "Author: Author", "Site: Site",
		"URL: https://example.com/original", "FinalURL: https://example.com/final",
		"Canonical: https://example.com/canonical", "Language: ko", "Published: 2026-07-11",
		"StatusCode: 200", "FetchMs: 120", "Provider: stealth", "WordCount: 42", "Truncated: true",
		"Signals: article, json_ld",
		"<content>\nbody\n</content>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formatted result missing %q:\n%s", want, got)
		}
	}

	minimal := formatFetchResult(webFetchMeta{URL: "u", FinalURL: "u", CanonicalURL: "u", StatusCode: 204}, "")
	for _, absent := range []string{"Title:", "Description:", "FinalURL:", "Canonical:", "FetchMs:", "Provider:", "WordCount:", "Truncated:", "Signals:"} {
		if strings.Contains(minimal, absent) {
			t.Errorf("minimal metadata unexpectedly contains %q: %s", absent, minimal)
		}
	}
}

func TestFetchErrorFormattingAndTruncationBoundaries(t *testing.T) {
	full := formatFetchError(webFetchErr{
		Code: "http_429", Message: "rate limited", URL: "https://example.com", Retryable: true, Hint: "wait",
	})
	for _, want := range []string{"Code: http_429", "Message: rate limited", "URL: https://example.com", "Retryable: true", "Hint: wait"} {
		if !strings.Contains(full, want) {
			t.Errorf("error output missing %q: %s", want, full)
		}
	}
	minimal := formatFetchError(webFetchErr{Code: "unknown", Message: "x"})
	if strings.Contains(minimal, "URL:") || strings.Contains(minimal, "Hint:") {
		t.Fatalf("minimal error emitted optional fields: %s", minimal)
	}

	short := "short"
	if got := applyTruncation(short, 10); got != short {
		t.Fatalf("short truncation = %q", got)
	}
	plain := strings.Repeat("x", 100)
	if got := applyTruncation(plain, 20); !strings.HasSuffix(got, "\n[...truncated]") || !strings.HasPrefix(got, strings.Repeat("x", 20)) {
		t.Fatalf("plain truncation = %q", got)
	}
	formatted := formatFetchResult(webFetchMeta{URL: "u", StatusCode: 200}, "# First\n\n"+strings.Repeat("a", 100)+"\n\n# Second\n"+strings.Repeat("b", 100))
	truncated := applyTruncation(formatted, 150)
	if !strings.Contains(truncated, "<metadata>") || !strings.Contains(truncated, "</content>") || !strings.Contains(truncated, "chars remaining") {
		t.Fatalf("structured truncation = %q", truncated)
	}
}

func TestTruncateAtSectionPriorityAndHelpers(t *testing.T) {
	if got, truncated := truncateAtSection("short", 10); truncated || got != "short" {
		t.Fatalf("short truncate = (%q,%v)", got, truncated)
	}
	withHeading := strings.Repeat("a", 60) + "\n# Next\n" + strings.Repeat("b", 80)
	got, truncated := truncateAtSection(withHeading, 100)
	if !truncated || strings.Contains(got, "# Next") || len(got) >= 100 {
		t.Fatalf("heading truncation = len %d %q", len(got), got)
	}
	paragraph := strings.Repeat("a", 55) + "\n\n" + strings.Repeat("b", 60)
	got, truncated = truncateAtSection(paragraph, 90)
	if !truncated || len(got) != 55 {
		t.Fatalf("paragraph truncation = len %d %q", len(got), got)
	}
	hard := strings.Repeat("z", 120)
	got, truncated = truncateAtSection(hard, 80)
	if !truncated || len(got) != 80 {
		t.Fatalf("hard truncation = len %d", len(got))
	}
	if estimateWordCount(" one\t둘\nthree  ") != 3 || estimateWordCount("   ") != 0 {
		t.Fatal("word count whitespace semantics changed")
	}
	values := []string{"a"}
	if got := appendUnique(values, "a"); len(got) != 1 {
		t.Fatalf("append duplicate = %#v", got)
	}
	if got := appendUnique(values, "b"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("append new = %#v", got)
	}
}

func TestSearchResultUnmarshalProviderShapesAndMalformedInput(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want searchResult
	}{
		{"brave", `{"title":"A","url":"https://a","description":"desc"}`, searchResult{Title: "A", URL: "https://a", Description: "desc"}},
		{"serper", `{"title":"B","link":"https://b","snippet":"snippet"}`, searchResult{Title: "B", URL: "https://b", Description: "snippet"}},
		{"primary wins", `{"title":"C","url":"u","link":"l","description":"d","snippet":"s"}`, searchResult{Title: "C", URL: "u", Description: "d"}},
		{"empty", `{}`, searchResult{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got searchResult
			if err := json.Unmarshal([]byte(tc.raw), &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got != tc.want {
				t.Fatalf("result = %#v, want %#v", got, tc.want)
			}
		})
	}
	var malformed searchResult
	if err := json.Unmarshal([]byte(`{"title":`), &malformed); err == nil {
		t.Fatal("malformed search result succeeded")
	}
}

func TestSearchFormattingAnswerPriorityAndEmptyFallback(t *testing.T) {
	results := []searchResult{
		{Title: "One", URL: "https://one", Description: "First"},
		{Title: "Two", URL: "https://two", Description: "Second"},
	}
	formatted := formatSearchResults(results)
	if !strings.Contains(formatted, "1. **One**") || !strings.Contains(formatted, "2. **Two**") {
		t.Fatalf("search formatting = %q", formatted)
	}
	if got := formatSearchResults(nil); got != "No results found." {
		t.Fatalf("empty search formatting = %q", got)
	}

	answer := serperAnswerBox{Title: "ignored", Answer: "direct", Snippet: "fallback", Link: "https://source"}
	if got := pickAnswer(answer); got != "direct" {
		t.Fatalf("answer priority = %q", got)
	}
	answer.Answer = ""
	if got := pickAnswer(answer); got != "fallback" {
		t.Fatalf("snippet fallback = %q", got)
	}
	answer.Snippet = ""
	if got := pickAnswer(answer); got != "" {
		t.Fatalf("empty answer = %q", got)
	}
	withAnswer := formatSerperResults(nil, serperAnswerBox{Answer: "42", Link: "https://source"})
	if !strings.Contains(withAnswer, "**Answer:** 42") || !strings.Contains(withAnswer, "Source: https://source") {
		t.Fatalf("answer-only formatting = %q", withAnswer)
	}
	if got := formatSerperResults(nil, serperAnswerBox{}); got != "No results found." {
		t.Fatalf("empty serper formatting = %q", got)
	}
}

func TestScrapeContentParsesMetadataAndBinaryURLs(t *testing.T) {
	if got := pickScrapeContent(&serperScrapeResponse{Markdown: "  # heading  ", Text: "plain"}); got != "  # heading  " {
		t.Fatalf("markdown preference = %q", got)
	}
	if got := pickScrapeContent(&serperScrapeResponse{Markdown: "   ", Text: "plain"}); got != "plain" {
		t.Fatalf("text fallback = %q", got)
	}
	meta := &webFetchMeta{}
	populateScrapeMetadata(meta, map[string]string{
		"title":                  "  Main title ",
		"og:title":               "Fallback title",
		"og:description":         "Description",
		"application-name":       "App",
		"og:locale":              "ko_KR",
		"article:author":         "Author",
		"article:published_time": "2026-01-02",
		"canonical":              "https://canonical",
		"og:type":                "article",
	})
	if meta.Title != "Main title" || meta.Description != "Description" || meta.SiteName != "App" ||
		meta.Language != "ko_KR" || meta.Author != "Author" || meta.Published != "2026-01-02" ||
		meta.CanonicalURL != "https://canonical" || meta.OGType != "article" {
		t.Fatalf("scrape metadata = %#v", meta)
	}
	unchanged := *meta
	populateScrapeMetadata(meta, nil)
	if !reflect.DeepEqual(*meta, unchanged) {
		t.Fatalf("empty metadata mutated result: %#v", meta)
	}

	binary := []string{
		"https://x/report.PDF", "https://x/a.docx?download=1", "https://x/image.jpeg#hero",
		"https://x/archive.tar", "https://x/audio.flac", "https://x/movie.webm",
	}
	for _, u := range binary {
		if !looksLikeBinaryURL(u) {
			t.Errorf("binary URL not detected: %s", u)
		}
	}
	for _, u := range []string{"https://x/article", "https://x/pdf-viewer", "https://x/file.pdf/html", ""} {
		if looksLikeBinaryURL(u) {
			t.Errorf("HTML-like URL classified binary: %s", u)
		}
	}
}

func TestSearchAPIKeyFallbackToFirstEnv(t *testing.T) {
	t.Setenv("BRAVE_SEARCH_API_KEY", "search-key")
	t.Setenv("BRAVE_API_KEY", "legacy-key")
	t.Setenv("SERPER_API_KEY", "serper-key")
	if braveAPIKey() != "search-key" || serperAPIKey() != "serper-key" {
		t.Fatalf("primary API keys = brave %q serper %q", braveAPIKey(), serperAPIKey())
	}
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	if braveAPIKey() != "legacy-key" {
		t.Fatalf("legacy Brave fallback = %q", braveAPIKey())
	}
	t.Setenv("FIRST_EMPTY", "")
	t.Setenv("SECOND_VALUE", "second")
	t.Setenv("THIRD_VALUE", "third")
	if got := firstEnv("FIRST_EMPTY", "SECOND_VALUE", "THIRD_VALUE"); got != "second" {
		t.Fatalf("firstEnv = %q", got)
	}
	if got := firstEnv("MISSING_ONE", "MISSING_TWO"); got != "" {
		t.Fatalf("firstEnv missing = %q", got)
	}
}

func TestLocalAIExtractorLoadsEnvAndCachesAvailability(t *testing.T) {
	t.Setenv("LOCAL_AI_BASE_URL", "http://local-primary")
	t.Setenv("SGLANG_BASE_URL", "http://secondary")
	t.Setenv("LOCAL_AI_API_KEY", "primary-key")
	t.Setenv("SGLANG_API_KEY", "secondary-key")
	t.Setenv("LOCAL_AI_MODEL", "primary-model")
	t.Setenv("SGLANG_MODEL", "secondary-model")
	configured := NewLocalAIExtractor()
	if configured.baseURL != "http://local-primary" || configured.apiKey != "primary-key" || configured.model != "primary-model" || configured.client == nil {
		t.Fatalf("configured extractor = %#v", configured)
	}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/models" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("probe = %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	extractor := &LocalAIExtractor{client: server.Client(), baseURL: server.URL, apiKey: "test-key", model: "test"}
	// Two calls on purpose: the second must be served from the cached probe.
	first, second := extractor.available(), extractor.available()
	if !first || !second {
		t.Fatal("healthy local AI reported unavailable")
	}
	if requests.Load() != 1 {
		t.Fatalf("available state did not cache probe: requests=%d", requests.Load())
	}

	unavailable := &LocalAIExtractor{client: server.Client(), baseURL: "://bad", apiKey: "x"}
	if unavailable.available() || unavailable.state != localAIUnavailable {
		t.Fatalf("malformed extractor state=%d", unavailable.state)
	}
	before := unavailable.probeAt
	if unavailable.available() || !unavailable.probeAt.Equal(before) {
		t.Fatal("recent unavailable state unexpectedly re-probed")
	}
}

func TestLocalAIExtractParsesSmallContentAndResponseVariants(t *testing.T) {
	extractor := &LocalAIExtractor{}
	small := "<html><body><h1>Hello</h1><p>short body</p></body></html>"
	got, err := extractor.extract(context.Background(), small, "https://example.com", "en")
	if err != nil || !strings.Contains(got, "Hello") || !strings.Contains(got, "short body") {
		t.Fatalf("small extraction = %q, %v", got, err)
	}

	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"success strips thinking", 200, `{"choices":[{"message":{"content":"<think>secret</think>\n extracted "}}]}`, "extracted"},
		{"no choices", 200, `{"choices":[]}`, "no choices"},
		{"malformed", 200, `{`, "decode local AI response"},
		{"http error", 503, "down", "localai HTTP 503"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			e := &LocalAIExtractor{client: server.Client(), baseURL: server.URL, apiKey: "key", model: "model"}
			large := "<main><p>" + strings.Repeat("useful content ", 220) + "</p></main>"
			got, err := e.extract(context.Background(), large, "https://example.com", "en")
			if tc.status == 200 && tc.want == "extracted" {
				if err != nil || got != tc.want {
					t.Fatalf("success = %q, %v", got, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestSharedClientUsesPooledTransportAndIndependentTimeouts(t *testing.T) {
	a := SharedClient(time.Second)
	b := SharedClient(2 * time.Second)
	if a == b || a.Transport != b.Transport || a.Transport != sharedTransport {
		t.Fatalf("shared client transport: a=%p b=%p transports=%p/%p", a, b, a.Transport, b.Transport)
	}
	if a.Timeout != time.Second || b.Timeout != 2*time.Second {
		t.Fatalf("client timeouts = %s, %s", a.Timeout, b.Timeout)
	}
}

func TestRetryabilityAndClassificationErrorMatrix(t *testing.T) {
	target := "https://example.com"
	cases := []struct {
		name      string
		err       error
		code      string
		retryable bool
		hintPart  string
	}{
		{"http403", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 403, Message: "forbidden"}, "http_403", false, "blocked"},
		{"http429", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 429, Message: "limited"}, "http_429", false, "Rate limited"},
		{"http503", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 503, Message: "down"}, "http_503", true, "Server error"},
		{"too large", &media.MediaFetchError{Code: media.ErrMaxBytes, Message: "large"}, "content_too_large", false, "narrower URL"},
		{"ssrf", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "SSRF blocked"}, "ssrf_blocked", false, "public URL"},
		{"dns", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "no such host"}, "dns_failure", false, "typos"},
		{"redirect", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "too many redirects"}, "redirect_loop", false, ""},
		{"tls", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "certificate expired"}, "tls_error", false, ""},
		{"refused", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "connection refused"}, "connection_refused", true, "may be down"},
		{"reset", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "connection reset"}, "connection_reset", true, ""},
		{"generic fetch", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "network"}, "fetch_failed", true, ""},
		{"deadline", context.DeadlineExceeded, "timeout", true, "Retry"},
		{"cancel", context.Canceled, "canceled", false, ""},
		{"unknown", errors.New("boom"), "unknown", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyFetchError(tc.err, target)
			if got.Code != tc.code || got.Retryable != tc.retryable || got.URL != target {
				t.Fatalf("classification = %#v", got)
			}
			if tc.hintPart != "" && !strings.Contains(got.Hint, tc.hintPart) {
				t.Errorf("hint %q missing %q", got.Hint, tc.hintPart)
			}
		})
	}
	if !isRetryableError(context.DeadlineExceeded) || isRetryableError(context.Canceled) {
		t.Fatal("context retryability incorrect")
	}
	if !isRetryableError(&media.MediaFetchError{Code: media.ErrHTTPError, Status: 500}) ||
		isRetryableError(&media.MediaFetchError{Code: media.ErrHTTPError, Status: 404}) ||
		!isRetryableError(&media.MediaFetchError{Code: media.ErrFetchFailed}) {
		t.Fatal("media retryability incorrect")
	}
}

func TestTypedSearchFormattingRepresentativePayloads(t *testing.T) {
	rawNews := map[string]json.RawMessage{
		"knowledgeGraph": json.RawMessage(`{"title":"Topic","description":"Overview"}`),
		"news":           json.RawMessage(`[{"title":"Headline","link":"https://news","snippet":"Details","date":"1h","source":"Wire"}]`),
	}
	news := formatSerperTypedResults("news", rawNews)
	for _, want := range []string{"**Topic**: Overview", "1. **Headline**", "Source: Wire | 1h", "Details", "https://news"} {
		if !strings.Contains(news, want) {
			t.Errorf("news output missing %q: %s", want, news)
		}
	}
	rawScholar := map[string]json.RawMessage{
		"organic": json.RawMessage(`[{"title":"Paper","link":"https://paper","snippet":"Abstract","publication":"Journal","authors":"A, B","year":"2025","citedBy":{"total":9,"link":"https://cites"}}]`),
	}
	scholar := formatSerperTypedResults("scholar", rawScholar)
	for _, want := range []string{"**Paper**", "A, B", "Journal", "2025", "Cited by: 9", "https://paper"} {
		if !strings.Contains(scholar, want) {
			t.Errorf("scholar output missing %q: %s", want, scholar)
		}
	}
	rawAutocomplete := map[string]json.RawMessage{
		"1": json.RawMessage(`["alpha","beta"]`),
	}
	autocomplete := formatSerperTypedResults("autocomplete", rawAutocomplete)
	if !strings.Contains(autocomplete, "alpha") || !strings.Contains(autocomplete, "beta") {
		t.Fatalf("autocomplete output = %q", autocomplete)
	}
	if got := formatSerperTypedResults("news", map[string]json.RawMessage{}); got != "No results found." {
		t.Fatalf("empty typed output = %q", got)
	}
}

func TestBinaryExtensionDetectionIgnoresCaseAndQuery(t *testing.T) {
	urls := []string{
		"a.pdf", "a.doc", "a.docx", "a.xls", "a.xlsx", "a.ppt", "a.pptx",
		"a.zip", "a.tar", "a.gz", "a.7z", "a.mp3", "a.wav", "a.ogg", "a.flac",
		"a.mp4", "a.mov", "a.avi", "a.webm", "a.jpg", "a.jpeg", "a.png", "a.gif", "a.webp", "a.svg",
	}
	sort.Strings(urls)
	for _, u := range urls {
		upper := strings.ToUpper(u) + "?signed=true#fragment"
		if !looksLikeBinaryURL(upper) {
			t.Errorf("extension contract missing for %q", upper)
		}
	}
}
