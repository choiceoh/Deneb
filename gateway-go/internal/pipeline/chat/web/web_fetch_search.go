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
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// --- Provider dispatch ---

// Test seams (same pattern as jinaFetchFn): production defaults to the real
// providers; unit tests swap these to assert failure fallback without network.
var (
	serperSearchRawFn  = serperSearchRaw
	braveSearchRawFn   = braveSearchRaw
	duckDuckGoSearchFn = duckDuckGoSearch
)

// webSearch dispatches to the best available search provider.
// Priority: Kagi → Serper → Brave → DuckDuckGo. Missing keys skip a provider; a
// provider error also falls through to the next (sequential, not raced).
func webSearch(ctx context.Context, query string, count int) (string, error) {
	if key := kagiAPIKey(); key != "" {
		results, err := kagiSearchRawFn(ctx, key, query, count)
		if err == nil {
			return formatSearchResults(results), nil
		}
		slog.Info("web search fallback", "from", "kagi", "to", nextSearchProvider("kagi"), "error", err)
	}
	if key := serperAPIKey(); key != "" {
		results, answerBox, _, err := serperSearchRawFn(ctx, key, query, count)
		if err == nil {
			return formatSerperResults(results, answerBox), nil
		}
		slog.Info("web search fallback", "from", "serper", "to", nextSearchProvider("serper"), "error", err)
	}
	if key := braveAPIKey(); key != "" {
		results, err := braveSearchRawFn(ctx, key, query, count)
		if err == nil {
			return formatSearchResults(results), nil
		}
		slog.Info("web search fallback", "from", "brave", "to", "duckduckgo", "error", err)
	}
	return duckDuckGoSearchFn(ctx, query)
}

// webSearchWithURLs searches and returns formatted output, organic results, and
// optional Serper answer-box / knowledge-graph links for fetch ranking. Same
// Kagi→Serper→Brave→DuckDuckGo fallback as webSearch; Kagi and Brave carry no
// answer-box/knowledge links, and DuckDuckGo Instant Answer has no reliable
// organic URLs, so results may be empty.
func webSearchWithURLs(ctx context.Context, query string, count int) (output string, results []searchResult, answerLink, knowledgeLink string, err error) {
	if key := kagiAPIKey(); key != "" {
		organic, kerr := kagiSearchRawFn(ctx, key, query, count)
		if kerr == nil {
			return formatSearchResults(organic), organic, "", "", nil
		}
		slog.Info("web search fallback", "from", "kagi", "to", nextSearchProvider("kagi"), "error", kerr)
	}
	if key := serperAPIKey(); key != "" {
		organic, answerBox, kgLink, err := serperSearchRawFn(ctx, key, query, count)
		if err == nil {
			return formatSerperResults(organic, answerBox), organic, strings.TrimSpace(answerBox.Link), strings.TrimSpace(kgLink), nil
		}
		slog.Info("web search fallback", "from", "serper", "to", nextSearchProvider("serper"), "error", err)
	}
	if key := braveAPIKey(); key != "" {
		organic, err := braveSearchRawFn(ctx, key, query, count)
		if err == nil {
			return formatSearchResults(organic), organic, "", "", nil
		}
		slog.Info("web search fallback", "from", "brave", "to", "duckduckgo", "error", err)
	}
	result, err := duckDuckGoSearchFn(ctx, query)
	return result, nil, "", "", err
}

// nextSearchProvider names the provider webSearch will try after `from` fails,
// for fallback logs. Providers whose keys are absent are skipped.
func nextSearchProvider(from string) string {
	switch from {
	case "kagi":
		if serperAPIKey() != "" {
			return "serper"
		}
		if braveAPIKey() != "" {
			return "brave"
		}
		return "duckduckgo"
	case "serper":
		if braveAPIKey() != "" {
			return "brave"
		}
		return "duckduckgo"
	default:
		return "duckduckgo"
	}
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
	GL  string `json:"gl,omitempty"`
	HL  string `json:"hl,omitempty"`
}

// buildSerperRequest sets Korean locale when the query contains Hangul.
// ASCII-only queries keep global defaults (no gl/hl).
func buildSerperRequest(query string, count int) serperRequest {
	if count <= 0 {
		count = 5
	}
	req := serperRequest{Q: query, Num: count}
	if queryHasHangul(query) {
		req.GL = "kr"
		req.HL = "ko"
	}
	return req
}

func queryHasHangul(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 { // Hangul syllables
			return true
		}
		if r >= 0x1100 && r <= 0x11FF { // Hangul Jamo
			return true
		}
		if r >= 0x3130 && r <= 0x318F { // Hangul Compatibility Jamo
			return true
		}
	}
	return false
}

type serperAnswerBox struct {
	Title   string `json:"title"`
	Answer  string `json:"answer"`
	Snippet string `json:"snippet"`
	Link    string `json:"link"`
}

type serperKnowledgeGraph struct {
	Title       string `json:"title"`
	Website     string `json:"website"`
	WebsiteLink string `json:"websiteLink"`
	Description string `json:"description"`
}

func (k serperKnowledgeGraph) Link() string {
	if s := strings.TrimSpace(k.WebsiteLink); s != "" {
		return s
	}
	return strings.TrimSpace(k.Website)
}

type serperResponse struct {
	Organic        []searchResult       `json:"organic"`
	AnswerBox      serperAnswerBox      `json:"answerBox"`
	KnowledgeGraph serperKnowledgeGraph `json:"knowledgeGraph"`
}

// serperSearchRaw performs a POST /search request against Serper and returns
// organic results, answer box, and knowledge-graph website link (may be empty).
func serperSearchRaw(ctx context.Context, apiKey, query string, count int) ([]searchResult, serperAnswerBox, string, error) {
	body, err := json.Marshal(buildSerperRequest(query, count))
	if err != nil {
		return nil, serperAnswerBox{}, "", fmt.Errorf("marshal serper request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		"https://google.serper.dev/search", bytes.NewReader(body))
	if err != nil {
		return nil, serperAnswerBox{}, "", fmt.Errorf("create serper request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", apiKey)

	resp, err := SharedClient(20 * time.Second).Do(req)
	if err != nil {
		return nil, serperAnswerBox{}, "", fmt.Errorf("serper request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, serperAnswerBox{}, "", fmt.Errorf("serper HTTP %d", resp.StatusCode)
	}

	var result serperResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, serperAnswerBox{}, "", fmt.Errorf("parse serper response: %w", err)
	}
	return result.Organic, result.AnswerBox, result.KnowledgeGraph.Link(), nil
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

	// Fail-fast: a slow scrape burns the turn; fall through to stealth instead.
	const serperScrapeTimeout = 10 * time.Second
	reqCtx, cancel := context.WithTimeout(ctx, serperScrapeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		"https://scrape.serper.dev", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create serper scrape request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", apiKey)

	resp, err := SharedClient(serperScrapeTimeout).Do(req)
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

// buildBraveSearchURL builds the Brave web search URL. Hangul queries get
// country=KR and search_lang=ko; ASCII-only queries keep Brave defaults.
func buildBraveSearchURL(query string, count int) string {
	if count <= 0 {
		count = 5
	}
	v := url.Values{}
	v.Set("q", query)
	v.Set("count", fmt.Sprintf("%d", count))
	if queryHasHangul(query) {
		v.Set("country", "KR")
		v.Set("search_lang", "ko")
	}
	return "https://api.search.brave.com/res/v1/web/search?" + v.Encode()
}

func braveSearchRaw(ctx context.Context, apiKey, query string, count int) ([]searchResult, error) {
	reqURL := buildBraveSearchURL(query, count)

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

// --- Typed search (Serper-specific endpoints) ---

// serperTypedEndpoints maps search types to Serper endpoint URLs.
var serperTypedEndpoints = map[string]string{
	"news":         "https://google.serper.dev/news",
	"scholar":      "https://google.serper.dev/scholar",
	"autocomplete": "https://google.serper.dev/autocomplete",
}

// webSearchWithType dispatches to a specialised search endpoint.
// Serper types: news, scholar, autocomplete. Kagi types: fastgpt, enrich_web,
// enrich_news. Falls back to regular webSearch for unknown types.
func webSearchWithType(ctx context.Context, searchType, query string, count int) (string, error) {
	if isKagiSearchType(searchType) {
		return kagiTypedSearch(ctx, searchType, query, count)
	}
	endpoint, ok := serperTypedEndpoints[searchType]
	if !ok {
		return webSearch(ctx, query, count)
	}
	key := serperAPIKey()
	if key == "" {
		//nolint:nilerr
		return formatFetchError(webFetchErr{
			Code: "no_serper_key", Message: "news/scholar/autocomplete search requires SERPER_API_KEY", Retryable: false,
		}), nil
	}
	return serperTypedSearch(ctx, key, endpoint, searchType, query, count)
}

// webParallelSearchWithType runs typed search for multiple queries in parallel.
func webParallelSearchWithType(ctx context.Context, queries []string, searchType string, count int) (string, error) {
	if len(queries) == 1 {
		return webSearchWithType(ctx, searchType, queries[0], count)
	}
	if count <= 0 {
		count = 5
	}

	type queryResult struct {
		index   int
		query   string
		content string
		err     error
	}
	results := make([]queryResult, len(queries))
	var wg sync.WaitGroup
	for i, q := range queries {
		wg.Add(1)
		go func(i int, q string) {
			defer wg.Done()
			content, err := webSearchWithType(ctx, searchType, q, count)
			results[i] = queryResult{index: i, query: q, content: content, err: err}
		}(i, q)
	}
	wg.Wait()

	var sb strings.Builder
	for _, r := range results {
		fmt.Fprintf(&sb, "<query index=\"%d\" q=\"%s\">\n", r.index+1, r.query)
		if r.err != nil {
			fmt.Fprintf(&sb, "Search failed: %s\n", r.err.Error())
		} else {
			sb.WriteString(r.content)
		}
		sb.WriteString("</query>\n\n")
	}
	return sb.String(), nil
}

// serperTypedSearch calls a Serper specialised endpoint and formats the response.
func serperTypedSearch(ctx context.Context, apiKey, endpoint, searchType, query string, count int) (string, error) {
	payload := buildSerperRequest(query, count)
	reqBody := map[string]any{
		"q":   payload.Q,
		"num": payload.Num,
	}
	if payload.GL != "" {
		reqBody["gl"] = payload.GL
	}
	if payload.HL != "" {
		reqBody["hl"] = payload.HL
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal serper %s request: %w", searchType, err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create serper %s request: %w", searchType, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", apiKey)

	resp, err := SharedClient(20 * time.Second).Do(req)
	if err != nil {
		return "", fmt.Errorf("serper %s request failed: %w", searchType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("serper %s HTTP %d", searchType, resp.StatusCode)
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return "", fmt.Errorf("parse serper %s response: %w", searchType, err)
	}

	return formatSerperTypedResults(searchType, raw), nil
}

// formatSerperTypedResults formats typed Serper responses by type.
func formatSerperTypedResults(searchType string, raw map[string]json.RawMessage) string {
	var formatted string
	switch searchType {
	case "news":
		formatted = formatSerperNews(raw)
	case "scholar":
		formatted = formatSerperScholar(raw)
	case "autocomplete":
		formatted = formatSerperAutocomplete(raw)
	}
	if formatted == "" {
		return "No results found."
	}
	return formatted
}

func formatSerperNews(raw map[string]json.RawMessage) string {
	type newsItem struct {
		Title   string `json:"title"`
		Link    string `json:"link"`
		Snippet string `json:"snippet"`
		Date    string `json:"date"`
		Source  string `json:"source"`
	}
	var builder strings.Builder
	if knowledgeGraph, ok := raw["knowledgeGraph"]; ok {
		var knowledge struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		}
		if json.Unmarshal(knowledgeGraph, &knowledge) == nil && knowledge.Title != "" {
			fmt.Fprintf(&builder, "**%s**: %s\n\n", knowledge.Title, knowledge.Description)
		}
	}
	var items []newsItem
	if data, ok := raw["news"]; ok {
		_ = json.Unmarshal(data, &items)
	}
	for index, item := range items {
		fmt.Fprintf(&builder, "%d. **%s**\n", index+1, item.Title)
		writeSerperNewsMetadata(&builder, item.Source, item.Date)
		if item.Snippet != "" {
			fmt.Fprintf(&builder, "   %s\n", item.Snippet)
		}
		fmt.Fprintf(&builder, "   %s\n\n", item.Link)
	}
	return builder.String()
}

func writeSerperNewsMetadata(builder *strings.Builder, source, date string) {
	if source != "" {
		fmt.Fprintf(builder, "   Source: %s", source)
	}
	if date != "" {
		fmt.Fprintf(builder, " | %s", date)
	}
	builder.WriteString("\n")
}

func formatSerperScholar(raw map[string]json.RawMessage) string {
	type scholarItem struct {
		Title       string `json:"title"`
		Link        string `json:"link"`
		Snippet     string `json:"snippet"`
		Publication string `json:"publication"`
		Authors     string `json:"authors"`
		Year        string `json:"year"`
		CitedBy     struct {
			Total int    `json:"total"`
			Link  string `json:"link"`
		} `json:"citedBy"`
	}
	var items []scholarItem
	if data, ok := raw["organic"]; ok {
		_ = json.Unmarshal(data, &items)
	}
	var builder strings.Builder
	for index, item := range items {
		fmt.Fprintf(&builder, "%d. **%s**\n", index+1, item.Title)
		if item.Authors != "" {
			fmt.Fprintf(&builder, "   Authors: %s\n", item.Authors)
		}
		writeSerperScholarMetadata(&builder, item.Publication, item.Year, item.CitedBy.Total)
		if item.Snippet != "" {
			fmt.Fprintf(&builder, "   %s\n", item.Snippet)
		}
		fmt.Fprintf(&builder, "   %s\n\n", item.Link)
	}
	return builder.String()
}

func writeSerperScholarMetadata(builder *strings.Builder, publication, year string, citedBy int) {
	if publication != "" {
		fmt.Fprintf(builder, "   Publication: %s", publication)
	}
	if year != "" {
		fmt.Fprintf(builder, " (%s)", year)
	}
	if citedBy > 0 {
		fmt.Fprintf(builder, " | Cited by: %d", citedBy)
	}
	builder.WriteString("\n")
}

func formatSerperAutocomplete(raw map[string]json.RawMessage) string {
	var suggestions []string
	if data, ok := raw["1"]; ok {
		_ = json.Unmarshal(data, &suggestions)
	}
	var builder strings.Builder
	for index, suggestion := range suggestions {
		fmt.Fprintf(&builder, "%d. %s\n", index+1, suggestion)
	}
	questionsRaw, ok := raw["peopleAlsoAsk"]
	if !ok {
		return builder.String()
	}
	var questions []string
	if json.Unmarshal(questionsRaw, &questions) != nil || len(questions) == 0 {
		return builder.String()
	}
	builder.WriteString("\n**Related questions:**\n")
	for _, question := range questions {
		fmt.Fprintf(&builder, "- %s\n", question)
	}
	return builder.String()
}
