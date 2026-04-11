// web_fetch_search.go — Serper/Brave/DuckDuckGo providers for search + scrape.
//
// Search provider priority: Serper (Google) → Brave → DuckDuckGo.
// Scrape provider: Serper's dedicated `scrape.serper.dev` endpoint (called by
// web_fetch.go ahead of the raw HTTP fetcher when SERPER_API_KEY is set).
//
// Serper search returns Google organic results (title, link, snippet) plus an
// answer box when available. Serper scrape returns clean text/markdown plus
// head metadata for a single URL — cheaper and more reliable than stealth
// browser fetching for normal HTML pages.
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// --- Provider dispatch ---

// webSearch dispatches to the best available search provider.
// Priority: Serper → Brave → DuckDuckGo.
func webSearch(ctx context.Context, query string, count int) (string, error) {
	if key := serperAPIKey(); key != "" {
		return serperWebSearch(ctx, key, query, count)
	}
	if key := braveAPIKey(); key != "" {
		return braveWebSearch(ctx, key, query, count)
	}
	return duckDuckGoSearch(ctx, query)
}

// webSearchWithURLs searches and returns both formatted output and fetchable URLs.
// Used by search+fetch mode.
func webSearchWithURLs(ctx context.Context, query string, count int) (output string, urls []string, err error) {
	if key := serperAPIKey(); key != "" {
		results, answerBox, err := serperSearchRaw(ctx, key, query, count)
		if err != nil {
			return "", nil, err
		}
		var resultURLs []string
		for _, r := range results {
			resultURLs = append(resultURLs, r.URL)
		}
		return formatSerperResults(results, answerBox), resultURLs, nil
	}
	if key := braveAPIKey(); key != "" {
		results, err := braveSearchRaw(ctx, key, query, count)
		if err != nil {
			return "", nil, err
		}
		var resultURLs []string
		for _, r := range results {
			resultURLs = append(resultURLs, r.URL)
		}
		return formatSearchResults(results), resultURLs, nil
	}
	// DuckDuckGo: no reliable URLs for fetching.
	result, err := duckDuckGoSearch(ctx, query)
	return result, nil, err
}

func braveAPIKey() string {
	key := os.Getenv("BRAVE_SEARCH_API_KEY")
	if key == "" {
		key = os.Getenv("BRAVE_API_KEY")
	}
	return key
}

func serperAPIKey() string {
	return os.Getenv("SERPER_API_KEY")
}

// --- Serper (Google Search API) ---
//
// Serper (https://serper.dev) is a fast, cheap Google Search API.
// POST https://google.serper.dev/search with { "q": "...", "num": N }
// Auth: X-API-KEY header.
// Response: { "organic": [{title, link, snippet}], "answerBox": {...}, "knowledgeGraph": {...} }.

type serperRequest struct {
	Q   string `json:"q"`
	Num int    `json:"num,omitempty"`
}

type serperAnswerBox struct {
	Title   string `json:"title"`
	Answer  string `json:"answer"`
	Snippet string `json:"snippet"`
	Link    string `json:"link"`
}

type serperResponse struct {
	Organic   []searchResult  `json:"organic"`
	AnswerBox serperAnswerBox `json:"answerBox"`
}

// serperWebSearch performs a search via Serper and formats the output.
func serperWebSearch(ctx context.Context, apiKey, query string, count int) (string, error) {
	results, answerBox, err := serperSearchRaw(ctx, apiKey, query, count)
	if err != nil {
		//nolint:nilerr // tool returns user-facing error in result string
		return formatFetchError(webFetchErr{
			Code: "search_failed", Message: err.Error(), Retryable: true,
		}), nil
	}
	return formatSerperResults(results, answerBox), nil
}

// serperSearchRaw performs a POST /search request against Serper and returns
// the parsed organic results plus the answer box (which may be empty).
func serperSearchRaw(ctx context.Context, apiKey, query string, count int) ([]searchResult, serperAnswerBox, error) {
	if count <= 0 {
		count = 5
	}
	reqBody := serperRequest{Q: query, Num: count}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, serperAnswerBox{}, fmt.Errorf("marshal serper request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		"https://google.serper.dev/search", bytes.NewReader(body))
	if err != nil {
		return nil, serperAnswerBox{}, fmt.Errorf("create serper request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", apiKey)

	resp, err := SharedClient(20 * time.Second).Do(req)
	if err != nil {
		return nil, serperAnswerBox{}, fmt.Errorf("serper request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, serperAnswerBox{}, fmt.Errorf("serper HTTP %d", resp.StatusCode)
	}

	var result serperResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, serperAnswerBox{}, fmt.Errorf("parse serper response: %w", err)
	}
	return result.Organic, result.AnswerBox, nil
}

// formatSerperResults renders Serper output: optional answer box followed by
// the organic result list. Format parallels formatSearchResults so downstream
// consumers (AI agent, search+fetch) see consistent output across providers.
func formatSerperResults(results []searchResult, answerBox serperAnswerBox) string {
	var sb strings.Builder
	if ans := pickAnswer(answerBox); ans != "" {
		sb.WriteString("**Answer:** ")
		sb.WriteString(ans)
		if answerBox.Link != "" {
			fmt.Fprintf(&sb, "\nSource: %s", answerBox.Link)
		}
		sb.WriteString("\n\n")
	}
	if len(results) == 0 && sb.Len() == 0 {
		return "No results found."
	}
	for i, r := range results {
		fmt.Fprintf(&sb, "%d. **%s**\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Description)
	}
	return sb.String()
}

func pickAnswer(a serperAnswerBox) string {
	switch {
	case a.Answer != "":
		return a.Answer
	case a.Snippet != "":
		return a.Snippet
	default:
		return ""
	}
}

// --- Serper scrape (dedicated web-fetch endpoint) ---
//
// POST https://scrape.serper.dev with { "url": "...", "includeMarkdown": true }.
// Auth: X-API-KEY header.
// Response: { "text", "markdown", "metadata": { "title", "description", ...},
//             "jsonld": {...}, "credits": N }.

type serperScrapeRequest struct {
	URL             string `json:"url"`
	IncludeMarkdown bool   `json:"includeMarkdown"`
}

type serperScrapeResponse struct {
	Text     string            `json:"text"`
	Markdown string            `json:"markdown"`
	Metadata map[string]string `json:"metadata"`
	Credits  int               `json:"credits"`
}

// serperScrape calls Serper's scrape endpoint to extract clean text/markdown
// for a single URL. Returns the parsed response, or an error if the key is
// missing, the request fails, or the API returns non-200.
func serperScrape(ctx context.Context, apiKey, targetURL string) (*serperScrapeResponse, error) {
	reqBody := serperScrapeRequest{URL: targetURL, IncludeMarkdown: true}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal serper scrape request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		"https://scrape.serper.dev", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create serper scrape request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", apiKey)

	resp, err := SharedClient(30 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("serper scrape request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("serper scrape HTTP %d", resp.StatusCode)
	}

	var result serperScrapeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse serper scrape response: %w", err)
	}
	return &result, nil
}

// pickScrapeContent prefers markdown (structured), falls back to plain text.
func pickScrapeContent(s *serperScrapeResponse) string {
	if strings.TrimSpace(s.Markdown) != "" {
		return s.Markdown
	}
	return s.Text
}

// populateScrapeMetadata maps Serper's head metadata keys onto webFetchMeta.
// Keys follow common OpenGraph/HTML conventions (title, description, og:title, etc.).
func populateScrapeMetadata(meta *webFetchMeta, md map[string]string) {
	if len(md) == 0 {
		return
	}
	firstNonEmpty := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(md[k]); v != "" {
				return v
			}
		}
		return ""
	}
	meta.Title = firstNonEmpty("title", "og:title", "twitter:title")
	meta.Description = firstNonEmpty("description", "og:description", "twitter:description")
	meta.SiteName = firstNonEmpty("og:site_name", "application-name")
	meta.Language = firstNonEmpty("language", "og:locale")
	meta.Author = firstNonEmpty("author", "article:author")
	meta.Published = firstNonEmpty("article:published_time", "published_time", "date")
	meta.CanonicalURL = firstNonEmpty("canonical", "og:url")
	meta.OGType = firstNonEmpty("og:type")
}

// looksLikeBinaryURL returns true for URLs whose extension indicates a binary
// asset (PDF, Office doc, image, archive). Serper's scraper is HTML-only, so
// we skip it and fall through to the raw fetcher + liteparse path.
func looksLikeBinaryURL(u string) bool {
	lower := strings.ToLower(u)
	if i := strings.Index(lower, "?"); i >= 0 {
		lower = lower[:i]
	}
	if i := strings.Index(lower, "#"); i >= 0 {
		lower = lower[:i]
	}
	for _, ext := range []string{
		".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".zip", ".tar", ".gz", ".7z",
		".mp3", ".wav", ".ogg", ".flac",
		".mp4", ".mov", ".avi", ".webm",
		".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg",
	} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// --- Brave Search ---

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

// UnmarshalJSON lets searchResult handle both Brave's {title,url,description}
// and Serper's {title,link,snippet} shapes without a separate type.
func (r *searchResult) UnmarshalJSON(data []byte) error {
	var raw struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		Link        string `json:"link"`
		Description string `json:"description"`
		Snippet     string `json:"snippet"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Title = raw.Title
	r.URL = raw.URL
	if r.URL == "" {
		r.URL = raw.Link
	}
	r.Description = raw.Description
	if r.Description == "" {
		r.Description = raw.Snippet
	}
	return nil
}

func braveWebSearch(ctx context.Context, apiKey, query string, count int) (string, error) {
	results, err := braveSearchRaw(ctx, apiKey, query, count)
	if err != nil {
		//nolint:nilerr // tool returns user-facing error in result string
		return formatFetchError(webFetchErr{
			Code: "search_failed", Message: err.Error(), Retryable: true,
		}), nil
	}
	return formatSearchResults(results), nil
}

func braveSearchRaw(ctx context.Context, apiKey, query string, count int) ([]searchResult, error) {
	reqURL := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(query), count)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	resp, err := SharedClient(15 * time.Second).Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave search HTTP %d", resp.StatusCode)
	}

	var result braveSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse brave response: %w", err)
	}
	return result.Web.Results, nil
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

// --- DuckDuckGo (zero-config fallback) ---

func duckDuckGoSearch(ctx context.Context, query string) (string, error) {
	reqURL := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&no_html=1&skip_disambig=1",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", chromeProfile.headers["User-Agent"])

	resp, err := SharedClient(10 * time.Second).Do(req)
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
