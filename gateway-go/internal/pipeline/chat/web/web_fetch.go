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
//   - web_html.go           — HTML → text (local AI, htmlmd fallback)
//   - web_html_preprocess.go — HTML noise stripping, metadata, signals, charset
//   - web_content.go        — Content dispatch, metadata type, output formatting
//   - web_fetch_stealth.go  — Browser profiles, bot-block evasion
//   - web_fetch_search.go   — Search providers (Serper, Brave, DuckDuckGo)
//   - web_fetch_rank.go     — search+fetch candidate ranking / usable fill
//   - fetch_cache.go        — In-memory result cache
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

// fetchGroup collapses duplicate in-flight URL fetches into a single request.
// When multiple goroutines (e.g. search+fetch, concurrent tool calls) request the
// same URL simultaneously, only one fetch executes and the result is shared.
var fetchGroup singleflight

// Tool returns the unified web tool handler (fetch + search + search+fetch).
// spill (optional) lets the YouTube path offload full transcripts to disk while
// returning only a summary to the conversation transcript.
func Tool(cache *FetchCache, localAI *LocalAIExtractor, spill tooldeps.SpilloverStore) toolport.ToolFunc {
	return func(ctx context.Context, input rawJSON) (string, error) {
		var p struct {
			URL       string   `json:"url"`
			Query     string   `json:"query"`
			Queries   []string `json:"queries"`
			Fetch     int      `json:"fetch"`
			MaxChars  int      `json:"maxChars"`
			Count     int      `json:"count"`
			Type      string   `json:"type"`
			Academic  bool     `json:"academic"`
			Summarize string   `json:"summarize"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			//nolint:nilerr // tool returns user-facing error in result string
			return formatFetchError(webFetchErr{
				Code: "invalid_params", Message: err.Error(), Retryable: false,
			}), nil
		}

		// Dispatch by mode.
		switch {
		case p.Summarize != "":
			// Summarize mode: Kagi Universal Summarizer on a URL (standalone).
			return webSummarize(ctx, p.Summarize)

		case p.URL != "":
			// Fetch mode: extract content from URL.
			return webFetchURL(ctx, cache, localAI, spill, p.URL, p.MaxChars)

		case len(p.Queries) > 0:
			// Parallel search mode: multiple queries at once.
			if len(p.Queries) > 5 {
				p.Queries = p.Queries[:5]
			}
			if p.Count <= 0 {
				p.Count = 5
			}
			if p.Type != "" && p.Type != "search" && p.Fetch > 0 {
				return formatFetchError(webFetchErr{
					Code: "invalid_params", Message: "typed search (news/scholar/autocomplete/fastgpt/enrich_web/enrich_news) is not compatible with fetch; use query + type without fetch", Retryable: false,
				}), nil
			}
			if p.Type != "" && p.Type != "search" {
				return webParallelSearchWithType(ctx, p.Queries, p.Type, p.Count)
			}
			fetch := p.Fetch
			if fetch > 3 {
				fetch = 3
			}
			return webParallelSearch(ctx, cache, localAI, spill, p.Queries, p.Count, fetch, p.MaxChars)

		case p.Query != "":
			if p.Count <= 0 {
				p.Count = 5
			}
			// Typed search (news/scholar/autocomplete) — Serper only, no fetch.
			if p.Type != "" && p.Type != "search" {
				if p.Fetch > 0 {
					return formatFetchError(webFetchErr{
						Code: "invalid_params", Message: "typed search (news/scholar/autocomplete/fastgpt/enrich_web/enrich_news) is not compatible with fetch; use query + type without fetch", Retryable: false,
					}), nil
				}
				return webSearchWithType(ctx, p.Type, p.Query, p.Count)
			}
			// Academic lane rides ALONGSIDE the main search (labeled append,
			// never rank fusion) — started first so it overlaps the search.
			laneCh := startAcademicLane(ctx, p.Query, p.Academic)
			var out string
			var err error
			if p.Fetch > 0 {
				// Search+fetch mode: search then auto-fetch top N.
				if p.Fetch > 3 {
					p.Fetch = 3
				}
				out, err = webSearchAndFetch(ctx, cache, localAI, spill, p.Query, p.Count, p.Fetch, p.MaxChars)
			} else {
				// Search-only mode: return search results.
				out, err = webSearch(ctx, p.Query, p.Count)
			}
			if err != nil {
				return out, err
			}
			if lane := joinAcademicLane(laneCh); lane != "" {
				out += "\n\n" + lane
			}
			return out, nil

		default:
			return formatFetchError(webFetchErr{
				Code: "missing_params", Message: "either url, query, or queries is required", Retryable: false,
			}), nil
		}
	}
}

// webFetchURL fetches a URL and returns extracted content with metadata envelope.
func webFetchURL(ctx context.Context, cache *FetchCache, localAI *LocalAIExtractor, spill tooldeps.SpilloverStore, targetURL string, maxChars int) (string, error) {
	out, err := webFetchURLDetailed(ctx, cache, localAI, spill, targetURL, maxChars)
	if err != nil {
		return "", err
	}
	return out.Content, nil
}

// webFetchURLDetailed is the search+fetch path: same fetch as webFetchURL but
// returns a structured usability verdict alongside the envelope.
func webFetchURLDetailed(ctx context.Context, cache *FetchCache, localAI *LocalAIExtractor, spill tooldeps.SpilloverStore, targetURL string, maxChars int) (fetchOutcome, error) {
	if maxChars <= 0 {
		maxChars = 20000
	}

	// YouTube → summarized transcript (full text offloaded to spillover).
	if media.IsYouTubeURL(targetURL) {
		content, err := fetchYouTube(ctx, spill, targetURL)
		if err != nil {
			return fetchOutcome{Assess: fetchUsability{HasError: true}}, err
		}
		return fetchOutcome{Content: content, Assess: assessFetchResult(content, nil)}, nil
	}

	// Reddit → JSON API (posts+comments, listings, search). The SPA HTML the
	// stealth fetcher would get is an empty JS shell — see web_reddit.go.
	if isRedditURL(targetURL) {
		content, err := fetchReddit(ctx, targetURL, maxChars)
		if err != nil {
			return fetchOutcome{Assess: fetchUsability{HasError: true}}, err
		}
		return fetchOutcome{Content: content, Assess: assessFetchResult(content, nil)}, nil
	}

	// X/Twitter single tweet → syndication endpoint (no-auth read). Search and
	// timelines are login-walled and not reachable — see web_x.go.
	if id, ok := isXStatusURL(targetURL); ok {
		content, err := fetchXTweet(ctx, targetURL, id, maxChars)
		if err != nil {
			return fetchOutcome{Assess: fetchUsability{HasError: true}}, err
		}
		return fetchOutcome{Content: content, Assess: assessFetchResult(content, nil)}, nil
	}

	// Cache hit — envelope only; fall back to parsing for assess.
	if cached, ok := cache.Get(targetURL); ok {
		slog.Info("web fetch", "url", targetURL, "cache_hit", true)
		truncated := applyTruncation(cached, maxChars)
		return fetchOutcome{Content: truncated, Assess: assessFetchResult(truncated, nil)}, nil
	}

	// Singleflight: collapse concurrent fetches for the same URL into one request.
	// The result is cached after the first fetch completes.
	v, err := fetchGroup.do(targetURL, func() (any, error) {
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
			slog.Info("web fetch",
				"url", targetURL, "provider", "stealth", "cache_hit", false,
				"fetch_ms", fetchMs, "error", err.Error())
			envelope := formatFetchError(classifyFetchError(err, targetURL))
			return fetchOutcome{Content: envelope, Assess: fetchUsability{HasError: true}}, nil
		}

		rawContent := normalizeCharset(result.Data, result.ContentType)
		origChars := len(rawContent)

		meta := webFetchMeta{
			URL: targetURL, FinalURL: result.FinalURL,
			ContentType: result.ContentType, StatusCode: result.StatusCode,
			FetchMs: fetchMs, Provider: "stealth", OrigChars: origChars,
		}

		extractStart := time.Now()
		content := processFetchedContent(ctx, rawContent, result.Data, result.ContentType, targetURL, localAI, &meta)
		meta.ExtractChars = len(content)

		if shouldEscalateThinContent(&meta) {
			if escContent, ok := escalateThinContent(ctx, targetURL, maxBytes, localAI, &meta); ok {
				content = escContent
			}
		}
		extractMs := time.Since(extractStart).Milliseconds()

		if origChars > 0 {
			meta.Retention = fmt.Sprintf("%.1f%%", float64(meta.ExtractChars)/float64(origChars)*100)
		} else {
			meta.Retention = "0%"
		}
		if meta.WordCount == 0 {
			meta.WordCount = estimateWordCount(content)
		}

		assess := assessMetaBody(meta.Signals, content)
		slog.Info("web fetch",
			"url", targetURL, "provider", meta.Provider, "cache_hit", false,
			"fetch_ms", fetchMs, "extract_ms", extractMs,
			"extract_chars", meta.ExtractChars, "signals", meta.Signals,
			"usable", assess.Usable, "thin", assess.Thin)

		fullResult := formatFetchResult(meta, content)
		cache.Put(targetURL, fullResult)
		return fetchOutcome{Content: fullResult, Assess: assess}, nil
	})
	if err != nil {
		return fetchOutcome{}, err
	}

	out, ok := v.(fetchOutcome)
	if !ok {
		return fetchOutcome{}, fmt.Errorf("web fetch %q: unexpected result type %T", targetURL, v)
	}
	out.Content = applyTruncation(out.Content, maxChars)
	return out, nil
}

// webFetchViaSerper extracts content for a single URL via Serper's dedicated
// scrape endpoint (scrape.serper.dev). Returns (fullResult, true) on success,
// or ("", false) to signal the caller should fall through to the raw HTTP
// fetcher (e.g. non-HTML URL, empty response, or API error).
//
// The returned result is already cached; the caller does not need to re-cache.
func webFetchViaSerper(ctx context.Context, cache *FetchCache, apiKey, targetURL string) (fetchOutcome, bool) {
	fetchStart := time.Now()
	scrape, err := serperScrape(ctx, apiKey, targetURL)
	fetchMs := time.Since(fetchStart).Milliseconds()
	if err != nil {
		return fetchOutcome{}, false
	}
	content := pickScrapeContent(scrape)
	if strings.TrimSpace(content) == "" {
		return fetchOutcome{}, false
	}

	origChars := len(content)
	meta := webFetchMeta{
		URL:          targetURL,
		ContentType:  "text/html",
		StatusCode:   200,
		FetchMs:      fetchMs,
		Provider:     "serper",
		OrigChars:    origChars,
		ExtractChars: origChars,
		Retention:    "100.0%",
		WordCount:    estimateWordCount(content),
		Signals:      []string{"serper_scrape"},
	}
	populateScrapeMetadata(&meta, scrape.Metadata)

	assess := assessMetaBody(meta.Signals, content)
	slog.Info("web fetch",
		"url", targetURL, "provider", "serper", "cache_hit", false,
		"fetch_ms", fetchMs, "extract_chars", origChars,
		"usable", assess.Usable, "thin", assess.Thin)

	fullResult := formatFetchResult(meta, content)
	cache.Put(targetURL, fullResult)
	return fetchOutcome{Content: fullResult, Assess: assess}, true
}

// webParallelSearch runs multiple search queries concurrently and returns
// combined results. Each query runs independently with optional fetch.
// This avoids sequential LLM round-trips for multi-constraint questions.
func webParallelSearch(ctx context.Context, cache *FetchCache, localAI *LocalAIExtractor, spill tooldeps.SpilloverStore, queries []string, count, fetch, maxChars int) (string, error) {
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
				content, err = webSearchAndFetch(ctx, cache, localAI, spill, query, count, fetch, perQueryChars)
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

// webSearchAndFetch searches the web and auto-fetches the top N usable pages.
// Candidates are ranked (answer-box, knowledge graph, query overlap, denylist),
// then filled with a parallel wave + sequential early-stop.
func webSearchAndFetch(ctx context.Context, cache *FetchCache, localAI *LocalAIExtractor, spill tooldeps.SpilloverStore, query string, count, fetchTop, maxChars int) (string, error) {
	if maxChars <= 0 {
		maxChars = 15000
	}
	if fetchTop <= 0 {
		fetchTop = 1
	}

	searchOutput, organic, answerLink, knowledgeLink, err := webSearchWithURLs(ctx, query, count)
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

	poolSize := fetchCandidatePoolSize(count, fetchTop)
	candidates, fstats := rankFetchCandidates(query, answerLink, knowledgeLink, organic, poolSize)
	slog.Info(
		"web search+fetch candidates",
		"query", query,
		"pool", len(candidates),
		"answer_link", answerLink != "",
		"knowledge_link", knowledgeLink != "",
		"denied_host", fstats.DeniedHost,
		"denied_youtube", fstats.DeniedYT,
		"denied_path", fstats.DeniedPath,
		"denied_media", fstats.DeniedMedia,
		"dup_host", fstats.DupHost,
		"dup_etld", fstats.DupETLD,
	)
	if len(candidates) == 0 {
		sb.WriteString("\n[Note: fetch requested but no fetchable URLs (provider=duckduckgo or filtered). " +
			"search+fetch needs Serper/Brave organic results; use web(url=...) for specific pages.]\n")
		return sb.String(), nil
	}

	perCandidateChars := maxChars / fetchTop
	if perCandidateChars <= 0 {
		perCandidateChars = maxChars
	}

	selected := fillUsableFetches(ctx, cache, localAI, spill, candidates, fetchTop, perCandidateChars, nil)
	if len(selected) == 0 {
		sb.WriteString(fmt.Sprintf(
			"\n[Note: filled 0 of %d; skipped thin/failed. Try web(url=...) on a specific result.]\n",
			fetchTop,
		))
		return sb.String(), nil
	}
	if len(selected) < fetchTop {
		fmt.Fprintf(&sb, "\n[Note: filled %d of %d; skipped thin/failed]\n\n", len(selected), fetchTop)
	}

	finalChars := maxChars / len(selected)
	if finalChars <= 0 {
		finalChars = maxChars
	}
	for i, item := range selected {
		fmt.Fprintf(&sb, "<fetched index=\"%d\" url=\"%s\">\n", i+1, item.url)
		sb.WriteString(applyTruncation(item.content, finalChars))
		sb.WriteString("\n</fetched>\n\n")
	}

	return sb.String(), nil
}
