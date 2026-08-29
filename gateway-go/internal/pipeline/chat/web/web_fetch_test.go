package web

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

// --- HTML noise stripping tests ---

func TestStripNoiseElementsPreservesArticleContent(t *testing.T) {
	html := `<html><body>
<nav><ul><li>Home</li><li>About</li></ul></nav>
<header><h1>Site Header</h1></header>
<article><h1>Main Content</h1><p>This is the article body.</p></article>
<aside><h3>Sidebar</h3><p>Related links</p></aside>
<footer><p>Copyright 2024</p></footer>
</body></html>`

	result := StripNoiseElements(html)

	// Article content should survive.
	if !strings.Contains(result, "Main Content") {
		t.Error("main content was stripped")
	}
	if !strings.Contains(result, "article body") {
		t.Error("article body was stripped")
	}

	// Noise elements should be removed.
	if strings.Contains(result, "Home") || strings.Contains(result, "About") {
		t.Error("nav content should be stripped")
	}
	if strings.Contains(result, "Site Header") {
		t.Error("header content should be stripped")
	}
	if strings.Contains(result, "Sidebar") {
		t.Error("aside content should be stripped")
	}
	if strings.Contains(result, "Copyright") {
		t.Error("footer content should be stripped")
	}
}

func TestStripNoiseElementsPreservesContentAroundCookieBanner(t *testing.T) {
	html := `<p>Before</p>
<div class="cookie-consent-banner"><p>We use cookies</p><button>Accept</button></div>
<p>After</p>`
	result := StripNoiseElements(html)
	if strings.Contains(result, "We use cookies") {
		t.Error("cookie banner should be stripped")
	}
	if !strings.Contains(result, "Before") || !strings.Contains(result, "After") {
		t.Error("surrounding content should survive")
	}
}

func TestStripTagBlockPreservesPrefixedTagContent(t *testing.T) {
	// <navigate> should NOT be stripped when stripping <nav>.
	html := `<navigate>keep this</navigate><nav>remove this</nav>`
	result := stripTagBlock(html, "nav")
	if !strings.Contains(result, "keep this") {
		t.Error("<navigate> content should survive")
	}
	if strings.Contains(result, "remove this") {
		t.Error("<nav> content should be stripped")
	}
}

// --- Metadata extraction tests ---

func TestExtractHTMLMetaParsesOpenGraphAndMetaTags(t *testing.T) {
	html := `<html lang="ko">
<head>
<title>테스트 페이지</title>
<meta property="og:title" content="OG Title">
<meta property="og:description" content="OG Description">
<meta name="description" content="Meta Description">
<meta name="author" content="Kim Author">
<meta property="og:site_name" content="Test Site">
<meta property="og:type" content="article">
<link rel="canonical" href="https://example.com/canonical">
<meta property="article:published_time" content="2024-01-15T09:00:00Z">
</head>
<body>Content</body>
</html>`

	meta := &webFetchMeta{}
	extractHTMLMeta(html, meta)

	checks := map[string]struct{ got, want string }{
		"title":     {meta.Title, "OG Title"},
		"desc":      {meta.Description, "OG Description"},
		"canonical": {meta.CanonicalURL, "https://example.com/canonical"},
		"language":  {meta.Language, "ko"},
		"published": {meta.Published, "2024-01-15T09:00:00Z"},
		"author":    {meta.Author, "Kim Author"},
		"siteName":  {meta.SiteName, "Test Site"},
		"ogType":    {meta.OGType, "article"},
	}
	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", name, c.got, c.want)
		}
	}
}

func TestExtractHTMLMetaParsesReversedAttributeOrder(t *testing.T) {
	html := `<html><head>
<meta content="Rev Title" property="og:title">
<meta content="Rev Desc" name="description">
<link href="https://example.com/rev" rel="canonical">
<meta content="Author Rev" name="author">
</head></html>`
	meta := &webFetchMeta{}
	extractHTMLMeta(html, meta)
	if meta.Title != "Rev Title" {
		t.Errorf("title = %q, want %q", meta.Title, "Rev Title")
	}
	if meta.Author != "Author Rev" {
		t.Errorf("author = %q, want %q", meta.Author, "Author Rev")
	}
}

func TestExtractJSONLDParsesArticleMetadata(t *testing.T) {
	html := `<head>
<script type="application/ld+json">
{
  "@type": "Article",
  "headline": "JSON-LD Title",
  "description": "JSON-LD Desc",
  "datePublished": "2024-06-01",
  "author": {"@type": "Person", "name": "LD Author"},
  "publisher": {"@type": "Organization", "name": "LD Publisher"},
  "wordCount": 1500
}
</script>
</head>`
	meta := &webFetchMeta{}
	extractJSONLD(html, meta)

	if meta.Title != "JSON-LD Title" {
		t.Errorf("title = %q, want %q", meta.Title, "JSON-LD Title")
	}
	if meta.Author != "LD Author" {
		t.Errorf("author = %q, want %q", meta.Author, "LD Author")
	}
	if meta.SiteName != "LD Publisher" {
		t.Errorf("siteName = %q, want %q", meta.SiteName, "LD Publisher")
	}
	if meta.WordCount != 1500 {
		t.Errorf("wordCount = %d, want %d", meta.WordCount, 1500)
	}
	if meta.OGType != "ld:Article" {
		t.Errorf("ogType = %q, want %q", meta.OGType, "ld:Article")
	}
}

// --- Signal detection tests ---

func TestDetectSignalsReturnsExpectedSignalForPageType(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{"login wall", `<div class="login-wall">Sign in</div>`, "login_wall"},
		{"paywall", `<div class="paywall">Subscribe</div>`, "login_wall"},
		{"soft paywall", `<p>You have 2 free articles remaining</p>`, "soft_paywall"},
		{"cookie consent", `<div class="cookie-consent">Cookies</div>`, "cookie_consent"},
		{"cloudflare", `<h1>Blocked by Cloudflare</h1>`, "bot_blocked"},
		{"captcha", `<div class="g-recaptcha">Verify</div>`, "captcha_required"},
		{"age gate", `<div class="age-gate">Verify your age</div>`, "age_gate"},
		{"clean page", `<html><body><h1>Hello</h1><p>Content here</p></body></html>`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signals := detectSignals(tt.html)
			if tt.want != "" {
				found := false
				for _, s := range signals {
					if s == tt.want {
						found = true
					}
				}
				if !found {
					t.Errorf("got %v, want signal %q", signals, tt.want)
				}
			}
			if tt.want == "" && len(signals) > 0 {
				t.Errorf("got %v, want no signals", signals)
			}
		})
	}
}

func TestDetectSignalsReturnsJSRequiredForSPAShell(t *testing.T) {
	// React app with empty body — should detect js_required.
	html := `<html><body><div id="root"></div>
<script src="/static/js/main.chunk.js"></script></body></html>`
	signals := detectSignals(html)
	found := false
	for _, s := range signals {
		if s == "js_required" {
			found = true
		}
	}
	if !found {
		t.Errorf("got %v, want js_required for SPA shell", signals)
	}
}

func TestDetectSignalsReturnsRedirectToLoginForMetaRefresh(t *testing.T) {
	html := `<html><head><meta http-equiv="refresh" content="0;url=/login"></head></html>`
	signals := detectSignals(html)
	found := false
	for _, s := range signals {
		if s == "redirect_to_login" {
			found = true
		}
	}
	if !found {
		t.Errorf("got %v, want redirect_to_login", signals)
	}
}

// --- Error classification tests ---

func TestClassifyFetchError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  string
		wantRetry bool
	}{
		{"404", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 404, Message: "not found"}, "http_404", false},
		{"503", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 503, Message: "unavailable"}, "http_503", true},
		{"SSRF", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "SSRF: blocked"}, "ssrf_blocked", false},
		{"DNS", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "no such host"}, "dns_failure", false},
		{"TLS", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "certificate verify failed"}, "tls_error", false},
		{"reset", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "connection reset"}, "connection_reset", true},
		{"refused", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "connection refused"}, "connection_refused", true},
		{"too large", &media.MediaFetchError{Code: media.ErrMaxBytes, Message: "too big"}, "content_too_large", false},
		{"timeout", context.DeadlineExceeded, "timeout", true},
		{"canceled", context.Canceled, "canceled", false},
		{"redirect", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "too many redirects (5)"}, "redirect_loop", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyFetchError(tt.err, "https://example.com")
			if result.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", result.Code, tt.wantCode)
			}
			if result.Retryable != tt.wantRetry {
				t.Errorf("retryable = %v, want %v", result.Retryable, tt.wantRetry)
			}
		})
	}
}

// --- Output formatting tests ---

func TestFormatFetchResult(t *testing.T) {
	meta := webFetchMeta{
		Title:        "Test Page",
		Description:  "A test page description",
		Author:       "Test Author",
		SiteName:     "Test Site",
		URL:          "https://example.com",
		FinalURL:     "https://example.com/final",
		Language:     "en",
		OGType:       "article",
		ContentType:  "text/html",
		StatusCode:   200,
		FetchMs:      150,
		Provider:     "stealth",
		OrigChars:    10000,
		ExtractChars: 5000,
		Retention:    "50.0%",
		WordCount:    800,
		Signals:      []string{"cookie_consent"},
	}
	content := "# Hello\n\nThis is the content."
	result := formatFetchResult(meta, content)

	checks := []string{
		"<metadata>", "</metadata>", "<content>", "</content>",
		"Title: Test Page",
		"Description: A test page description",
		"Author: Test Author",
		"Site: Test Site",
		"FinalURL: https://example.com/final",
		"Language: en",
		"StatusCode: 200",
		"FetchMs: 150",
		"Provider: stealth",
		"WordCount: 800",
		"Signals: cookie_consent",
		"# Hello",
	}
	// Verify removed fields are no longer present.
	removed := []string{"Type: article", "FetchTime:", "ContentChars:", "ContentType:"}
	for _, r := range removed {
		if strings.Contains(result, r) {
			t.Errorf("should not contain removed field %q", r)
		}
	}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("missing %q in output", check)
		}
	}
}

func TestFormatFetchResult_NoRedirect(t *testing.T) {
	meta := webFetchMeta{URL: "https://x.com", FinalURL: "https://x.com", ContentType: "text/plain", StatusCode: 200, Retention: "100.0%"}
	result := formatFetchResult(meta, "hello")
	if strings.Contains(result, "FinalURL:") {
		t.Error("FinalURL should be omitted when same as URL")
	}
}

func TestFormatFetchError(t *testing.T) {
	e := webFetchErr{Code: "http_404", Message: "not found", URL: "https://x.com", Retryable: false}
	result := formatFetchError(e)
	if !strings.Contains(result, "<error>") || !strings.Contains(result, "Code: http_404") {
		t.Errorf("unexpected error format: %s", result)
	}
}

func TestClassifyFetchError_Hints(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantHint string
	}{
		{"403", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 403, Message: "forbidden"}, "web(query=...)"},
		{"429", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 429, Message: "too many requests"}, "Rate limited"},
		{"500", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 500, Message: "internal error"}, "Server error"},
		{"SSRF", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "SSRF: blocked"}, "public URL"},
		{"DNS", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "no such host"}, "typos"},
		{"timeout", context.DeadlineExceeded, "Retry"},
		{"content_too_large", &media.MediaFetchError{Code: media.ErrMaxBytes, Message: "too big"}, "narrower URL"},
		{"connection_refused", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "connection refused"}, "may be down"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyFetchError(tt.err, "https://example.com")
			if !strings.Contains(result.Hint, tt.wantHint) {
				t.Errorf("hint = %q, want substring %q", result.Hint, tt.wantHint)
			}
		})
	}
}

// --- Truncation tests ---

func TestApplyTruncation_PreservesMetadata(t *testing.T) {
	meta := webFetchMeta{URL: "https://x.com", ContentType: "text/html", StatusCode: 200, Retention: "100.0%"}
	content := strings.Repeat("word ", 200)
	result := formatFetchResult(meta, content)
	truncated := applyTruncation(result, 500)

	if !strings.Contains(truncated, "<metadata>") {
		t.Error("metadata should be preserved")
	}
	if !strings.Contains(truncated, "truncated") {
		t.Error("should have truncation marker")
	}
	if !strings.Contains(truncated, "chars remaining") {
		t.Error("should show remaining chars count")
	}
}

func TestTruncateAtSection(t *testing.T) {
	content := "# Section 1\n\nParagraph one.\n\n# Section 2\n\nParagraph two.\n\n# Section 3\n\nParagraph three."
	truncated, wasTruncated := truncateAtSection(content, 50)
	if !wasTruncated {
		t.Error("should be truncated")
	}
	// Should cut at a heading boundary.
	if strings.Contains(truncated, "Section 3") {
		t.Error("should not include section 3")
	}
	if !strings.Contains(truncated, "Section 1") {
		t.Error("should include section 1")
	}
}

// --- Charset normalization tests ---

func TestNormalizeCharset_UTF8(t *testing.T) {
	data := []byte("Hello 세계")
	result := normalizeCharset(data, "text/html; charset=utf-8")
	if result != "Hello 세계" {
		t.Errorf("got %q, want %q", result, "Hello 세계")
	}
}

func TestNormalizeCharset_Latin1(t *testing.T) {
	// Latin-1 encoded: café (é = 0xe9).
	data := []byte{'c', 'a', 'f', 0xe9}
	result := normalizeCharset(data, "text/html; charset=iso-8859-1")
	if result != "café" {
		t.Errorf("got %q, want %q", result, "café")
	}
}

// --- JSON processing tests ---

func TestProcessJSONFormatsRawJSON(t *testing.T) {
	raw := `{"name":"test","value":42}`
	result := processJSON(raw)
	if !strings.Contains(result, "  \"name\": \"test\"") {
		t.Errorf("expected pretty-printed JSON, got: %s", result)
	}
}

// --- Helper tests ---

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"5xx", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 500}, true},
		{"404", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 404}, false},
		{"fetch_failed", &media.MediaFetchError{Code: media.ErrFetchFailed}, true},
		{"max_bytes", &media.MediaFetchError{Code: media.ErrMaxBytes}, false},
		{"deadline", context.DeadlineExceeded, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableError(tt.err); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectFetchURLsFiltersMediaDupesAndJavascript(t *testing.T) {
	in := []string{
		"",
		"javascript:void(0)",
		"https://a.example/page",
		"https://a.example/other", // same host — skip
		"https://cdn.example/img.png",
		"https://docs.example/report.pdf", // keep
		"https://b.example/article",
		"not-a-url",
	}
	got := selectFetchURLs(in, 3)
	want := []string{
		"https://a.example/page",
		"https://docs.example/report.pdf",
		"https://b.example/article",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSelectFetchURLsRespectsLimit(t *testing.T) {
	in := []string{
		"https://a.example/1",
		"https://b.example/2",
		"https://c.example/3",
	}
	got := selectFetchURLs(in, 2)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %v", len(got), got)
	}
	if selectFetchURLs(in, 0) != nil {
		t.Fatal("limit 0 should return nil")
	}
}

func TestRankFetchCandidatesPrefersAnswerBoxAndSkipsSocial(t *testing.T) {
	organic := []searchResult{
		{URL: "https://www.facebook.com/post/1"},
		{URL: "https://good.example/article"},
		{URL: "https://pinterest.com/pin/9"},
		{URL: "https://news.example/story"},
		{URL: "https://www.linkedin.com/pulse/x"},
		{URL: "https://www.youtube.com/@SomeChannel"},
	}
	got, _ := rankFetchCandidates("", "https://answer.example/box", "", organic, 4)
	want := []string{
		"https://answer.example/box",
		"https://good.example/article",
		"https://news.example/story",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestRankFetchCandidatesQueryOverlapAndDiversity(t *testing.T) {
	organic := []searchResult{
		{Title: "Unrelated shop", URL: "https://a.example/cart", Description: "buy now"},
		{Title: "Deneb star facts", URL: "https://wiki.example/deneb", Description: "Deneb is a star"},
		{Title: "Also deneb", URL: "https://blog.wiki.example/post", Description: "more deneb"},
		{Title: "Other topic", URL: "https://news.example/x", Description: "politics"},
	}
	got, _ := rankFetchCandidates("Deneb star", "", "", organic, 3)
	if len(got) < 2 || got[0] != "https://wiki.example/deneb" {
		t.Fatalf("expected deneb wiki first, got %v", got)
	}
	// blog.wiki.example shares eTLD+1 with wiki.example under naive registrableDomain
	// (example) — actually wiki.example vs blog.wiki.example → example vs example?
	// wiki.example → example (last 2: wiki.example → wait parts=["wiki","example"] → wiki.example)
	// blog.wiki.example → parts=["blog","wiki","example"] → wiki.example
	// so second deneb URL should be demoted/skipped by eTLD diversity in filter.
	for _, u := range got {
		if strings.Contains(u, "/cart") {
			t.Fatalf("cart path should be filtered: %v", got)
		}
		if strings.Contains(u, "blog.wiki.example") {
			t.Fatalf("same eTLD+1 as wiki.example should be filtered: %v", got)
		}
	}
}

func TestAssessFetchResultStructured(t *testing.T) {
	errA := assessFetchResult(formatFetchError(webFetchErr{Code: "http_403", Message: "no"}), nil)
	if !errA.HasError || errA.Usable {
		t.Fatalf("error envelope: %+v", errA)
	}
	thin := assessFetchResult("<metadata>\nSignals: js_required\n</metadata>\n<content>\nshort\n</content>", nil)
	if !thin.Thin || thin.Usable || thin.BodyChars == 0 {
		t.Fatalf("thin: %+v", thin)
	}
	okBody := "<metadata>\nSignals: serper_scrape\n</metadata>\n<content>\n" + strings.Repeat("body ", 100) + "\n</content>"
	ok := assessFetchResult(okBody, nil)
	if !ok.Usable || ok.HasError {
		t.Fatalf("ok: %+v", ok)
	}
}

func TestWebFetchURLDetailedKeepsFastSerperScrapePath(t *testing.T) {
	t.Setenv("SERPER_API_KEY", "serper-test")

	origScrape := serperScrapeFn
	origFetch := fetchWithRetryFn
	t.Cleanup(func() {
		serperScrapeFn = origScrape
		fetchWithRetryFn = origFetch
	})

	serperScrapeFn = func(context.Context, string, string) (*serperScrapeResponse, error) {
		return &serperScrapeResponse{Markdown: strings.Repeat("serper body ", 80)}, nil
	}
	fetchWithRetryFn = func(context.Context, string, int64) (*media.FetchResult, error) {
		t.Fatal("origin fetch should not run when Serper scrape succeeds before grace")
		return nil, nil
	}

	out, err := webFetchURLDetailed(context.Background(), NewFetchCache(), nil, nil, "https://fast-serper.example/article", 20000, "")
	if err != nil {
		t.Fatalf("webFetchURLDetailed: %v", err)
	}
	if !strings.Contains(out.Content, "Provider: serper") || !strings.Contains(out.Content, "serper body") {
		t.Fatalf("unexpected Serper output:\n%s", out.Content)
	}
}

func TestWebFetchURLDetailedStartsOriginAfterSlowSerperGrace(t *testing.T) {
	t.Setenv("SERPER_API_KEY", "serper-test")

	origScrape := serperScrapeFn
	origFetch := fetchWithRetryFn
	origGrace := serperScrapeGrace
	t.Cleanup(func() {
		serperScrapeFn = origScrape
		fetchWithRetryFn = origFetch
		serperScrapeGrace = origGrace
	})
	serperScrapeGrace = 10 * time.Millisecond

	serperStarted := make(chan struct{})
	rawStarted := make(chan struct{})
	serperScrapeFn = func(ctx context.Context, _, _ string) (*serperScrapeResponse, error) {
		close(serperStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	fetchWithRetryFn = func(_ context.Context, targetURL string, _ int64) (*media.FetchResult, error) {
		close(rawStarted)
		body := []byte(strings.Repeat("fast origin body ", 80))
		return &media.FetchResult{
			Data:        body,
			ContentType: "text/plain; charset=utf-8",
			Size:        len(body),
			FinalURL:    targetURL,
			StatusCode:  200,
		}, nil
	}

	start := time.Now()
	out, err := webFetchURLDetailed(context.Background(), NewFetchCache(), nil, nil, "https://slow-serper.example/article", 20000, "")
	if err != nil {
		t.Fatalf("webFetchURLDetailed: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 250*time.Millisecond {
		t.Fatalf("origin fetch waited too long for slow Serper scrape: %s", elapsed)
	}
	if !strings.Contains(out.Content, "Provider: stealth") || !strings.Contains(out.Content, "fast origin body") {
		t.Fatalf("unexpected origin output:\n%s", out.Content)
	}
	select {
	case <-serperStarted:
	default:
		t.Fatal("Serper scrape did not start")
	}
	select {
	case <-rawStarted:
	default:
		t.Fatal("origin fetch did not start")
	}
}

func TestWebFetchURLDetailedWaitsForSerperWhenOriginIsTiny(t *testing.T) {
	t.Setenv("SERPER_API_KEY", "serper-test")

	origScrape := serperScrapeFn
	origFetch := fetchWithRetryFn
	origGrace := serperScrapeGrace
	t.Cleanup(func() {
		serperScrapeFn = origScrape
		fetchWithRetryFn = origFetch
		serperScrapeGrace = origGrace
	})
	serperScrapeGrace = time.Millisecond

	rawStarted := make(chan struct{})
	serperScrapeFn = func(_ context.Context, _, _ string) (*serperScrapeResponse, error) {
		<-rawStarted
		return &serperScrapeResponse{Markdown: strings.Repeat("serper recovered body ", 80)}, nil
	}
	fetchWithRetryFn = func(_ context.Context, targetURL string, _ int64) (*media.FetchResult, error) {
		close(rawStarted)
		return &media.FetchResult{
			Data:        []byte("tiny"),
			ContentType: "text/plain; charset=utf-8",
			Size:        len("tiny"),
			FinalURL:    targetURL,
			StatusCode:  200,
		}, nil
	}

	out, err := webFetchURLDetailed(context.Background(), NewFetchCache(), nil, nil, "https://tiny-origin.example/article", 20000, "")
	if err != nil {
		t.Fatalf("webFetchURLDetailed: %v", err)
	}
	if !strings.Contains(out.Content, "Provider: serper") || !strings.Contains(out.Content, "serper recovered body") {
		t.Fatalf("tiny origin should wait for Serper, got:\n%s", out.Content)
	}
}

func TestFillUsableFetchesEarlyStop(t *testing.T) {
	okBody := strings.Repeat("body ", 100)
	okEnvelope := "<metadata>\nSignals: serper_scrape\n</metadata>\n<content>\n" + okBody + "\n</content>"
	thinEnvelope := "<metadata>\nSignals: js_required\n</metadata>\n<content>\nx\n</content>"
	var mu sync.Mutex
	var calls []string
	fetch := func(_ context.Context, _ *FetchCache, _ *LocalAIExtractor, _ tooldeps.SpilloverStore, u string, _ int, _ string) (fetchOutcome, error) {
		mu.Lock()
		calls = append(calls, u)
		mu.Unlock()
		if strings.Contains(u, "err") {
			env := formatFetchError(webFetchErr{Code: "http_403", Message: "no"})
			return fetchOutcome{Content: env, Assess: fetchUsability{HasError: true}}, nil
		}
		if strings.Contains(u, "thin") {
			return fetchOutcome{Content: thinEnvelope, Assess: assessMetaBody([]string{"js_required"}, "x")}, nil
		}
		return fetchOutcome{Content: okEnvelope, Assess: assessMetaBody([]string{"serper_scrape"}, okBody)}, nil
	}
	candidates := []string{
		"https://a.example/err",
		"https://b.example/thin",
		"https://c.example/ok1",
		"https://d.example/ok2",
		"https://e.example/ok3",
	}
	got := fillUsableFetches(context.Background(), nil, nil, nil, candidates, 2, 1000, "", fetch)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %+v", len(got), got)
	}
	if got[0].url != "https://c.example/ok1" || got[1].url != "https://d.example/ok2" {
		t.Fatalf("urls=%v", got)
	}
	for _, c := range calls {
		if c == "https://e.example/ok3" {
			t.Fatalf("fetched past fill target: %v", calls)
		}
	}
	if len(calls) != 4 { // wave err+thin, then ok1, ok2
		t.Fatalf("calls=%v want 4", calls)
	}
}

func TestFillUsableFetchesHybridWaveStopsEarly(t *testing.T) {
	okBody := strings.Repeat("body ", 100)
	okEnvelope := "<metadata>\nSignals: serper_scrape\n</metadata>\n<content>\n" + okBody + "\n</content>"
	var mu sync.Mutex
	var calls []string
	fetch := func(_ context.Context, _ *FetchCache, _ *LocalAIExtractor, _ tooldeps.SpilloverStore, u string, _ int, _ string) (fetchOutcome, error) {
		mu.Lock()
		calls = append(calls, u)
		mu.Unlock()
		return fetchOutcome{Content: okEnvelope, Assess: assessMetaBody([]string{"serper_scrape"}, okBody)}, nil
	}
	candidates := []string{
		"https://a.example/ok1",
		"https://b.example/ok2",
		"https://c.example/ok3",
	}
	got := fillUsableFetches(context.Background(), nil, nil, nil, candidates, 2, 1000, "", fetch)
	if len(got) != 2 {
		t.Fatalf("len=%d: %+v", len(got), got)
	}
	if len(calls) != 2 {
		t.Fatalf("hybrid wave should only fetch 2, got %v", calls)
	}
}

func TestRankFetchCandidatesKnowledgeGraphAfterAnswer(t *testing.T) {
	organic := []searchResult{{URL: "https://organic.example/a", Title: "x"}}
	got, _ := rankFetchCandidates("", "https://answer.example/box", "https://kg.example/entity", organic, 3)
	want := []string{"https://answer.example/box", "https://kg.example/entity", "https://organic.example/a"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestAssessMetaBodyFromFetchPath(t *testing.T) {
	ok := assessMetaBody([]string{"serper_scrape"}, strings.Repeat("x", 50))
	if !ok.Usable || ok.Thin || ok.HasError {
		t.Fatalf("ok=%+v", ok)
	}
	thin := assessMetaBody([]string{"js_required", "empty_body"}, "short")
	if thin.Usable || !thin.Thin {
		t.Fatalf("thin=%+v", thin)
	}
}

func TestKeepYouTubeWatchRejectChannel(t *testing.T) {
	watch, _ := url.Parse("https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	channel, _ := url.Parse("https://www.youtube.com/@DenvFeed")
	if isDeniedYouTubeURL(watch) {
		t.Fatal("watch URL should be kept")
	}
	if !isDeniedYouTubeURL(channel) {
		t.Fatal("channel URL should be denied")
	}
}

func TestBuildBraveSearchURLLocaleForHangul(t *testing.T) {
	ko := buildBraveSearchURL("덴브 별", 5)
	if !strings.Contains(ko, "country=KR") || !strings.Contains(ko, "search_lang=ko") {
		t.Fatalf("hangul brave url=%s", ko)
	}
	en := buildBraveSearchURL("Deneb star", 3)
	if strings.Contains(en, "country=") || strings.Contains(en, "search_lang=") {
		t.Fatalf("ascii brave url should omit locale: %s", en)
	}
}

func TestSelectUsableFetchesSkipsErrorAndThin(t *testing.T) {
	candidates := []string{
		"https://a.example/err",
		"https://b.example/thin",
		"https://c.example/ok",
		"https://d.example/ok2",
	}
	thin := "<metadata>\nSignals: js_required, empty_body\n</metadata>\n<content>\nshort\n</content>"
	ok := "<metadata>\nSignals: serper_scrape\n</metadata>\n<content>\n" + strings.Repeat("body ", 100) + "\n</content>"
	results := []searchFetchOutcome{
		{content: formatFetchError(webFetchErr{Code: "http_403", Message: "forbidden"})},
		{content: thin},
		{content: ok},
		{content: ok},
	}
	got := selectUsableFetches(candidates, results, 2)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %+v", len(got), got)
	}
	if got[0].url != "https://c.example/ok" || got[1].url != "https://d.example/ok2" {
		t.Fatalf("urls=%v %v", got[0].url, got[1].url)
	}
}

func TestBuildSerperRequestAddsLocaleForHangul(t *testing.T) {
	ko := buildSerperRequest("덴브 별", 5)
	if ko.GL != "kr" || ko.HL != "ko" || ko.Num != 5 {
		t.Fatalf("hangul request = %+v", ko)
	}
	en := buildSerperRequest("Deneb star", 3)
	if en.GL != "" || en.HL != "" || en.Q != "Deneb star" {
		t.Fatalf("ascii request = %+v", en)
	}
}

func TestFetchCandidatePoolSize(t *testing.T) {
	if got := fetchCandidatePoolSize(5, 2); got != 4 {
		t.Fatalf("pool=%d want 4", got)
	}
	if got := fetchCandidatePoolSize(3, 2); got != 3 {
		t.Fatalf("pool capped by count=%d", got)
	}
	if got := fetchCandidatePoolSize(10, 4); got != 5 {
		t.Fatalf("pool capped at 5=%d", got)
	}
}

// TestOversizeHintNeverRecommendsMaxChars guards the 2026-08-29 finding: the
// hint used to say "use maxChars to limit", but maxChars feeds rawFetchBudget,
// so lowering it SHRINKS the budget the error came from. The model followed the
// advice down 40000 → 4000 → 2000 → 1000, failing harder each time.
func TestOversizeHintNeverRecommendsMaxChars(t *testing.T) {
	hint := classifyFetchError(&media.MediaFetchError{Code: media.ErrMaxBytes, Message: "too big"}, "https://example.com").Hint
	if strings.Contains(hint, "maxChars") {
		t.Fatalf("the oversize hint must not send the model back to maxChars: %q", hint)
	}
}

// TestRawFetchBudgetIsNotDerivedFromMaxCharsAlone: an article-length page has to
// fit at the default maxChars. en.wikipedia.org/wiki/Rust_(programming_language)
// is 1,019,427 bytes and was refused at a 40,000-byte budget.
func TestRawFetchBudgetIsNotDerivedFromMaxCharsAlone(t *testing.T) {
	const wikipediaRustBytes = 1_019_427
	for _, maxChars := range []int{0, 1000, 2000, 20000} {
		if got := rawFetchBudget(maxChars); got < wikipediaRustBytes {
			t.Errorf("rawFetchBudget(%d) = %d, too small for an article-length page (%d)", maxChars, got, wikipediaRustBytes)
		}
	}
	// The transport ceiling still holds — a huge maxChars must not uncap it.
	if got := rawFetchBudget(10_000_000); got != 5*1024*1024 {
		t.Errorf("rawFetchBudget ceiling = %d, want 5MiB", got)
	}
	// Lowering maxChars must never shrink the budget, which was the trap.
	if rawFetchBudget(1000) > rawFetchBudget(20000) {
		t.Error("a smaller maxChars must not yield a smaller raw budget")
	}
}
