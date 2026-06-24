// link_enrichment.go — Automatic link understanding for inbound messages.
//
// When a user message contains URLs, this module extracts them, fetches their
// content, converts HTML to readable markdown, and appends a formatted summary
// to the message before it reaches the LLM agent. This saves the agent a web
// tool turn and provides immediate context.
//
// Wired into the synchronous send paths (SendSync / SendSyncStream — the
// native client's miniapp.chat.send and the SSE stream) via maybeEnrichLinks.
// The enriched text is persisted in the transcript as-is (prompt-cache rule:
// what the LLM saw is what history reloads), and History() strips the block
// for display so the user's chat bubble shows what they typed.
package chat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/coreparsing/htmlmd"
	"github.com/choiceoh/deneb/gateway-go/internal/core/coreparsing/urlextract"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/web"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

// Link enrichment limits.
const (
	maxLinksPerMessage     = 5
	maxCharsPerLink        = 12000
	maxTotalLinkChars      = 40000
	linkFetchTimeout       = 10 * time.Second
	totalEnrichmentTimeout = 30 * time.Second
	// linkFetchMaxBytes caps the raw download per link. Raw HTML is much
	// larger than the markdown it converts to, so this is intentionally far
	// above maxCharsPerLink.
	linkFetchMaxBytes = 2 * 1024 * 1024
)

// The enrichment block marker lives in toolctx (LinkEnrichmentHeader) so the
// display strips shared by chat.history and miniapp.sessions.transcript stay
// in sync with the formatter below.

// fetchFunc abstracts URL fetching for testability.
// Returns raw data, content-type header, and error.
type fetchFunc func(ctx context.Context, url string) (data []byte, contentType string, err error)

// webFetch is the production fetcher: the web tool's stealth fetch pipeline
// (pooled SSRF-safe transport, browser-like profiles, bot-block escalation).
func webFetch(ctx context.Context, url string) ([]byte, string, error) {
	return web.FetchRaw(ctx, url, linkFetchMaxBytes)
}

// linkContent holds the fetched and converted content for a single URL.
type linkContent struct {
	URL     string
	Title   string
	Content string
	Err     string // non-empty if fetch failed
}

// maybeEnrichLinks appends fetched link content to an interactive chat message.
// No-op (returns message unchanged) when the turn carries prebuilt API history,
// when the message has no fetchable URLs, or when every fetch fails.
func (h *Handler) maybeEnrichLinks(ctx context.Context, message string, opts *SyncOptions) string {
	if opts != nil && len(opts.Messages) > 0 {
		// OpenAI-compatible API traffic with caller-owned history — leave it
		// untouched, same boundary as trySlashSync.
		return message
	}
	if strings.Contains(message, toolctx.LinkEnrichmentHeader) {
		// Already enriched (e.g. a resent message) — never stack blocks.
		return message
	}
	start := time.Now()
	summary := enrichMessageWithLinks(ctx, message, webFetch, h.logger)
	if summary == "" {
		return message
	}
	h.logger.Info("link enrichment appended",
		"chars", len(summary), "elapsed", time.Since(start).Round(time.Millisecond))
	return message + "\n\n" + summary
}

// enrichMessageWithLinks extracts URLs from the message, fetches each one,
// converts HTML to markdown, and returns a formatted summary string.
// Returns "" if no links found or all fetches fail.
func enrichMessageWithLinks(ctx context.Context, text string, fetchFn fetchFunc, logger *slog.Logger) string {
	urls := urlextract.ExtractLinks(text, maxLinksPerMessage)
	if len(urls) == 0 {
		return ""
	}

	enrichCtx, enrichCancel := context.WithTimeout(ctx, totalEnrichmentTimeout)
	defer enrichCancel()

	// Parallel fetch: fan-out to goroutines, collect in order.
	results := make([]linkContent, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func(idx int, target string) {
			defer wg.Done()
			results[idx] = fetchAndConvert(enrichCtx, target, fetchFn, logger)
		}(i, u)
	}
	wg.Wait()

	// Apply total content budget in order.
	totalChars := 0
	var links []linkContent
	for _, lc := range results {
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

// youtubeExtract is the YouTube enrichment extractor: native-only (innertube
// captions + chapters + metadata, no yt-dlp/ASR) so it stays light enough for
// the synchronous send path. A package var so tests can stub it without network.
var youtubeExtract = media.ExtractYouTubeTranscriptNative

// fetchAndConvert fetches a single URL and converts the content.
func fetchAndConvert(ctx context.Context, url string, fetchFn fetchFunc, logger *slog.Logger) linkContent {
	// YouTube URLs can't be understood from a plain HTTP fetch (JS-heavy page, no
	// transcript). Extract captions + chapters + metadata natively so a pasted
	// link is summarizable immediately without an explicit web-tool turn. The
	// native path is HTTP-only (no subprocess); when it can't serve the video the
	// agent's web tool still covers the heavy fallback (yt-dlp + ASR).
	if media.IsYouTubeURL(url) {
		return enrichYouTube(ctx, url, logger)
	}

	fetchCtx, fetchCancel := context.WithTimeout(ctx, linkFetchTimeout)
	defer fetchCancel()

	data, contentType, err := fetchFn(fetchCtx, url)
	if err != nil {
		logger.Debug("link fetch failed", "url", url, "error", err)
		return linkContent{URL: url, Err: err.Error()}
	}

	if len(data) == 0 {
		return linkContent{URL: url, Err: "empty response"}
	}

	var title, content string

	if isHTMLContent(contentType) {
		cleaned := web.StripNoiseElements(string(data))
		r := htmlmd.ConvertWithOpts(cleaned, htmlmd.Options{StripNoise: true})
		content = r.Text
		title = r.Title
	} else {
		content = string(data)
	}

	content = truncateContent(content, maxCharsPerLink)

	return linkContent{
		URL:     url,
		Title:   title,
		Content: content,
	}
}

// enrichYouTube extracts a YouTube link's transcript + chapters + metadata via
// the native path and renders it as link content. Bounded by linkFetchTimeout
// and the shared per-link char budget. Returns an error-marked entry (which the
// summary skips) when the native path yields nothing, leaving the heavy fallback
// to the agent's web tool.
func enrichYouTube(ctx context.Context, url string, logger *slog.Logger) linkContent {
	yctx, cancel := context.WithTimeout(ctx, linkFetchTimeout)
	defer cancel()

	res := youtubeExtract(yctx, url)
	if res == nil {
		logger.Debug("youtube enrichment unavailable (native)", "url", url)
		return linkContent{URL: url, Err: "skipped (native extraction unavailable; use web tool)"}
	}

	title := res.Title
	if title == "" {
		title = url
	}
	return linkContent{
		URL:     url,
		Title:   title,
		Content: truncateContent(media.FormatYouTubeResult(res), maxCharsPerLink),
	}
}

// formatLinkSummary builds the summary block from fetched link contents.
// Skips links that failed to fetch entirely.
func formatLinkSummary(links []linkContent) string {
	var parts []string
	for _, lc := range links {
		if lc.Content == "" {
			continue // failed or empty links carry nothing for the agent
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
	b.WriteString("---\n" + toolctx.LinkEnrichmentHeader + "\n\n")
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
