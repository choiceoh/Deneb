package web

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/media"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// --- HTML noise stripping tests ---

func TestStripNoiseElements(t *testing.T) {
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

func TestStripNoiseElements_NestedTags(t *testing.T) {
	html := `<nav><div><nav>inner</nav></div></nav><p>keep</p>`
	result := StripNoiseElements(html)
	if strings.Contains(result, "inner") {
		t.Error("nested nav should be stripped")
	}
	if !strings.Contains(result, "keep") {
		t.Error("content outside nav should survive")
	}
}

func TestStripMatchingBlocks_CookieBanner(t *testing.T) {
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

func TestStripMatchingBlocks_Sidebar(t *testing.T) {
	html := `<main><p>Article</p></main><div class="sidebar widget-area"><p>Widget</p></div>`
	result := StripNoiseElements(html)
	if strings.Contains(result, "Widget") {
		t.Error("sidebar should be stripped")
	}
	if !strings.Contains(result, "Article") {
		t.Error("main content should survive")
	}
}

func TestStripMatchingBlocks_Comments(t *testing.T) {
	html := `<article><p>Content</p></article><section class="comments-section"><p>User said stuff</p></section>`
	result := StripNoiseElements(html)
	if strings.Contains(result, "User said stuff") {
		t.Error("comments section should be stripped")
	}
}

func TestStripTagBlock_NotConfusedByPrefix(t *testing.T) {
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

func TestExtractHTMLMeta(t *testing.T) {
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

func TestExtractHTMLMeta_ReversedAttributes(t *testing.T) {
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

func TestExtractHTMLMeta_FallbackTitle(t *testing.T) {
	html := `<html><head><title>Fallback Title</title></head></html>`
	meta := &webFetchMeta{}
	extractHTMLMeta(html, meta)
	if meta.Title != "Fallback Title" {
		t.Errorf("title = %q, want %q", meta.Title, "Fallback Title")
	}
}

func TestExtractJSONLD(t *testing.T) {
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

func TestExtractJSONLD_ArrayFormat(t *testing.T) {
	html := `<script type="application/ld+json">
[{"@type": "WebPage", "name": "Array Page"}]
</script>`
	meta := &webFetchMeta{}
	extractJSONLD(html, meta)
	if meta.Title != "Array Page" {
		t.Errorf("title = %q, want %q", meta.Title, "Array Page")
	}
}

func TestExtractJSONLD_AuthorString(t *testing.T) {
	html := `<script type="application/ld+json">
{"author": "Simple Author"}
</script>`
	meta := &webFetchMeta{}
	extractJSONLD(html, meta)
	if meta.Author != "Simple Author" {
		t.Errorf("author = %q, want %q", meta.Author, "Simple Author")
	}
}

// --- Signal detection tests ---

func TestDetectSignals(t *testing.T) {
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
					t.Errorf("expected signal %q, got %v", tt.want, signals)
				}
			}
			if tt.want == "" && len(signals) > 0 {
				t.Errorf("expected no signals, got %v", signals)
			}
		})
	}
}

func TestDetectSignals_SPAShell(t *testing.T) {
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
		t.Errorf("expected js_required for SPA shell, got %v", signals)
	}
}

func TestDetectSignals_MetaRefreshLogin(t *testing.T) {
	html := `<html><head><meta http-equiv="refresh" content="0;url=/login"></head></html>`
	signals := detectSignals(html)
	found := false
	for _, s := range signals {
		if s == "redirect_to_login" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected redirect_to_login, got %v", signals)
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

func TestFormatFetchResult_CanonicalSameAsFinal(t *testing.T) {
	meta := webFetchMeta{
		URL: "https://x.com/a", FinalURL: "https://x.com/b", CanonicalURL: "https://x.com/b",
		ContentType: "text/html", StatusCode: 200, Retention: "100.0%",
	}
	result := formatFetchResult(meta, "hello")
	if strings.Contains(result, "Canonical:") {
		t.Error("Canonical should be omitted when same as FinalURL")
	}
}

func TestFormatFetchError(t *testing.T) {
	e := webFetchErr{Code: "http_404", Message: "not found", URL: "https://x.com", Retryable: false}
	result := formatFetchError(e)
	if !strings.Contains(result, "<error>") || !strings.Contains(result, "Code: http_404") {
		t.Errorf("unexpected error format: %s", result)
	}
}

func TestFormatFetchError_WithHint(t *testing.T) {
	e := webFetchErr{Code: "http_403", Message: "forbidden", URL: "https://x.com", Retryable: false,
		Hint: "Try http tool with custom headers"}
	result := formatFetchError(e)
	if !strings.Contains(result, "Hint: Try http tool with custom headers") {
		t.Errorf("missing hint in output: %s", result)
	}
}

func TestClassifyFetchError_Hints(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantHint string
	}{
		{"403", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 403, Message: "forbidden"}, "custom headers"},
		{"429", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 429, Message: "too many requests"}, "Rate limited"},
		{"500", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 500, Message: "internal error"}, "Server error"},
		{"SSRF", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "SSRF: blocked"}, "public URL"},
		{"DNS", &media.MediaFetchError{Code: media.ErrFetchFailed, Message: "no such host"}, "typos"},
		{"timeout", context.DeadlineExceeded, "Retry"},
		{"content_too_large", &media.MediaFetchError{Code: media.ErrMaxBytes, Message: "too big"}, "maxChars"},
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

func TestApplyTruncation_NoTruncation(t *testing.T) {
	result := applyTruncation("short", 1000)
	if result != "short" {
		t.Error("should not truncate short content")
	}
}

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

func TestTruncateAtSection_NoTruncation(t *testing.T) {
	content := "short content"
	result, wasTruncated := truncateAtSection(content, 1000)
	if wasTruncated || result != content {
		t.Error("should not truncate short content")
	}
}

func TestTruncateAtSection_ParagraphBreak(t *testing.T) {
	content := "Paragraph one with some text.\n\nParagraph two with more text.\n\nParagraph three."
	truncated, wasTruncated := truncateAtSection(content, 55)
	if !wasTruncated {
		t.Error("should be truncated")
	}
	// Should cut at paragraph break.
	if !strings.HasSuffix(strings.TrimSpace(truncated), "text.") {
		// It should end at a paragraph boundary.
		if strings.Contains(truncated, "three") {
			t.Error("should not include paragraph three at this limit")
		}
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

func TestProcessJSON(t *testing.T) {
	raw := `{"name":"test","value":42}`
	result := processJSON(raw)
	if !strings.Contains(result, "  \"name\": \"test\"") {
		t.Errorf("expected pretty-printed JSON, got: %s", result)
	}
}

func TestProcessJSON_Invalid(t *testing.T) {
	raw := "not json"
	result := processJSON(raw)
	if result != raw {
		t.Error("invalid JSON should be returned as-is")
	}
}

// --- Helper tests ---

func TestStripThinkingTags(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"<think>reasoning</think>\nContent", "Content"},
		{"No tags here", "No tags here"},
		{"<think>\nmulti\nline\n</think>\n\nAfter", "After"},
	}
	for _, tt := range tests {
		got := jsonutil.StripThinkingTags(tt.input)
		if got != tt.want {
			t.Errorf("StripThinkingTags(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAppendUnique(t *testing.T) {
	ss := appendUnique([]string{"a", "b"}, "b")
	if len(ss) != 2 {
		t.Errorf("expected 2, got %d", len(ss))
	}
	ss = appendUnique(ss, "c")
	if len(ss) != 3 {
		t.Errorf("expected 3, got %d", len(ss))
	}
}

func TestEstimateWordCount(t *testing.T) {
	text := "This is a test sentence with seven words."
	wc := estimateWordCount(text)
	if wc != 8 { // "This" "is" "a" "test" "sentence" "with" "seven" "words."
		t.Errorf("word count = %d, want 8", wc)
	}
}

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
