// web_kagi.go — Kagi API v1 providers: Search + Extract.
//
// Kagi API v1 (https://kagi.com/api/v1, key from https://kagi.com/api/keys):
// HTTP Bearer auth (`Authorization: Bearer <TOKEN>`, env KAGI_API_KEY /
// KAGI_API_TOKEN). The v1 surface is two POST endpoints:
//
//   - POST /search  — premium web search. Slots in as the highest-priority
//     provider ahead of Serper when the key is set (webSearch /
//     webSearchWithURLs).
//   - POST /extract — server-side fetch of a URL into clean markdown
//     (web `extract`).
//
// (FastGPT, Summarizer and Enrichment belong to the older v0 surface and are
// not part of the v1 API.)
//
// Missing key: search silently falls through to Serper; extract returns a
// user-facing error string.
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const kagiAPIBase = "https://kagi.com/api/v1"

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

// --- Shared request plumbing + error envelope ---

// kagiErrorDetail is one item of the v1 error envelope: the top-level `error`
// array on an API failure, or a per-page extract error.
type kagiErrorDetail struct {
	Code     string `json:"code"`
	URL      string `json:"url"`
	Message  string `json:"message"`
	Location string `json:"location"`
}

func kagiErrOr(errs []kagiErrorDetail, fallback string) error {
	if len(errs) == 0 {
		return fmt.Errorf("%s", fallback)
	}
	e := errs[0]
	switch {
	case e.Message != "":
		return fmt.Errorf("kagi: %s", e.Message)
	case e.Code != "":
		return fmt.Errorf("kagi error %s", e.Code)
	default:
		return fmt.Errorf("%s", fallback)
	}
}

// kagiPostJSON POSTs reqBody to a v1 endpoint with Bearer auth and decodes the
// JSON response into out. On a non-200 it tries to surface the error
// envelope's message before falling back to the status code.
func kagiPostJSON(ctx context.Context, apiKey, endpoint string, reqBody, out any, timeout time.Duration) error {
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
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := SharedClient(timeout).Do(req)
	if err != nil {
		return fmt.Errorf("kagi request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var env struct {
			Error []kagiErrorDetail `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&env) == nil && len(env.Error) > 0 {
			return kagiErrOr(env.Error, fmt.Sprintf("kagi HTTP %d", resp.StatusCode))
		}
		return fmt.Errorf("kagi HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("parse kagi response: %w", err)
	}
	return nil
}

// --- Search (provider chain) ---
//
// POST /search { query, workflow:"search", limit } →
// { data: { search:[{url,title,snippet,time}], news:[...], ... } }.
// The response groups results by category; the organic web list is data.search,
// which we map onto the shared searchResult shape so formatSearchResults renders
// Kagi identically to Brave.

type kagiSearchRequest struct {
	Query    string `json:"query"`
	Workflow string `json:"workflow,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type kagiV1SearchItem struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Time    string `json:"time"`
}

type kagiSearchResponse struct {
	Data struct {
		Search []kagiV1SearchItem `json:"search"`
	} `json:"data"`
	Error []kagiErrorDetail `json:"error"`
}

// kagiSearchRaw performs a POST /search (workflow=search) and returns the
// organic web results (data.search), capped at count.
func kagiSearchRaw(ctx context.Context, apiKey, query string, count int) ([]searchResult, error) {
	if count <= 0 {
		count = 5
	}
	reqBody := kagiSearchRequest{Query: query, Workflow: "search", Limit: count}
	var resp kagiSearchResponse
	if err := kagiPostJSON(ctx, apiKey, kagiAPIBase+"/search", reqBody, &resp, 15*time.Second); err != nil {
		return nil, err
	}
	if len(resp.Error) > 0 {
		return nil, kagiErrOr(resp.Error, "kagi search failed")
	}
	return kagiItemsToResults(resp.Data.Search, count), nil
}

// kagiItemsToResults maps search rows (dropping URL-less ones) onto the shared
// searchResult shape, capped at count (count<=0 = no cap).
func kagiItemsToResults(items []kagiV1SearchItem, count int) []searchResult {
	results := make([]searchResult, 0, len(items))
	for _, item := range items {
		if item.URL == "" {
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

// --- Extract (URL → markdown) ---
//
// POST /extract { pages:[{url}], format:"json" } →
// { data:[{url, markdown, error}] }. Kagi fetches and renders the page
// server-side, so there is no local SSRF surface. A per-page failure lands in
// data[i].error rather than a top-level error.

type kagiPageInput struct {
	URL string `json:"url"`
}

type kagiExtractRequest struct {
	Pages  []kagiPageInput `json:"pages"`
	Format string          `json:"format"`
}

type kagiExtractResponse struct {
	Data []struct {
		URL      string `json:"url"`
		Markdown string `json:"markdown"`
		Error    string `json:"error"`
	} `json:"data"`
	Error []kagiErrorDetail `json:"error"`
}

// webKagiExtract fetches target (an HTTPS URL) into clean markdown via Kagi.
// Errors are returned as user-facing strings (nil Go error) so the agent sees
// the reason.
func webKagiExtract(ctx context.Context, target string) (string, error) {
	key := kagiAPIKey()
	if key == "" {
		//nolint:nilerr // tool surfaces the reason in the result string
		return formatFetchError(webFetchErr{
			Code:      "no_kagi_key",
			Message:   "extract requires KAGI_API_KEY",
			Retryable: false,
		}), nil
	}
	reqBody := kagiExtractRequest{
		Pages:  []kagiPageInput{{URL: target}},
		Format: "json",
	}
	var resp kagiExtractResponse
	if err := kagiPostJSON(ctx, key, kagiAPIBase+"/extract", reqBody, &resp, 30*time.Second); err != nil {
		//nolint:nilerr // tool surfaces the reason in the result string
		return formatFetchError(webFetchErr{
			Code:      "kagi_extract_failed",
			Message:   err.Error(),
			Retryable: true,
		}), nil
	}
	if len(resp.Error) > 0 {
		return formatFetchError(webFetchErr{
			Code:      "kagi_extract_failed",
			Message:   kagiErrOr(resp.Error, "kagi extract failed").Error(),
			Retryable: false,
		}), nil
	}
	if len(resp.Data) == 0 {
		return "No content extracted.", nil
	}
	page := resp.Data[0]
	if e := strings.TrimSpace(page.Error); e != "" {
		return formatFetchError(webFetchErr{
			Code:      "kagi_extract_failed",
			Message:   e,
			Retryable: true,
		}), nil
	}
	md := strings.TrimSpace(page.Markdown)
	if md == "" {
		return "No content extracted.", nil
	}
	return md, nil
}
