package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// stubFetcher returns a FetchFunc that serves canned responses keyed by URL.
func stubFetcher(responses map[string]struct {
	data        []byte
	contentType string
	err         error
}) FetchFunc {
	return func(_ context.Context, url string) ([]byte, string, error) {
		resp, ok := responses[url]
		if !ok {
			return nil, "", fmt.Errorf("not found: %s", url)
		}
		return resp.data, resp.contentType, resp.err
	}
}

func TestEnrichMessageWithLinks_NoLinks(t *testing.T) {
	logger := slog.Default()
	result := EnrichMessageWithLinks(context.Background(), "hello world", nil, logger)
	if result != "" {
		t.Fatalf("expected empty string for message without links, got: %q", result)
	}
}

func TestEnrichMessageWithLinks_SingleHTMLLink(t *testing.T) {
	logger := slog.Default()
	html := `<html><head><title>Example</title></head><body><p>Hello world</p></body></html>`

	fetch := stubFetcher(map[string]struct {
		data        []byte
		contentType string
		err         error
	}{
		"https://example.com": {[]byte(html), "text/html; charset=utf-8", nil},
	})

	result := EnrichMessageWithLinks(context.Background(), "check https://example.com out", fetch, logger)

	if !strings.Contains(result, "Link content from URLs") {
		t.Fatal("expected link summary header")
	}
	if !strings.Contains(result, "Example") {
		t.Fatal("expected title in output")
	}
	if !strings.Contains(result, "https://example.com") {
		t.Fatal("expected URL in output")
	}
}

func TestEnrichMessageWithLinks_PlainText(t *testing.T) {
	logger := slog.Default()
	fetch := stubFetcher(map[string]struct {
		data        []byte
		contentType string
		err         error
	}{
		"https://api.example.com/data": {[]byte(`{"key":"value"}`), "application/json", nil},
	})

	result := EnrichMessageWithLinks(context.Background(), "https://api.example.com/data", fetch, logger)

	if !strings.Contains(result, `{"key":"value"}`) {
		t.Fatal("expected JSON content in output")
	}
}

func TestEnrichMessageWithLinks_FetchFailure(t *testing.T) {
	logger := slog.Default()
	fetch := stubFetcher(map[string]struct {
		data        []byte
		contentType string
		err         error
	}{
		"https://example.com": {nil, "", fmt.Errorf("connection refused")},
	})

	result := EnrichMessageWithLinks(context.Background(), "https://example.com", fetch, logger)

	if result != "" {
		t.Fatalf("expected empty string when all fetches fail, got: %q", result)
	}
}

func TestEnrichMessageWithLinks_Truncation(t *testing.T) {
	logger := slog.Default()
	longContent := strings.Repeat("a", maxCharsPerLink+1000)

	fetch := stubFetcher(map[string]struct {
		data        []byte
		contentType string
		err         error
	}{
		"https://example.com": {[]byte(longContent), "text/plain", nil},
	})

	result := EnrichMessageWithLinks(context.Background(), "https://example.com", fetch, logger)

	if !strings.Contains(result, "[...truncated]") {
		t.Fatal("expected truncation marker")
	}
}

func TestFormatLinkSummary_Empty(t *testing.T) {
	result := formatLinkSummary(nil)
	if result != "" {
		t.Fatalf("expected empty string for nil links, got: %q", result)
	}

	result = formatLinkSummary([]LinkContent{{URL: "https://x.com", Err: "failed"}})
	if result != "" {
		t.Fatalf("expected empty string for all-failed links, got: %q", result)
	}
}

func TestFormatLinkSummary_WithTitle(t *testing.T) {
	links := []LinkContent{
		{URL: "https://example.com", Title: "Example Page", Content: "Hello world"},
	}
	result := formatLinkSummary(links)

	if !strings.Contains(result, "[Example Page](https://example.com)") {
		t.Fatal("expected markdown link with title")
	}
	if !strings.Contains(result, "Hello world") {
		t.Fatal("expected content")
	}
}
