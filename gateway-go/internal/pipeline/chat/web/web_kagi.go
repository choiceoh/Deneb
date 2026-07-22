// web_kagi.go — Kagi API providers: Search, FastGPT, Summarizer, Enrichment.
//
// Kagi (https://kagi.com) exposes a small v0 API surface, all under
// https://kagi.com/api/v0 with `Authorization: Bot <TOKEN>` auth and one shared
// token (KAGI_API_KEY / KAGI_API_TOKEN):
//
//   - Search      GET  /search?q=...&limit=N  — premium web search. Slots in as
//     the highest-priority provider ahead of Serper when the key is
//     set (webSearch / webSearchWithURLs).
//   - FastGPT     POST /fastgpt               — LLM answer with inline citations
//     plus a reference list (web `type: "fastgpt"`).
//   - Summarizer  POST /summarize             — Universal Summarizer for a URL
//     (pages, PDFs, YouTube/video, audio) (web `summarize`).
//   - Enrichment  GET  /enrich/web|news?q=... — Teclis (web) / TinyGem (news)
//     non-commercial supplementary index
//     (web `type: "enrich_web" | "enrich_news"`).
//
// Missing key: the search provider silently falls through to Serper; the typed
// (fastgpt/enrich) and summarize modes return a user-facing error string.
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

const kagiAPIBase = "https://kagi.com/api/v0"

// kagiSearchRawFn is a test seam (same pattern as serperSearchRawFn): the
// provider-chain path swaps it to assert fallback without network.
var kagiSearchRawFn = kagiSearchRaw

func kagiAPIKey() string {
	key := os.Getenv("KAGI_API_KEY")
	if key == "" {
		key = os.Getenv("KAGI_API_TOKEN")
	}
	return key
}

// isKagiSearchType reports whether a web-tool `type` routes to a Kagi endpoint
// rather than a Serper one.
func isKagiSearchType(searchType string) bool {
	switch searchType {
	case "fastgpt", "enrich_web", "enrich_news":
		return true
	default:
		return false
	}
}

// --- Shared request plumbing + error envelope ---

// kagiError is Kagi's uniform error item. Responses can return HTTP 200 with a
// non-empty top-level `error` array, so every decode also checks that field.
type kagiError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Ref  string `json:"ref"`
}

func kagiErrOr(errs []kagiError, fallback string) error {
	if len(errs) == 0 {
		return fmt.Errorf("%s", fallback)
	}
	e := errs[0]
	if e.Msg == "" {
		return fmt.Errorf("kagi error %d", e.Code)
	}
	return fmt.Errorf("kagi error %d: %s", e.Code, e.Msg)
}

func kagiGet(ctx context.Context, apiKey, endpoint string, timeout time.Duration, out any) error {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return fmt.Errorf("create kagi request: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := SharedClient(timeout).Do(req)
	if err != nil {
		return fmt.Errorf("kagi request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("kagi HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("parse kagi response: %w", err)
	}
	return nil
}

func kagiPostJSON(ctx context.Context, apiKey, endpoint string, reqBody any, timeout time.Duration, out any) error {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal kagi request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create kagi request: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := SharedClient(timeout).Do(req)
	if err != nil {
		return fmt.Errorf("kagi request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("kagi HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("parse kagi response: %w", err)
	}
	return nil
}

// --- Search (provider chain) ---
//
// GET /search?q=...&limit=N → { "data": [ {t,url,title,snippet,published}, ... ] }.
// Rows carry a discriminator `t`: 0 = a search result, 1 = a related-searches
// list. We keep only t==0 rows and map them onto the shared searchResult shape,
// so formatSearchResults renders Kagi identically to Brave.

type kagiSearchItem struct {
	T         int    `json:"t"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Snippet   string `json:"snippet"`
	Published string `json:"published"`
}

type kagiSearchResponse struct {
	Data  []kagiSearchItem `json:"data"`
	Error []kagiError      `json:"error"`
}

// kagiSearchRaw performs a GET /search and returns organic results (t==0),
// capped at count.
func kagiSearchRaw(ctx context.Context, apiKey, query string, count int) ([]searchResult, error) {
	if count <= 0 {
		count = 5
	}
	v := url.Values{}
	v.Set("q", query)
	v.Set("limit", fmt.Sprintf("%d", count))
	endpoint := kagiAPIBase + "/search?" + v.Encode()

	var resp kagiSearchResponse
	if err := kagiGet(ctx, apiKey, endpoint, 15*time.Second, &resp); err != nil {
		return nil, err
	}
	if len(resp.Error) > 0 {
		return nil, kagiErrOr(resp.Error, "kagi search failed")
	}
	return kagiItemsToResults(resp.Data, count), nil
}

// kagiItemsToResults keeps only search-result rows (t==0) with a URL and maps
// them to the shared searchResult shape, capped at count (count<=0 = no cap).
func kagiItemsToResults(items []kagiSearchItem, count int) []searchResult {
	results := make([]searchResult, 0, len(items))
	for _, item := range items {
		if item.T != 0 || item.URL == "" {
			continue
		}
		results = append(results, searchResult{
			Title:       item.Title,
			URL:         item.URL,
			Description: item.Snippet,
		})
		if count > 0 && len(results) >= count {
			break
		}
	}
	return results
}

// --- Typed search dispatch (FastGPT / Enrichment) ---

// kagiTypedSearch handles the Kagi-backed web-tool `type` values. The key is
// checked once here; a missing key returns a user-facing error string (nil
// Go error) mirroring webSearchWithType's no_serper_key path.
func kagiTypedSearch(ctx context.Context, searchType, query string, count int) (string, error) {
	key := kagiAPIKey()
	if key == "" {
		//nolint:nilerr // tool surfaces the reason in the result string
		return formatFetchError(webFetchErr{
			Code:      "no_kagi_key",
			Message:   "fastgpt / enrich_web / enrich_news search requires KAGI_API_KEY",
			Retryable: false,
		}), nil
	}
	switch searchType {
	case "fastgpt":
		return kagiFastGPT(ctx, key, query)
	case "enrich_web":
		return kagiEnrich(ctx, key, "web", query, count)
	case "enrich_news":
		return kagiEnrich(ctx, key, "news", query, count)
	default:
		return webSearch(ctx, query, count)
	}
}

// --- FastGPT ---
//
// POST /fastgpt { query, web_search:true, cache:true } →
// { "data": { output, tokens, references:[{title,snippet,url}] } }.

type kagiFastGPTReference struct {
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	URL     string `json:"url"`
}

type kagiFastGPTResponse struct {
	Data struct {
		Output     string                 `json:"output"`
		Tokens     int                    `json:"tokens"`
		References []kagiFastGPTReference `json:"references"`
	} `json:"data"`
	Error []kagiError `json:"error"`
}

func kagiFastGPT(ctx context.Context, apiKey, query string) (string, error) {
	reqBody := map[string]any{
		"query":      query,
		"cache":      true,
		"web_search": true,
	}
	var resp kagiFastGPTResponse
	if err := kagiPostJSON(ctx, apiKey, kagiAPIBase+"/fastgpt", reqBody, 30*time.Second, &resp); err != nil {
		return "", err
	}
	if len(resp.Error) > 0 {
		return "", kagiErrOr(resp.Error, "kagi fastgpt failed")
	}
	return formatKagiFastGPT(resp.Data.Output, resp.Data.References), nil
}

func formatKagiFastGPT(output string, refs []kagiFastGPTReference) string {
	out := strings.TrimSpace(output)
	if out == "" && len(refs) == 0 {
		return "No answer found."
	}
	var sb strings.Builder
	if out != "" {
		sb.WriteString("**Answer:** ")
		sb.WriteString(out)
		sb.WriteString("\n")
	}
	if len(refs) > 0 {
		sb.WriteString("\n**References:**\n")
		for i, r := range refs {
			fmt.Fprintf(&sb, "%d. **%s**\n   %s\n", i+1, strings.TrimSpace(r.Title), strings.TrimSpace(r.URL))
			if s := strings.TrimSpace(r.Snippet); s != "" {
				fmt.Fprintf(&sb, "   %s\n", s)
			}
		}
	}
	return sb.String()
}

// --- Enrichment (Teclis web / TinyGem news) ---
//
// GET /enrich/{web|news}?q=... → same { data:[{t,url,title,snippet}] } shape as
// /search, so it reuses kagiSearchResponse + kagiItemsToResults.

func kagiEnrich(ctx context.Context, apiKey, kind, query string, count int) (string, error) {
	v := url.Values{}
	v.Set("q", query)
	endpoint := kagiAPIBase + "/enrich/" + kind + "?" + v.Encode()

	var resp kagiSearchResponse
	if err := kagiGet(ctx, apiKey, endpoint, 15*time.Second, &resp); err != nil {
		return "", err
	}
	if len(resp.Error) > 0 {
		return "", kagiErrOr(resp.Error, "kagi enrich failed")
	}
	return formatSearchResults(kagiItemsToResults(resp.Data, count)), nil
}

// --- Universal Summarizer ---
//
// POST /summarize { url, summary_type:"summary" } → { "data": { output, tokens } }.
// Kagi fetches and summarizes server-side (web pages, PDFs, YouTube/video,
// audio), so there is no local SSRF surface. Long media can take a while — hence
// the generous timeout.

type kagiSummarizeResponse struct {
	Data struct {
		Output string `json:"output"`
		Tokens int    `json:"tokens"`
	} `json:"data"`
	Error []kagiError `json:"error"`
}

// webSummarize runs the Kagi Universal Summarizer on target (a URL). Errors are
// returned as user-facing strings (nil Go error) so the agent sees the reason.
func webSummarize(ctx context.Context, target string) (string, error) {
	key := kagiAPIKey()
	if key == "" {
		//nolint:nilerr // tool surfaces the reason in the result string
		return formatFetchError(webFetchErr{
			Code:      "no_kagi_key",
			Message:   "summarize requires KAGI_API_KEY",
			Retryable: false,
		}), nil
	}
	reqBody := map[string]any{
		"url":          target,
		"summary_type": "summary",
	}
	var resp kagiSummarizeResponse
	if err := kagiPostJSON(ctx, key, kagiAPIBase+"/summarize", reqBody, 90*time.Second, &resp); err != nil {
		//nolint:nilerr // tool surfaces the reason in the result string
		return formatFetchError(webFetchErr{
			Code:      "kagi_summarize_failed",
			Message:   err.Error(),
			Retryable: true,
		}), nil
	}
	if len(resp.Error) > 0 {
		return formatFetchError(webFetchErr{
			Code:      "kagi_summarize_failed",
			Message:   kagiErrOr(resp.Error, "kagi summarize failed").Error(),
			Retryable: false,
		}), nil
	}
	out := strings.TrimSpace(resp.Data.Output)
	if out == "" {
		return "No summary produced.", nil
	}
	return out, nil
}
