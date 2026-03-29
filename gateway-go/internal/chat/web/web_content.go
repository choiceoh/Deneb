// web_content.go — Content classification and output formatting.
//
// Classifies fetched payloads by MIME type and routes to the correct extraction
// path. Also owns output formatting (metadata envelope, error envelope, truncation)
// and the metadata type shared across the web fetch pipeline.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/liteparse"
)

// webFetchMeta holds machine-readable metadata about the fetched page.
type webFetchMeta struct {
	Title        string   `json:"title,omitempty"`
	Description  string   `json:"description,omitempty"`
	URL          string   `json:"url"`
	FinalURL     string   `json:"final_url,omitempty"`
	CanonicalURL string   `json:"canonical_url,omitempty"`
	Language     string   `json:"language,omitempty"`
	Published    string   `json:"published,omitempty"`
	Author       string   `json:"author,omitempty"`
	SiteName     string   `json:"site_name,omitempty"`
	OGType       string   `json:"og_type,omitempty"`
	ContentType  string   `json:"content_type"`
	StatusCode   int      `json:"status_code"`
	FetchMs      int64    `json:"fetch_ms"`
	OrigChars    int      `json:"original_chars"`
	ExtractChars int      `json:"extracted_chars"`
	Retention    string   `json:"retention_ratio"`
	Truncated    bool     `json:"truncated"`
	WordCount    int      `json:"word_count,omitempty"`
	Signals      []string `json:"signals,omitempty"`
}

// --- Content type classification ---

type fetchedContentType string

const (
	contentTypePlain    fetchedContentType = "plain"
	contentTypeHTML     fetchedContentType = "html"
	contentTypeJSON     fetchedContentType = "json"
	contentTypeDocument fetchedContentType = "document"
)

func classifyContentType(contentType string) fetchedContentType {
	switch {
	case strings.Contains(contentType, "text/html"),
		strings.Contains(contentType, "application/xhtml"):
		return contentTypeHTML
	case strings.Contains(contentType, "application/json"),
		strings.Contains(contentType, "+json"):
		return contentTypeJSON
	case liteparse.Available() && liteparse.SupportedMIME(contentType):
		return contentTypeDocument
	default:
		return contentTypePlain
	}
}

// processFetchedContent classifies fetched payloads and routes each type
// to the correct extraction/formatting path.
func processFetchedContent(
	ctx context.Context,
	rawContent string,
	rawBytes []byte,
	contentType string,
	url string,
	sglang *SGLangExtractor,
	meta *webFetchMeta,
) string {
	switch classifyContentType(contentType) {
	case contentTypeHTML:
		return processHTML(ctx, rawContent, url, sglang, meta)
	case contentTypeJSON:
		return processJSON(rawContent)
	case contentTypeDocument:
		// Use raw bytes (not charset-normalized string) for binary documents.
		return processDocument(ctx, rawBytes, url)
	default:
		return rawContent
	}
}

// processJSON pretty-prints JSON for readability.
func processJSON(raw string) string {
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return raw // invalid JSON — return as-is
	}
	pretty, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return raw
	}
	return string(pretty)
}

// processDocument extracts text from a binary document (PDF, Office, etc.)
// using the LiteParse CLI. Falls back to a notice if parsing fails.
func processDocument(ctx context.Context, data []byte, url string) string {
	name := "document"
	if url != "" {
		// Extract filename from URL path.
		if idx := strings.LastIndex(url, "/"); idx >= 0 {
			name = url[idx+1:]
		}
		// Strip query string.
		if idx := strings.Index(name, "?"); idx >= 0 {
			name = name[:idx]
		}
	}

	text, err := liteparse.Parse(ctx, data, name)
	if err != nil {
		return fmt.Sprintf("(문서 파싱 실패: %s)", err)
	}
	if strings.TrimSpace(text) == "" {
		return "(문서에서 텍스트를 추출하지 못했습니다)"
	}
	return text
}

// --- Output formatting ---

func formatFetchResult(meta webFetchMeta, content string) string {
	var b strings.Builder
	b.Grow(len(content) + 512)

	b.WriteString("<metadata>\n")
	if meta.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", meta.Title)
	}
	if meta.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", meta.Description)
	}
	if meta.Author != "" {
		fmt.Fprintf(&b, "Author: %s\n", meta.Author)
	}
	if meta.SiteName != "" {
		fmt.Fprintf(&b, "Site: %s\n", meta.SiteName)
	}
	fmt.Fprintf(&b, "URL: %s\n", meta.URL)
	if meta.FinalURL != "" && meta.FinalURL != meta.URL {
		fmt.Fprintf(&b, "FinalURL: %s\n", meta.FinalURL)
	}
	if meta.CanonicalURL != "" && meta.CanonicalURL != meta.URL && meta.CanonicalURL != meta.FinalURL {
		fmt.Fprintf(&b, "Canonical: %s\n", meta.CanonicalURL)
	}
	if meta.Language != "" {
		fmt.Fprintf(&b, "Language: %s\n", meta.Language)
	}
	if meta.Published != "" {
		fmt.Fprintf(&b, "Published: %s\n", meta.Published)
	}
	if meta.OGType != "" {
		fmt.Fprintf(&b, "Type: %s\n", meta.OGType)
	}
	fmt.Fprintf(&b, "ContentType: %s\n", meta.ContentType)
	fmt.Fprintf(&b, "StatusCode: %d\n", meta.StatusCode)
	fmt.Fprintf(&b, "FetchTime: %dms\n", meta.FetchMs)
	fmt.Fprintf(&b, "ContentChars: %d (original: %d, retention: %s)\n",
		meta.ExtractChars, meta.OrigChars, meta.Retention)
	if meta.WordCount > 0 {
		fmt.Fprintf(&b, "WordCount: %d\n", meta.WordCount)
	}
	if meta.Truncated {
		b.WriteString("Truncated: true\n")
	}
	if len(meta.Signals) > 0 {
		fmt.Fprintf(&b, "Signals: %s\n", strings.Join(meta.Signals, ", "))
	}
	b.WriteString("</metadata>\n<content>\n")
	b.WriteString(content)
	b.WriteString("\n</content>")

	return b.String()
}

func formatFetchError(e webFetchErr) string {
	var b strings.Builder
	b.WriteString("<error>\n")
	fmt.Fprintf(&b, "Code: %s\n", e.Code)
	fmt.Fprintf(&b, "Message: %s\n", e.Message)
	if e.URL != "" {
		fmt.Fprintf(&b, "URL: %s\n", e.URL)
	}
	fmt.Fprintf(&b, "Retryable: %v\n", e.Retryable)
	b.WriteString("</error>")
	return b.String()
}

// applyTruncation truncates a formatted result preserving the metadata section
// and cutting content at section boundaries rather than mid-sentence.
func applyTruncation(result string, maxChars int) string {
	if len(result) <= maxChars {
		return result
	}

	// Split at content boundary.
	contentStart := strings.Index(result, "<content>\n")
	if contentStart < 0 || contentStart >= maxChars {
		return result[:maxChars] + "\n[...truncated]"
	}

	metaSection := result[:contentStart+len("<content>\n")]
	contentBody := result[contentStart+len("<content>\n"):]

	// Remove trailing </content> for processing.
	contentBody = strings.TrimSuffix(contentBody, "\n</content>")

	// Available chars for content.
	availChars := maxChars - len(metaSection) - 40 // 40 for truncation marker + closing tag
	if availChars <= 0 {
		return metaSection + "\n[...no space for content]\n</content>"
	}

	truncated, wasTruncated := truncateAtSection(contentBody, availChars)
	if wasTruncated {
		remaining := len(contentBody) - len(truncated)
		return metaSection + truncated +
			"\n\n[...truncated: " + strconv.Itoa(remaining) + " chars remaining]\n</content>"
	}
	return metaSection + truncated + "\n</content>"
}

// truncateAtSection truncates markdown content at a section boundary
// (heading or paragraph break) near the target length, rather than
// cutting mid-sentence. This preserves structural coherence.
func truncateAtSection(content string, maxChars int) (string, bool) {
	if len(content) <= maxChars {
		return content, false
	}

	// Search backward from maxChars for a good break point.
	// Priority: heading > double newline > single newline.
	searchStart := maxChars
	if searchStart > len(content) {
		searchStart = len(content)
	}

	// Look for the last heading within the limit.
	bestBreak := -1
	window := content[:searchStart]

	// Find last heading (# at start of line).
	for i := searchStart - 1; i > maxChars/2; i-- {
		if i > 0 && content[i] == '#' && content[i-1] == '\n' {
			bestBreak = i - 1
			break
		}
	}

	// If no heading found, look for last paragraph break (double newline).
	if bestBreak < 0 {
		if idx := strings.LastIndex(window[maxChars/2:], "\n\n"); idx >= 0 {
			bestBreak = maxChars/2 + idx
		}
	}

	// If still nothing, look for last single newline.
	if bestBreak < 0 {
		if idx := strings.LastIndex(window[maxChars*3/4:], "\n"); idx >= 0 {
			bestBreak = maxChars*3/4 + idx
		}
	}

	// Fallback: hard cut.
	if bestBreak < 0 {
		bestBreak = maxChars
	}

	return content[:bestBreak], true
}

// estimateWordCount estimates word count from text content.
// Uses a simple split on whitespace, which works for both Latin and CJK text.
func estimateWordCount(text string) int {
	fields := strings.Fields(text)
	return len(fields)
}

// appendUnique appends s to ss only if not already present.
func appendUnique(ss []string, s string) []string {
	for _, existing := range ss {
		if existing == s {
			return ss
		}
	}
	return append(ss, s)
}
