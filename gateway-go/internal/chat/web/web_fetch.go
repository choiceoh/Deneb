// web_fetch.go — Unified web tool: search, fetch, and search+fetch in one.
//
// Three modes via parameter dispatch:
//
//	{"url": "..."}                        → Fetch mode (extract content from URL)
//	{"query": "..."}                      → Search mode (web search results)
//	{"query": "...", "fetch": N}          → Search+fetch (search then auto-fetch top N)
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
//   - web_fetch_search.go   — Search providers (Perplexity, Brave, DuckDuckGo)
//   - fetch_cache.go        — In-memory result cache
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/media"
	"golang.org/x/sync/singleflight"
)

// fetchGroup collapses duplicate in-flight URL fetches into a single request.
// When multiple goroutines (e.g. search+fetch, concurrent tool calls) request the
// same URL simultaneously, only one fetch executes and the result is shared.
var fetchGroup singleflight.Group

// Tool returns the unified web tool handler (fetch + search + search+fetch).
func Tool(cache *FetchCache, localAI *LocalAIExtractor) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			URL      string `json:"url"`
			Query    string `json:"query"`
			Fetch    int    `json:"fetch"`
			MaxChars int    `json:"maxChars"`
			Count    int    `json:"count"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return formatFetchError(webFetchErr{
				Code: "invalid_params", Message: err.Error(), Retryable: false,
			}), nil
		}

		// Dispatch by mode.
		switch {
		case p.URL != "":
			// Fetch mode: extract content from URL.
			return webFetchURL(ctx, cache, localAI, p.URL, p.MaxChars)

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
				Code: "missing_params", Message: "either url or query is required", Retryable: false,
			}), nil
		}
	}
}

// webFetchURL fetches a URL and returns extracted content with metadata envelope.
func webFetchURL(ctx context.Context, cache *FetchCache, localAI *LocalAIExtractor, targetURL string, maxChars int) (string, error) {
	if maxChars <= 0 {
		maxChars = 50000
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
	v, err, _ := fetchGroup.Do(targetURL, func() (any, error) {
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

	return applyTruncation(v.(string), maxChars), nil
}

// webSearchAndFetch searches the web and auto-fetches the top N results.
// Uses webSearchWithURLs() to get both formatted output and fetchable URLs.
func webSearchAndFetch(ctx context.Context, cache *FetchCache, localAI *LocalAIExtractor, query string, count, fetchTop, maxChars int) (string, error) {
	if maxChars <= 0 {
		maxChars = 30000
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
	for i := 0; i < fetchTop; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c, e := webFetchURL(ctx, cache, localAI, fetchURLs[idx], perResultChars)
			results[idx] = fetchResult{content: c, err: e}
		}(i)
	}
	wg.Wait()

	for i := 0; i < fetchTop; i++ {
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
