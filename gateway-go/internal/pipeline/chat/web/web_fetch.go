// web_fetch.go — Unified web tool: search, fetch, and search+fetch in one.
//
// Four modes via parameter dispatch:
//
//	{"url": "..."}                        → Fetch mode (extract content from URL)
//	{"query": "..."}                      → Search mode (web search results)
//	{"query": "...", "fetch": N}          → Search+fetch (search then auto-fetch top N)
//	{"queries": ["...", "..."]}           → Parallel search (multiple queries at once)
//
// Designed for AI agent consumption with structured metadata, machine-readable
// errors, aggressive noise removal, local AI extraction, and bot-block evasion.
//
// Layer overview:
//   - web_http.go           — HTTP fetch, retry, error type, error classification
//   - web_html.go           — HTML → text (FFI, local AI)
//   - web_html_preprocess.go — HTML noise stripping, metadata, signals, charset
//   - web_content.go        — Content dispatch, metadata type, output formatting
//   - web_fetch_stealth.go  — Browser profiles, bot-block evasion
//   - web_fetch_search.go   — Search providers (Serper, Brave, DuckDuckGo)
//   - fetch_cache.go        — In-memory result cache
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

// fetchGroup collapses duplicate in-flight URL fetches into a single request.
// When multiple goroutines (e.g. search+fetch, concurrent tool calls) request the
// same URL simultaneously, only one fetch executes and the result is shared.
var fetchGroup singleflight

// Tool returns the unified web tool handler (fetch + search + search+fetch).
func Tool(cache *FetchCache, localAI *LocalAIExtractor) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			URL      string   `json:"url"`
			Query    string   `json:"query"`
			Queries  []string `json:"queries"`
			Fetch    int      `json:"fetch"`
			MaxChars int      `json:"maxChars"`
			Count    int      `json:"count"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			//nolint:nilerr // tool returns user-facing error in result string
			return formatFetchError(webFetchErr{
				Code: "invalid_params", Message: err.Error(), Retryable: false,
			}), nil
		}

		// Dispatch by mode.
		switch {
		case p.URL != "":
			// Fetch mode: extract content from URL.
			return webFetchURL(ctx, cache, localAI, p.URL, p.MaxChars)

		case len(p.Queries) > 0:
			// Parallel search mode: multiple queries at once.
			if len(p.Queries) > 5 {
				p.Queries = p.Queries[:5]
			}
			if p.Count <= 0 {
				p.Count = 5
			}
			fetch := p.Fetch
			if fetch > 3 {
				fetch = 3
			}
			return webParallelSearch(ctx, cache, localAI, p.Queries, p.Count, fetch, p.MaxChars)

		case p.Query != "":
			if p.Count <= 0 {
				p.Count = 5
			}
			if p.Fetch > 0 {
				// Search+fetch mode: search then auto-fetch top N.
				if p.Fetch > 3 {
					p.Fetch = 3
				}
				return webSearchAndFetch(ctx, cache, localAI, p.Query, p.Count, p.Fetch, p.MaxChars)
			}
			// Search-only mode: return search results.
			return webSearch(ctx, p.Query, p.Count)

		default:
			return formatFetchError(webFetchErr{
				Code: "missing_params", Message: "either url, query, or queries is required", Retryable: false,
			}), nil
		}
	}
}

// webFetchURL fetches a URL and returns extracted content with metadata envelope.
func webFetchURL(ctx context.Context, cache *FetchCache, localAI *LocalAIExtractor, targetURL string, maxChars int) (string, error) {
	if maxChars <= 0 {
		maxChars = 20000
	}

	// YouTube → transcript.
	if media.IsYouTubeURL(targetURL) {
		return fetchYouTube(ctx, targetURL)
	}

	// Cache hit.
	if cached, ok := cache.Get(targetURL); ok {
		return applyTruncation(cached, maxChars), nil
	}

	// Singleflight: collapse concurrent fetches for the same URL into one request.
	// The result is cached after the first fetch completes.
	v, err := fetchGroup.do(targetURL, func() (any, error) {
		// Prefer Serper's scrape endpoint when available: it returns clean
		// markdown + head metadata, sidesteps bot-blocks, and is cheaper than
		// rendering HTML through our own pipeline. Binary URLs (PDF, Office,
		// archives, media) skip this and fall through to the raw fetcher so
		// liteparse can handle them.
		if key := serperAPIKey(); key != "" && !looksLikeBinaryURL(targetURL) {
			if result, ok := webFetchViaSerper(ctx, cache, key, targetURL); ok {
				return result, nil
			}
		}

		maxBytes := int64(maxChars * 2)
		if maxBytes > 5*1024*1024 {
			maxBytes = 5 * 1024 * 1024
		}

		fetchStart := time.Now()
		result, err := fetchWithRetry(ctx, targetURL, maxBytes)
		fetchMs := time.Since(fetchStart).Milliseconds()
		if err != nil {
			return formatFetchError(classifyFetchError(err, targetURL)), nil
		}

		rawContent := normalizeCharset(result.Data, result.ContentType)
		origChars := len(rawContent)

		meta := webFetchMeta{
			URL: targetURL, FinalURL: result.FinalURL,
			ContentType: result.ContentType, StatusCode: result.StatusCode,
			FetchMs: fetchMs, OrigChars: origChars,
		}

		content := processFetchedContent(ctx, rawContent, result.Data, result.ContentType, targetURL, localAI, &meta)

		meta.ExtractChars = len(content)
		if origChars > 0 {
			meta.Retention = fmt.Sprintf("%.1f%%", float64(meta.ExtractChars)/float64(origChars)*100)
		} else {
			meta.Retention = "0%"
		}
		if meta.WordCount == 0 {
			meta.WordCount = estimateWordCount(content)
		}

		fullResult := formatFetchResult(meta, content)
		cache.Put(targetURL, fullResult)
		return fullResult, nil
	})
	if err != nil {
		return "", err
	}

	return applyTruncation(v.(string), maxChars), nil //nolint:errcheck // type guaranteed by preceding switch
}

// webFetchViaSerper extracts content for a single URL via Serper's dedicated
// scrape endpoint (scrape.serper.dev). Returns (fullResult, true) on success,
// or ("", false) to signal the caller should fall through to the raw HTTP
// fetcher (e.g. non-HTML URL, empty response, or API error).
//
// The returned result is already cached; the caller does not need to re-cache.
func webFetchViaSerper(ctx context.Context, cache *FetchCache, apiKey, targetURL string) (string, bool) {
	fetchStart := time.Now()
	scrape, err := serperScrape(ctx, apiKey, targetURL)
	fetchMs := time.Since(fetchStart).Milliseconds()
	if err != nil {
		return "", false
	}
	content := pickScrapeContent(scrape)
	if strings.TrimSpace(content) == "" {
		return "", false
	}

	origChars := len(content)
	meta := webFetchMeta{
		URL:          targetURL,
		ContentType:  "text/html",
		StatusCode:   200,
		FetchMs:      fetchMs,
		OrigChars:    origChars,
		ExtractChars: origChars,
		Retention:    "100.0%",
		WordCount:    estimateWordCount(content),
		Signals:      []string{"serper_scrape"},
	}
	populateScrapeMetadata(&meta, scrape.Metadata)

	fullResult := formatFetchResult(meta, content)
	cache.Put(targetURL, fullResult)
	return fullResult, true
}

// webParallelSearch runs multiple search queries concurrently and returns
// combined results. Each query runs independently with optional fetch.
// This avoids sequential LLM round-trips for multi-constraint questions.
func webParallelSearch(ctx context.Context, cache *FetchCache, localAI *LocalAIExtractor, queries []string, count, fetch, maxChars int) (string, error) {
	if maxChars <= 0 {
		maxChars = 20000
	}
	perQueryChars := maxChars / len(queries)

	type queryResult struct {
		query   string
		content string
		err     error
	}
	results := make([]queryResult, len(queries))
	var wg sync.WaitGroup
	for i, q := range queries {
		wg.Add(1)
		go func(idx int, query string) {
			defer wg.Done()
			var content string
			var err error
			if fetch > 0 {
				content, err = webSearchAndFetch(ctx, cache, localAI, query, count, fetch, perQueryChars)
			} else {
				content, err = webSearch(ctx, query, count)
			}
			results[idx] = queryResult{query: query, content: content, err: err}
		}(i, q)
	}
	wg.Wait()

	var sb strings.Builder
	fmt.Fprintf(&sb, "<parallel_search queries=\"%d\">\n\n", len(queries))
	for i, r := range results {
		fmt.Fprintf(&sb, "<query index=\"%d\" q=\"%s\">\n", i+1, r.query)
		if r.err != nil {
			fmt.Fprintf(&sb, "Search failed: %s\n", r.err.Error())
		} else {
			sb.WriteString(r.content)
		}
		sb.WriteString("\n</query>\n\n")
	}
	sb.WriteString("</parallel_search>")
	return sb.String(), nil
}

// webSearchAndFetch searches the web and auto-fetches the top N results.
// Uses webSearchWithURLs() to get both formatted output and fetchable URLs.
func webSearchAndFetch(ctx context.Context, cache *FetchCache, localAI *LocalAIExtractor, query string, count, fetchTop, maxChars int) (string, error) {
	if maxChars <= 0 {
		maxChars = 15000
	}

	searchOutput, fetchURLs, err := webSearchWithURLs(ctx, query, count)
	if err != nil {
		return "", err
	}
	if searchOutput == "" {
		return "No results found.", nil
	}

	var sb strings.Builder
	sb.WriteString("<search_results query=\"" + query + "\">\n")
	sb.WriteString(searchOutput)
	sb.WriteString("\n</search_results>\n\n")

	// Auto-fetch top N URLs.
	if fetchTop > len(fetchURLs) {
		fetchTop = len(fetchURLs)
	}
	if fetchTop == 0 {
		sb.WriteString("\n[Note: fetch requested but no fetchable URLs from this search provider. Use web(url=...) to fetch specific pages.]\n")
		return sb.String(), nil
	}

	// Parallel fetch: fan-out to goroutines, collect in order.
	type fetchResult struct {
		content string
		err     error
	}
	perResultChars := maxChars / fetchTop
	results := make([]fetchResult, fetchTop)
	var wg sync.WaitGroup
	for i := range fetchTop {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c, e := webFetchURL(ctx, cache, localAI, fetchURLs[idx], perResultChars)
			results[idx] = fetchResult{content: c, err: e}
		}(i)
	}
	wg.Wait()

	for i := range fetchTop {
		fmt.Fprintf(&sb, "<fetched index=\"%d\" url=\"%s\">\n", i+1, fetchURLs[i])
		if results[i].err != nil {
			fmt.Fprintf(&sb, "Fetch failed: %s\n", results[i].err.Error())
		} else {
			sb.WriteString(results[i].content)
		}
		sb.WriteString("\n</fetched>\n\n")
	}

	return sb.String(), nil
}
