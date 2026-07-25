// Package linkenrichment owns automatic URL understanding for inbound chat
// messages: extraction, bounded parallel fetch, content conversion, and the
// asynchronous start/join lifecycle used to overlap enrichment with run prep.
package linkenrichment

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/coreparsing/htmlmd"
	"github.com/choiceoh/deneb/gateway-go/internal/core/coreparsing/urlextract"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/web"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

const (
	maxLinksPerMessage     = 5
	maxCharsPerLink        = 12000
	maxTotalLinkChars      = 40000
	linkFetchTimeout       = 10 * time.Second
	totalEnrichmentTimeout = 30 * time.Second
	linkJoinBudget         = 4 * time.Second
	linkFetchMaxBytes      = 2 * 1024 * 1024
)

// FetchFunc retrieves one URL for enrichment. The default uses the web tool's
// stealth, SSRF-safe fetch pipeline; explicit injection keeps tests offline.
type FetchFunc func(ctx context.Context, url string) (data []byte, contentType string, err error)

// Sanitizer applies the chat package's exact wire normalization to the typed
// message and fetched content before persistence.
type Sanitizer func(string) string

// Join waits for a started enrichment without extending the caller's context.
// It always returns at least the sanitized original message.
type Join func(context.Context) string

// Config supplies the stable dependencies shared by every enrichment run.
type Config struct {
	Fetch  FetchFunc
	Logger *slog.Logger
}

// Engine owns immutable fetch/conversion dependencies. Per-message goroutines
// and timers remain scoped to Start and its returned Join.
type Engine struct {
	fetch   FetchFunc
	youtube func(context.Context, string) *media.YouTubeResult
	logger  *slog.Logger
}

// New constructs a link enrichment engine with production-safe defaults.
func New(cfg Config) *Engine {
	if cfg.Fetch == nil {
		cfg.Fetch = webFetch
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Engine{
		fetch:   cfg.Fetch,
		youtube: media.ExtractYouTubeTranscriptNative,
		logger:  cfg.Logger,
	}
}

func webFetch(ctx context.Context, url string) ([]byte, string, error) {
	return web.FetchRaw(ctx, url, linkFetchMaxBytes)
}

type linkContent struct {
	URL     string
	Title   string
	Content string
	Err     string
}

// Start begins enrichment in a goroutine and returns the bounded join used
// after parallel chat preparation. Already-enriched and linkless messages
// return nil so callers retain their normal persist-first path.
func (engine *Engine) Start(ctx context.Context, message string, sanitize Sanitizer) Join {
	if strings.Contains(message, toolport.LinkEnrichmentHeader) {
		return nil
	}
	if len(urlextract.ExtractLinks(message, maxLinksPerMessage)) == 0 {
		return nil
	}
	if sanitize == nil {
		sanitize = func(text string) string { return text }
	}
	if engine == nil {
		engine = New(Config{})
	}

	message = sanitize(message)
	start := time.Now()
	runCtx, cancel := context.WithCancel(ctx)
	type outcome struct {
		summary string
		fetchMs int64
	}
	result := make(chan outcome, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				engine.logger.Error("panic in link enrichment", "panic", recovered)
				result <- outcome{fetchMs: time.Since(start).Milliseconds()}
			}
		}()
		summary := engine.enrichMessage(runCtx, message)
		result <- outcome{summary: summary, fetchMs: time.Since(start).Milliseconds()}
	}()

	return func(joinCtx context.Context) string {
		if joinCtx == nil {
			joinCtx = context.Background()
		}
		remaining := time.Until(start.Add(linkJoinBudget))
		if remaining <= 0 {
			cancel()
			engine.logger.Debug("link enrichment missed prep join budget; sending unenriched",
				"sinceSendMs", time.Since(start).Milliseconds())
			return message
		}
		grace := time.NewTimer(remaining)
		defer grace.Stop()
		select {
		case enriched := <-result:
			cancel()
			if enriched.summary == "" {
				return message
			}
			engine.logger.Info("link enrichment appended",
				"chars", len(enriched.summary),
				"fetchMs", enriched.fetchMs,
				"sinceSendMs", time.Since(start).Milliseconds())
			return sanitize(message + "\n\n" + enriched.summary)
		case <-grace.C:
			cancel()
			engine.logger.Debug("link enrichment missed prep join budget; sending unenriched",
				"sinceSendMs", time.Since(start).Milliseconds(),
				"budgetMs", linkJoinBudget.Milliseconds())
			return message
		case <-joinCtx.Done():
			cancel()
			return message
		}
	}
}

func (engine *Engine) enrichMessage(ctx context.Context, text string) string {
	urls := urlextract.ExtractLinks(text, maxLinksPerMessage)
	if len(urls) == 0 {
		return ""
	}

	enrichCtx, cancel := context.WithTimeout(ctx, totalEnrichmentTimeout)
	defer cancel()
	results := make([]linkContent, len(urls))
	var wg sync.WaitGroup
	for i, url := range urls {
		wg.Add(1)
		go func(index int, target string) {
			defer wg.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					results[index] = linkContent{URL: target, Err: fmt.Sprintf("panic: %v", recovered)}
					engine.logger.Error("panic in link enrichment fetch", "url", target, "panic", recovered)
				}
			}()
			results[index] = engine.fetchAndConvert(enrichCtx, target)
		}(i, url)
	}
	wg.Wait()

	totalChars := 0
	links := make([]linkContent, 0, len(results))
	for _, link := range results {
		contentLen := len(link.Content)
		if contentLen > 0 && totalChars+contentLen > maxTotalLinkChars {
			remaining := maxTotalLinkChars - totalChars
			if remaining <= 0 {
				break
			}
			link.Content = truncateContent(link.Content, remaining)
		}
		totalChars += len(link.Content)
		links = append(links, link)
	}
	return formatLinkSummary(links)
}

func (engine *Engine) fetchAndConvert(ctx context.Context, url string) linkContent {
	if media.IsYouTubeURL(url) {
		return engine.enrichYouTube(ctx, url)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, linkFetchTimeout)
	defer cancel()
	data, contentType, err := engine.fetch(fetchCtx, url)
	if err != nil {
		engine.logger.Debug("link fetch failed", "url", url, "error", err)
		return linkContent{URL: url, Err: err.Error()}
	}
	if len(data) == 0 {
		return linkContent{URL: url, Err: "empty response"}
	}

	var title, content string
	if isHTMLContent(contentType) {
		cleaned := web.StripNoiseElements(string(data))
		converted := htmlmd.ConvertWithOpts(cleaned, htmlmd.Options{StripNoise: true})
		content = converted.Text
		title = converted.Title
	} else {
		content = string(data)
	}
	return linkContent{
		URL:     url,
		Title:   title,
		Content: truncateContent(content, maxCharsPerLink),
	}
}

func (engine *Engine) enrichYouTube(ctx context.Context, url string) linkContent {
	youtubeCtx, cancel := context.WithTimeout(ctx, linkFetchTimeout)
	defer cancel()
	result := engine.youtube(youtubeCtx, url)
	if result == nil {
		engine.logger.Debug("youtube enrichment unavailable (native)", "url", url)
		return linkContent{URL: url, Err: "skipped (native extraction unavailable; use web tool)"}
	}
	title := result.Title
	if title == "" {
		title = url
	}
	return linkContent{
		URL:     url,
		Title:   title,
		Content: truncateContent(media.FormatYouTubeResult(result), maxCharsPerLink),
	}
}

func formatLinkSummary(links []linkContent) string {
	parts := make([]string, 0, len(links))
	for _, link := range links {
		if link.Content == "" {
			continue
		}
		label := link.Title
		if label == "" {
			label = link.URL
		}
		parts = append(parts, fmt.Sprintf("[%s](%s)\n%s", label, link.URL, link.Content))
	}
	if len(parts) == 0 {
		return ""
	}
	var summary strings.Builder
	summary.WriteString("---\n" + toolport.LinkEnrichmentHeader + "\n\n")
	summary.WriteString(strings.Join(parts, "\n\n"))
	summary.WriteString("\n---")
	return summary.String()
}

func isHTMLContent(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml")
}

func truncateContent(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "\n[...truncated]"
}
