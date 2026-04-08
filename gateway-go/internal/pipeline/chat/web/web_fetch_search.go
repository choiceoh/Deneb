// web_fetch_search.go — Search provider implementations for the unified web tool.
//
// Provider priority: Perplexity Sonar → Brave Search → DuckDuckGo.
// Perplexity returns AI-synthesized answers with citations — concise, no token bloat.
// Brave returns traditional search results (title, URL, snippet).
// DuckDuckGo is the zero-config fallback (no API key needed).
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// --- Provider dispatch ---

// webSearch dispatches to the best available search provider.
// Priority: Perplexity → Brave → DuckDuckGo.
func webSearch(ctx context.Context, query string, count int) (string, error) {
	pplxKey := os.Getenv("PERPLEXITY_API_KEY")
	if pplxKey != "" {
		answer, citations, err := perplexitySearch(ctx, pplxKey, query)
		if err != nil {
			return "", err
		}
		return formatPerplexityResult(answer, citations), nil
	}
	braveKey := braveAPIKey()
	if braveKey != "" {
		return braveWebSearch(ctx, braveKey, query, count)
	}
	return duckDuckGoSearch(ctx, query)
}

// webSearchWithURLs searches and returns both formatted output and fetchable URLs.
// Used by search+fetch mode.
func webSearchWithURLs(ctx context.Context, query string, count int) (output string, urls []string, err error) {
	pplxKey := os.Getenv("PERPLEXITY_API_KEY")
	if pplxKey != "" {
		answer, citations, err := perplexitySearch(ctx, pplxKey, query)
		if err != nil {
			return "", nil, err
		}
		return formatPerplexityResult(answer, citations), citations, nil
	}
	braveKey := braveAPIKey()
	if braveKey != "" {
		results, err := braveSearchRaw(ctx, braveKey, query, count)
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

// --- Perplexity Sonar ---

// perplexityRequest is the chat completion request for Perplexity Sonar API.
type perplexityRequest struct {
	Model    string               `json:"model"`
	Messages []perplexityMessage  `json:"messages"`
}

type perplexityMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// perplexityResponse is the parsed Perplexity Sonar API response.
type perplexityResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Citations []string `json:"citations"`
}

// perplexitySearch performs a search via the Perplexity Sonar API.
// Returns an AI-synthesized answer and citation URLs.
func perplexitySearch(ctx context.Context, apiKey, query string) (answer string, citations []string, err error) {
	reqBody := perplexityRequest{
		Model: "sonar",
		Messages: []perplexityMessage{
			{Role: "user", Content: query},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, fmt.Errorf("marshal perplexity request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		"https://api.perplexity.ai/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", nil, fmt.Errorf("create perplexity request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := SharedClient(30 * time.Second).Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("perplexity request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", nil, fmt.Errorf("perplexity HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result perplexityResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil, fmt.Errorf("parse perplexity response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", nil, fmt.Errorf("perplexity returned no choices")
	}

	return result.Choices[0].Message.Content, result.Citations, nil
}

func formatPerplexityResult(answer string, citations []string) string {
	var sb strings.Builder
	if answer != "" {
		sb.WriteString(answer)
		sb.WriteString("\n\n")
	}
	if len(citations) > 0 {
		sb.WriteString("**Sources:**\n")
		for i, u := range citations {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, u)
		}
	}
	if sb.Len() == 0 {
		return "No results from Perplexity."
	}
	return sb.String()
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
