// link_enrichment.go — Automatic link understanding for inbound messages.
//
// When a user message contains URLs, this module extracts them, fetches their
// content, converts HTML to readable markdown, and returns a formatted summary
// to append to the message before it reaches the LLM agent. This saves the
// agent a web_fetch tool turn and provides immediate context.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/media"
)

// Link enrichment limits.
const (
	maxLinksPerMessage     = 5
	maxCharsPerLink        = 12000
	maxTotalLinkChars      = 40000
	linkFetchTimeout       = 10 * time.Second
	totalEnrichmentTimeout = 30 * time.Second
	linkFetchMaxBytes      = int64(2 * 1024 * 1024) // 2 MB raw download
)

// FetchFunc abstracts URL fetching for testability.
// Returns raw data, content-type header, and error.
type FetchFunc func(ctx context.Context, url string) (data []byte, contentType string, err error)

// LinkContent holds the fetched and converted content for a single URL.
type LinkContent struct {
	URL     string
	Title   string
	Content string
	Err     string // non-empty if fetch failed
}

// defaultLinkFetcher wraps media.Fetch for production use.
func defaultLinkFetcher(ctx context.Context, url string) ([]byte, string, error) {
	result, err := media.Fetch(ctx, media.FetchOptions{
		URL:      url,
		MaxBytes: linkFetchMaxBytes,
		Headers: map[string]string{
			"User-Agent": "Deneb-Gateway/1.0",
			"Accept":     "text/html,text/plain,application/json,*/*",
		},
	})
	if err != nil {
		return nil, "", err
	}
	return result.Data, result.ContentType, nil
}

// EnrichMessageWithLinks extracts URLs from the message, fetches each one,
// converts HTML to markdown, and returns a formatted summary string.
// Returns "" if no links found or all fetches fail.
func EnrichMessageWithLinks(ctx context.Context, text string, fetchFn FetchFunc, logger *slog.Logger) string {
	urls, err := ffi.ExtractLinks(text, maxLinksPerMessage)
	if err != nil {
		logger.Warn("link extraction failed", "error", err)
		return ""
	}
	if len(urls) == 0 {
		return ""
	}

	enrichCtx, enrichCancel := context.WithTimeout(ctx, totalEnrichmentTimeout)
	defer enrichCancel()

	var links []LinkContent
	totalChars := 0

	for _, url := range urls {
		if enrichCtx.Err() != nil {
			break
		}

		lc := fetchAndConvert(enrichCtx, url, fetchFn, logger)

		// Track total content budget.
		contentLen := len(lc.Content)
		if contentLen > 0 && totalChars+contentLen > maxTotalLinkChars {
			remaining := maxTotalLinkChars - totalChars
			if remaining <= 0 {
				break
			}
			lc.Content = truncateContent(lc.Content, remaining)
		}
		totalChars += len(lc.Content)

		links = append(links, lc)
	}

	return formatLinkSummary(links)
}

// fetchAndConvert fetches a single URL and converts the content.
func fetchAndConvert(ctx context.Context, url string, fetchFn FetchFunc, logger *slog.Logger) LinkContent {
	fetchCtx, fetchCancel := context.WithTimeout(ctx, linkFetchTimeout)
	defer fetchCancel()

	data, contentType, err := fetchFn(fetchCtx, url)
	if err != nil {
		logger.Debug("link fetch failed", "url", url, "error", err)
		return LinkContent{URL: url, Err: err.Error()}
	}

	if len(data) == 0 {
		return LinkContent{URL: url, Err: "empty response"}
	}

	var title, content string

	if isHTMLContent(contentType) {
		text, t, err := ffi.HtmlToMarkdown(string(data))
		if err != nil {
			logger.Debug("html-to-markdown failed", "url", url, "error", err)
			// Fall back to raw text with basic tag stripping.
			content = string(data)
		} else {
			content = text
			title = t
		}
	} else {
		content = string(data)
	}

	content = truncateContent(content, maxCharsPerLink)

	return LinkContent{
		URL:     url,
		Title:   title,
		Content: content,
	}
}

// formatLinkSummary builds the summary block from fetched link contents.
// Skips links that failed to fetch entirely.
func formatLinkSummary(links []LinkContent) string {
	var parts []string
	for _, lc := range links {
		if lc.Content == "" && lc.Err != "" {
			continue // skip failed links
		}
		if lc.Content == "" {
			continue
		}

		label := lc.Title
		if label == "" {
			label = lc.URL
		}
		part := fmt.Sprintf("[%s](%s)\n%s", label, lc.URL, lc.Content)
		parts = append(parts, part)
	}

	if len(parts) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("---\nLink content from URLs in this message:\n\n")
	b.WriteString(strings.Join(parts, "\n\n"))
	b.WriteString("\n---")
	return b.String()
}

// isHTMLContent checks if the content-type indicates HTML.
func isHTMLContent(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml")
}

// truncateContent truncates text to maxLen characters with a marker.
func truncateContent(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "\n[...truncated]"
}
