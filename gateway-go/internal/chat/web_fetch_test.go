package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/media"
)

func TestExtractHTMLMeta(t *testing.T) {
	html := `<html lang="ko">
<head>
<title>테스트 페이지</title>
<meta property="og:title" content="OG Title">
<meta property="og:description" content="OG Description">
<meta name="description" content="Meta Description">
<link rel="canonical" href="https://example.com/canonical">
<meta property="article:published_time" content="2024-01-15T09:00:00Z">
</head>
<body>Content</body>
</html>`

	meta := &webFetchMeta{}
	extractHTMLMeta(html, meta)

	if meta.Title != "OG Title" {
		t.Errorf("title = %q, want %q", meta.Title, "OG Title")
	}
	if meta.Description != "OG Description" {
		t.Errorf("description = %q, want %q", meta.Description, "OG Description")
	}
	if meta.CanonicalURL != "https://example.com/canonical" {
		t.Errorf("canonical = %q, want %q", meta.CanonicalURL, "https://example.com/canonical")
	}
	if meta.Language != "ko" {
		t.Errorf("language = %q, want %q", meta.Language, "ko")
	}
	if meta.Published != "2024-01-15T09:00:00Z" {
		t.Errorf("published = %q, want %q", meta.Published, "2024-01-15T09:00:00Z")
	}
}

func TestExtractHTMLMeta_FallbackTitle(t *testing.T) {
	html := `<html><head><title>Fallback Title</title></head><body></body></html>`
	meta := &webFetchMeta{}
	extractHTMLMeta(html, meta)
	if meta.Title != "Fallback Title" {
		t.Errorf("title = %q, want %q", meta.Title, "Fallback Title")
	}
}

func TestExtractHTMLMeta_ReversedAttributes(t *testing.T) {
	// Some pages put content before property in meta tags.
	html := `<html><head>
<meta content="Rev OG Title" property="og:title">
<meta content="Rev Desc" name="description">
<link href="https://example.com/rev" rel="canonical">
</head></html>`
	meta := &webFetchMeta{}
	extractHTMLMeta(html, meta)
	if meta.Title != "Rev OG Title" {
		t.Errorf("title = %q, want %q", meta.Title, "Rev OG Title")
	}
	if meta.Description != "Rev Desc" {
		t.Errorf("description = %q, want %q", meta.Description, "Rev Desc")
	}
	if meta.CanonicalURL != "https://example.com/rev" {
		t.Errorf("canonical = %q, want %q", meta.CanonicalURL, "https://example.com/rev")
	}
}

func TestDetectSignals(t *testing.T) {
	tests := []struct {
		name    string
		html    string
		want    string // signal to expect (empty means none)
		wantNot string // signal that should NOT be present
	}{
		{
			"login wall",
			`<div class="login-wall">Please sign in</div>`,
			"login_wall_detected", "",
		},
		{
			"paywall",
			`<div class="paywall">Subscribe to continue</div>`,
			"login_wall_detected", "",
		},
		{
			"js required",
			`<noscript>You need to enable JavaScript to run this app.</noscript>`,
			"js_required", "",
		},
		{
			"cloudflare blocked",
			`<h1>Blocked by Cloudflare</h1>`,
			"bot_blocked", "",
		},
		{
			"captcha",
			`<div>Please verify you are a human</div>`,
			"bot_blocked", "",
		},
		{
			"clean page",
			`<html><body><h1>Hello World</h1><p>Content here</p></body></html>`,
			"", "",
		},
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

func TestClassifyFetchError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCode  string
		wantRetry bool
	}{
		{
			"404 not found",
			&media.MediaFetchError{Code: media.ErrHTTPError, Status: 404, Message: "not found"},
			"http_404", false,
		},
		{
			"503 service unavailable",
			&media.MediaFetchError{Code: media.ErrHTTPError, Status: 503, Message: "unavailable"},
			"http_503", true,
		},
		{
			"SSRF blocked",
			&media.MediaFetchError{Code: media.ErrFetchFailed, Message: "SSRF: private IP blocked"},
			"ssrf_blocked", false,
		},
		{
			"DNS failure",
			&media.MediaFetchError{Code: media.ErrFetchFailed, Message: "no such host"},
			"dns_failure", false,
		},
		{
			"content too large",
			&media.MediaFetchError{Code: media.ErrMaxBytes, Message: "too big"},
			"content_too_large", false,
		},
		{
			"timeout",
			context.DeadlineExceeded,
			"timeout", true,
		},
		{
			"redirect loop",
			&media.MediaFetchError{Code: media.ErrFetchFailed, Message: "too many redirects (5)"},
			"redirect_loop", false,
		},
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

func TestFormatFetchResult(t *testing.T) {
	meta := webFetchMeta{
		Title:       "Test Page",
		URL:         "https://example.com",
		FinalURL:    "https://example.com/final",
		Language:    "en",
		ContentType: "text/html",
		StatusCode:  200,
		OrigChars:   10000,
		ExtractChars: 5000,
		Retention:   "50.0%",
		Signals:     []string{"js_required"},
	}
	content := "# Hello\n\nThis is the content."

	result := formatFetchResult(meta, content)

	// Verify metadata section.
	if !strings.Contains(result, "<metadata>") {
		t.Error("missing <metadata> tag")
	}
	if !strings.Contains(result, "Title: Test Page") {
		t.Error("missing title")
	}
	if !strings.Contains(result, "FinalURL: https://example.com/final") {
		t.Error("missing final URL")
	}
	if !strings.Contains(result, "Language: en") {
		t.Error("missing language")
	}
	if !strings.Contains(result, "Signals: js_required") {
		t.Error("missing signals")
	}
	if !strings.Contains(result, "</metadata>") {
		t.Error("missing </metadata> tag")
	}

	// Verify content section.
	if !strings.Contains(result, "<content>") {
		t.Error("missing <content> tag")
	}
	if !strings.Contains(result, "# Hello") {
		t.Error("missing content")
	}
}

func TestFormatFetchResult_NoRedirect(t *testing.T) {
	meta := webFetchMeta{
		URL:         "https://example.com",
		FinalURL:    "https://example.com", // same as URL
		ContentType: "text/plain",
		StatusCode:  200,
		OrigChars:   100,
		ExtractChars: 100,
		Retention:   "100.0%",
	}
	result := formatFetchResult(meta, "hello")
	// FinalURL should NOT be shown when it matches URL.
	if strings.Contains(result, "FinalURL:") {
		t.Error("FinalURL should be omitted when same as URL")
	}
}

func TestFormatFetchError(t *testing.T) {
	e := webFetchErr{
		Code:      "http_404",
		Message:   "Page not found",
		URL:       "https://example.com/missing",
		Retryable: false,
	}
	result := formatFetchError(e)
	if !strings.Contains(result, "<error>") {
		t.Error("missing <error> tag")
	}
	if !strings.Contains(result, "Code: http_404") {
		t.Error("missing error code")
	}
	if !strings.Contains(result, "Retryable: false") {
		t.Error("missing retryable field")
	}
}

func TestTruncateResult(t *testing.T) {
	meta := webFetchMeta{
		URL:         "https://example.com",
		ContentType: "text/html",
		StatusCode:  200,
		OrigChars:   100,
		ExtractChars: 100,
		Retention:   "100.0%",
	}
	content := strings.Repeat("a", 1000)
	result := formatFetchResult(meta, content)

	truncated := truncateResult(result, 500)
	if len(truncated) > 600 { // 500 + truncation marker overhead
		t.Errorf("truncated result too long: %d chars", len(truncated))
	}
	if !strings.Contains(truncated, "truncated at") {
		t.Error("missing truncation marker")
	}
	// Metadata section should be preserved.
	if !strings.Contains(truncated, "<metadata>") {
		t.Error("metadata section not preserved after truncation")
	}
}

func TestTruncateResult_NoTruncation(t *testing.T) {
	short := "short content"
	result := truncateResult(short, 1000)
	if result != short {
		t.Errorf("short content should not be truncated")
	}
}

func TestStripThinkingTags(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			"<think>reasoning here</think>\nActual content",
			"Actual content",
		},
		{
			"No thinking tags here",
			"No thinking tags here",
		},
		{
			"<think>\nmulti\nline\nthinking\n</think>\n\nContent after",
			"Content after",
		},
	}
	for _, tt := range tests {
		got := stripThinkingTags(tt.input)
		if got != tt.want {
			t.Errorf("stripThinkingTags(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAppendUnique(t *testing.T) {
	ss := []string{"a", "b"}
	ss = appendUnique(ss, "b")
	if len(ss) != 2 {
		t.Errorf("expected 2 items, got %d", len(ss))
	}
	ss = appendUnique(ss, "c")
	if len(ss) != 3 {
		t.Errorf("expected 3 items, got %d", len(ss))
	}
}
