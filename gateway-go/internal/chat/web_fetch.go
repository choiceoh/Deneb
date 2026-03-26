// web_fetch.go — Unified web tool: search, fetch, and search+fetch in one.
//
// Three modes via parameter dispatch:
//   {"url": "..."}                        → Fetch mode (extract content from URL)
//   {"query": "..."}                      → Search mode (web search results)
//   {"query": "...", "fetch": N}          → Search+fetch (search then auto-fetch top N)
//
// Designed for AI agent consumption with structured metadata, machine-readable
// errors, aggressive noise removal, SGLang AI extraction, and bot-block evasion.
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/media"
)

// --- Tool schema ---

func webToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "URL to fetch and extract content from",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Web search query (uses Brave Search or DuckDuckGo)",
			},
			"fetch": map[string]any{
				"type":        "number",
				"description": "When used with query: auto-fetch top N search results (1-3, default: 0 = search only)",
			},
			"maxChars": map[string]any{
				"type":        "number",
				"description": "Maximum content characters per result (default: 50000)",
			},
			"count": map[string]any{
				"type":        "number",
				"description": "Number of search results (default: 5)",
			},
		},
	}
}

// --- Structured output types ---

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

// webFetchErr is a machine-readable error for agent consumption.
type webFetchErr struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	URL       string `json:"url"`
	Retryable bool   `json:"retryable"`
}

// --- Unified tool implementation ---

func toolWeb(cache *FetchCache, sglang *sglangExtractor) ToolFunc {
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
			return webFetchURL(ctx, cache, sglang, p.URL, p.MaxChars)

		case p.Query != "":
			if p.Count <= 0 {
				p.Count = 5
			}
			if p.Fetch > 0 {
				// Search+fetch mode: search then auto-fetch top N.
				if p.Fetch > 3 {
					p.Fetch = 3
				}
				return webSearchAndFetch(ctx, cache, sglang, p.Query, p.Count, p.Fetch, p.MaxChars)
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

// --- Fetch mode ---

func webFetchURL(ctx context.Context, cache *FetchCache, sglang *sglangExtractor, targetURL string, maxChars int) (string, error) {
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

	// Size limit.
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

	isHTML := strings.Contains(result.ContentType, "text/html") ||
		strings.Contains(result.ContentType, "application/xhtml")
	isJSON := strings.Contains(result.ContentType, "application/json") ||
		strings.Contains(result.ContentType, "+json")

	var content string
	switch {
	case isHTML:
		content = processHTML(ctx, rawContent, targetURL, sglang, &meta)
	case isJSON:
		content = processJSON(rawContent)
	default:
		content = rawContent
	}

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

	return applyTruncation(fullResult, maxChars), nil
}

// --- Search mode ---

// webSearch dispatches to the best available search provider.
// Priority: Perplexity (sonar) → Brave → DuckDuckGo.
func webSearch(ctx context.Context, query string, count int) (string, error) {
	pplxKey := os.Getenv("PERPLEXITY_API_KEY")
	if pplxKey != "" {
		return perplexitySearch(ctx, pplxKey, query)
	}
	braveKey := os.Getenv("BRAVE_SEARCH_API_KEY")
	if braveKey == "" {
		braveKey = os.Getenv("BRAVE_API_KEY")
	}
	if braveKey != "" {
		return braveWebSearch(ctx, braveKey, query, count)
	}
	return duckDuckGoSearch(ctx, query)
}

// --- Perplexity Sonar search ---

// perplexitySearch uses Perplexity's sonar model for web search.
// Returns an AI-synthesized answer with citations — ideal for agent consumption
// because the answer is already structured and noise-free.
func perplexitySearch(ctx context.Context, apiKey, query string) (string, error) {
	reqBody := map[string]any{
		"model": "sonar",
		"messages": []map[string]string{
			{"role": "user", "content": query},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal perplexity request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", "https://api.perplexity.ai/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create perplexity request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("perplexity request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return formatFetchError(webFetchErr{
			Code:      "search_http_" + strconv.Itoa(resp.StatusCode),
			Message:   fmt.Sprintf("Perplexity returned HTTP %d: %s", resp.StatusCode, string(respBody)),
			Retryable: resp.StatusCode >= 500,
		}), nil
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Citations []string `json:"citations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse perplexity response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "No results from Perplexity.", nil
	}

	var sb strings.Builder
	sb.WriteString(result.Choices[0].Message.Content)

	// Append citations as numbered sources.
	if len(result.Citations) > 0 {
		sb.WriteString("\n\n**Sources:**\n")
		for i, cite := range result.Citations {
			fmt.Fprintf(&sb, "[%d] %s\n", i+1, cite)
		}
	}

	return sb.String(), nil
}

func braveWebSearch(ctx context.Context, apiKey, query string, count int) (string, error) {
	reqURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(query), count)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("brave search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return formatFetchError(webFetchErr{
			Code: "search_http_" + strconv.Itoa(resp.StatusCode),
			Message: fmt.Sprintf("Brave Search returned HTTP %d", resp.StatusCode),
			Retryable: resp.StatusCode >= 500,
		}), nil
	}

	var result braveSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse brave response: %w", err)
	}

	return formatSearchResults(result.Web.Results), nil
}

type braveSearchResult struct {
	Web struct {
		Results []searchResult `json:"results"`
	} `json:"web"`
}

type searchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

func duckDuckGoSearch(ctx context.Context, query string) (string, error) {
	reqURL := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&no_html=1&skip_disambig=1",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", chromeProfile.headers["User-Agent"])

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("duckduckgo search failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Abstract      string `json:"Abstract"`
		AbstractURL   string `json:"AbstractURL"`
		RelatedTopics []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"RelatedTopics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse duckduckgo response: %w", err)
	}

	var sb strings.Builder
	if result.Abstract != "" {
		fmt.Fprintf(&sb, "**Summary:** %s\nSource: %s\n\n", result.Abstract, result.AbstractURL)
	}
	for i, topic := range result.RelatedTopics {
		if i >= 5 || topic.Text == "" {
			break
		}
		fmt.Fprintf(&sb, "- %s\n  %s\n", topic.Text, topic.FirstURL)
	}
	if sb.Len() == 0 {
		return "No results found for this query.", nil
	}
	return sb.String(), nil
}

func formatSearchResults(results []searchResult) string {
	if len(results) == 0 {
		return "No results found."
	}
	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. **%s**\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Description)
	}
	return sb.String()
}

// --- Search+fetch mode ---

// webSearchAndFetch searches the web and auto-fetches the top N results.
// Uses Perplexity citations or Brave results as fetch targets.
func webSearchAndFetch(ctx context.Context, cache *FetchCache, sglang *sglangExtractor, query string, count, fetchTop, maxChars int) (string, error) {
	if maxChars <= 0 {
		maxChars = 30000 // lower default for multi-fetch to fit in context
	}

	// Step 1: Search and collect URLs to fetch.
	var searchOutput string
	var fetchURLs []string

	pplxKey := os.Getenv("PERPLEXITY_API_KEY")
	braveKey := os.Getenv("BRAVE_SEARCH_API_KEY")
	if braveKey == "" {
		braveKey = os.Getenv("BRAVE_API_KEY")
	}

	switch {
	case pplxKey != "":
		// Perplexity: get answer + citations as fetch targets.
		answer, citations, err := perplexitySearchRaw(ctx, pplxKey, query)
		if err != nil {
			return "", err
		}
		searchOutput = answer
		fetchURLs = citations

	case braveKey != "":
		// Brave: get search results as fetch targets.
		results, err := braveSearchRaw(ctx, braveKey, query, count)
		if err != nil {
			return "", err
		}
		searchOutput = formatSearchResults(results)
		for _, r := range results {
			fetchURLs = append(fetchURLs, r.URL)
		}

	default:
		// DuckDuckGo: no good URLs for fetching.
		return duckDuckGoSearch(ctx, query)
	}

	if searchOutput == "" {
		return "No results found.", nil
	}

	// Step 2: Build output with search summary.
	var sb strings.Builder
	sb.WriteString("<search_results query=\"" + query + "\">\n")
	sb.WriteString(searchOutput)
	sb.WriteString("\n</search_results>\n\n")

	// Step 3: Auto-fetch top N URLs.
	if fetchTop > len(fetchURLs) {
		fetchTop = len(fetchURLs)
	}
	if fetchTop == 0 {
		return sb.String(), nil
	}

	perResultChars := maxChars / fetchTop
	for i := 0; i < fetchTop; i++ {
		fetchURL := fetchURLs[i]
		fmt.Fprintf(&sb, "<fetched index=\"%d\" url=\"%s\">\n", i+1, fetchURL)

		content, err := webFetchURL(ctx, cache, sglang, fetchURL, perResultChars)
		if err != nil {
			fmt.Fprintf(&sb, "Fetch failed: %s\n", err.Error())
		} else {
			sb.WriteString(content)
		}
		sb.WriteString("\n</fetched>\n\n")
	}

	return sb.String(), nil
}

// perplexitySearchRaw returns the answer text and citation URLs separately.
func perplexitySearchRaw(ctx context.Context, apiKey, query string) (answer string, citations []string, err error) {
	reqBody := map[string]any{
		"model": "sonar",
		"messages": []map[string]string{
			{"role": "user", "content": query},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", "https://api.perplexity.ai/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("perplexity request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", nil, fmt.Errorf("Perplexity HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Citations []string `json:"citations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil, fmt.Errorf("parse perplexity response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", nil, nil
	}

	return result.Choices[0].Message.Content, result.Citations, nil
}

// braveSearchRaw returns raw search results for use in search+fetch mode.
func braveSearchRaw(ctx context.Context, apiKey, query string, count int) ([]searchResult, error) {
	reqURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(query), count)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Brave Search HTTP %d", resp.StatusCode)
	}

	var result braveSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse brave response: %w", err)
	}
	return result.Web.Results, nil
}

// --- Content processing by type ---

// processHTML runs the full HTML extraction pipeline:
// 1. Extract metadata from raw HTML
// 2. Detect quality signals
// 3. Strip noise elements (nav, aside, footer, ads, cookie banners)
// 4. Convert to Markdown (SGLang AI or FFI fallback)
func processHTML(ctx context.Context, html string, url string, sglang *sglangExtractor, meta *webFetchMeta) string {
	// Step 1: Extract metadata from raw HTML (before any stripping).
	extractHTMLMeta(html, meta)

	// Step 2: Detect quality signals from raw HTML.
	meta.Signals = detectSignals(html)

	// Step 3: Strip noise elements — the critical preprocessing step.
	// This removes nav, aside, footer, ads, cookie banners, comments, etc.
	// Even when SGLang is available, pre-stripping reduces input tokens
	// and prevents noise from confusing the AI extraction.
	cleaned := stripNoiseElements(html)

	// Step 4: Convert to Markdown.
	var content string
	if sglang.available() {
		extracted, err := sglang.extract(ctx, cleaned, url, meta.Language)
		if err != nil {
			slog.Warn("sglang extraction failed, falling back to FFI",
				"url", url, "error", err)
			content = ffiConvert(cleaned)
		} else {
			content = extracted
		}
	} else {
		content = ffiConvert(cleaned)
	}

	// Step 5: Post-extraction quality check.
	trimmedLen := len(strings.TrimSpace(content))
	if trimmedLen < 100 && meta.OrigChars > 1000 {
		meta.Signals = appendUnique(meta.Signals, "low_content_yield")
	}

	return content
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

func fetchYouTube(ctx context.Context, url string) (string, error) {
	ytCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	result, err := media.ExtractYouTubeTranscript(ytCtx, url)
	if err != nil {
		return formatFetchError(webFetchErr{
			Code: "youtube_failed", Message: err.Error(),
			URL: url, Retryable: true,
		}), nil
	}
	return media.FormatYouTubeResult(result), nil
}

// --- Fetch with stealth ---

// fetchWithRetry fetches a URL using browser-like stealth profiles.
// Delegates to stealthFetch which handles bot-block detection and escalation.
func fetchWithRetry(ctx context.Context, url string, maxBytes int64) (*media.FetchResult, error) {
	return stealthFetch(ctx, url, maxBytes)
}

func isRetryableError(err error) bool {
	var mfe *media.MediaFetchError
	if errors.As(err, &mfe) {
		if mfe.Code == media.ErrHTTPError && mfe.Status >= 500 {
			return true
		}
		if mfe.Code == media.ErrFetchFailed {
			return true
		}
		return false
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// --- FFI HTML→Markdown conversion ---

func ffiConvert(html string) string {
	text, _, err := ffi.HtmlToMarkdown(html)
	if err != nil {
		slog.Warn("ffi html-to-markdown failed", "error", err)
		return html
	}
	return text
}

// --- SGLang AI-powered content extraction ---

type sglangExtractor struct {
	mu      sync.Mutex
	client  *http.Client
	baseURL string
	apiKey  string
	model   string
	state   int // 0=unknown, 1=available, -1=unavailable
	probeAt time.Time
}

const (
	sglangUnknown     = 0
	sglangAvailable   = 1
	sglangUnavailable = -1
	// Re-probe interval when previously unavailable.
	sglangReprobeInterval = 5 * time.Minute
)

func newSGLangExtractor() *sglangExtractor {
	baseURL := os.Getenv("SGLANG_BASE_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:30000/v1"
	}
	apiKey := os.Getenv("SGLANG_API_KEY")
	if apiKey == "" {
		apiKey = "local"
	}
	model := os.Getenv("SGLANG_MODEL")
	if model == "" {
		model = "Qwen/Qwen3.5-35B-A3B"
	}
	return &sglangExtractor{
		client:  &http.Client{Timeout: 60 * time.Second},
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
	}
}

// available checks if SGLang is reachable. Probes on first call,
// then re-probes periodically if previously unavailable.
func (s *sglangExtractor) available() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == sglangAvailable {
		return true
	}
	if s.state == sglangUnavailable && time.Since(s.probeAt) < sglangReprobeInterval {
		return false
	}

	// Probe the server.
	s.probeAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+"/models", nil)
	if err != nil {
		s.state = sglangUnavailable
		return false
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		slog.Info("sglang not available", "url", s.baseURL, "error", err)
		s.state = sglangUnavailable
		return false
	}
	resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		s.state = sglangAvailable
		slog.Info("sglang available", "url", s.baseURL, "model", s.model)
		return true
	}
	s.state = sglangUnavailable
	return false
}

const sglangSystemPrompt = `You are a precision web content extractor for AI agents. Your output becomes the agent's sole understanding of the webpage.

REMOVE completely:
- Navigation menus, breadcrumbs, pagination elements
- Cookie banners, GDPR notices, consent dialogs
- Advertisement blocks, sponsored content, promotional banners
- "Related articles", "You might also like", "Trending" sections
- Comment sections, user reviews (unless they ARE the main content)
- Social media share buttons, follow widgets
- Site-wide headers, footers, copyright notices
- Search bars, login forms, newsletter signup forms
- Sidebar widgets, tag clouds, archive links

PRESERVE with structure:
- Main article/page body text — this is the primary output
- Headings hierarchy (# through ######) exactly as structured
- Data tables as proper markdown tables with alignment
- Code blocks with language tags (` + "```" + `lang ... ` + "```" + `)
- Ordered and unordered lists with proper nesting
- Blockquotes with > prefix
- Image references as ![alt](url) when informational
- Inline links [text](url) when they add value

RULES:
- Output ONLY the extracted content — no wrapping, no commentary
- Preserve the source language exactly
- If content is already clean, return it unchanged
- Empty extraction is better than including noise`

// extract calls SGLang for intelligent content extraction from pre-cleaned HTML.
func (s *sglangExtractor) extract(ctx context.Context, html string, url string, language string) (string, error) {
	// Convert HTML to markdown via FFI first to reduce token count.
	mdContent := ffiConvert(html)

	// Small content: AI adds little value, return directly.
	if len(mdContent) < 2000 {
		return mdContent, nil
	}

	// Cap input to ~100K chars (well within Qwen 262K context).
	if len(mdContent) > 100000 {
		mdContent = mdContent[:100000]
	}

	// Build user message with context hints.
	var userMsg strings.Builder
	fmt.Fprintf(&userMsg, "URL: %s\n", url)
	if language != "" {
		fmt.Fprintf(&userMsg, "Language: %s\n", language)
	}
	userMsg.WriteString("\n---\n")
	userMsg.WriteString(mdContent)

	reqBody := map[string]any{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "system", "content": sglangSystemPrompt},
			{"role": "user", "content": userMsg.String()},
		},
		"max_tokens":  16384,
		"temperature": 0,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal sglang request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", s.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create sglang request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sglang request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("sglang HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode sglang response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("sglang returned no choices")
	}

	extracted := result.Choices[0].Message.Content
	extracted = stripThinkingTags(extracted)

	return strings.TrimSpace(extracted), nil
}

var thinkingTagRe = regexp.MustCompile(`(?s)<think>.*?</think>\s*`)

func stripThinkingTags(s string) string {
	return thinkingTagRe.ReplaceAllString(s, "")
}

// --- Error classification ---

func classifyFetchError(err error, url string) webFetchErr {
	var mfe *media.MediaFetchError
	if errors.As(err, &mfe) {
		switch mfe.Code {
		case media.ErrHTTPError:
			return webFetchErr{
				Code:      "http_" + strconv.Itoa(mfe.Status),
				Message:   mfe.Message,
				URL:       url,
				Retryable: mfe.Status >= 500,
			}
		case media.ErrMaxBytes:
			return webFetchErr{
				Code: "content_too_large", Message: mfe.Message,
				URL: url, Retryable: false,
			}
		case media.ErrFetchFailed:
			code := "fetch_failed"
			msg := mfe.Message
			retryable := true
			switch {
			case strings.Contains(msg, "SSRF"):
				code, retryable = "ssrf_blocked", false
			case strings.Contains(msg, "no such host") || strings.Contains(msg, "no addresses"):
				code, retryable = "dns_failure", false
			case strings.Contains(msg, "too many redirects"):
				code, retryable = "redirect_loop", false
			case strings.Contains(msg, "certificate"):
				code, retryable = "tls_error", false
			case strings.Contains(msg, "connection refused"):
				code, retryable = "connection_refused", true
			case strings.Contains(msg, "connection reset"):
				code, retryable = "connection_reset", true
			}
			return webFetchErr{Code: code, Message: msg, URL: url, Retryable: retryable}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return webFetchErr{Code: "timeout", Message: "request timed out", URL: url, Retryable: true}
	}
	if errors.Is(err, context.Canceled) {
		return webFetchErr{Code: "canceled", Message: "request canceled", URL: url, Retryable: false}
	}
	return webFetchErr{Code: "unknown", Message: err.Error(), URL: url, Retryable: false}
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

// applyTruncation truncates a formatted result preserving metadata section
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

// --- Helpers ---

func appendUnique(ss []string, s string) []string {
	for _, existing := range ss {
		if existing == s {
			return ss
		}
	}
	return append(ss, s)
}

// estimateWordCount estimates word count from text content.
// Uses a simple split on whitespace, which works for both Latin and CJK text.
func estimateWordCount(text string) int {
	fields := strings.Fields(text)
	return len(fields)
}
