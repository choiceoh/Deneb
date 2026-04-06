// reranker.go — HTTP client for cross-encoder reranking.
//
// Calls a /v1/rerank endpoint to score query-document pairs using a
// cross-encoder model. Supports both local (jina-reranker-v3) and remote
// (Jina API) backends.
package reranker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"time"
)

const (
	rerankTimeout = 30 * time.Second
	// Truncate documents to avoid exceeding the model's context window (8192 tokens).
	// ~1000 chars is a safe limit for typical multilingual content.
	maxRerankDocChars = 1000
	// Default reranker endpoint (local jina-reranker-v3 server).
	DefaultRerankURL = "http://localhost:8090/v1/rerank"
	// Default model.
	defaultJinaRerankModel = "jinaai/jina-reranker-v3"
)

// RerankResult holds a single reranked document with its relevance score.
type RerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

// Reranker scores query-document pairs via the Jina Reranker API.
type Reranker struct {
	url    string // e.g. "https://api.jina.ai/v1/rerank"
	model  string
	apiKey string
	client *http.Client
	logger *slog.Logger
}

// RerankConfig configures the reranker client.
type RerankConfig struct {
	// APIKey is the bearer token for the reranker endpoint.
	// Optional for local servers; required for remote Jina API.
	APIKey string
	// URL overrides the default Jina API endpoint (optional).
	URL string
	// Model overrides the default model (optional).
	Model  string
	Logger *slog.Logger
}

// NewReranker creates a reranker client. Always returns a valid client
// (local server needs no API key). Pass APIKey for remote Jina API usage.
func NewReranker(cfg RerankConfig) *Reranker {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	url := cfg.URL
	if url == "" {
		url = DefaultRerankURL
	}
	model := cfg.Model
	if model == "" {
		model = defaultJinaRerankModel
	}
	return &Reranker{
		url:    url,
		model:  model,
		apiKey: cfg.APIKey,
		client: &http.Client{Timeout: rerankTimeout},
		logger: cfg.Logger,
	}
}

// rerankRequest is the Jina /v1/rerank request body.
type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n,omitempty"`
}

// rerankResponse is the Jina /v1/rerank response body.
type rerankResponse struct {
	Results []RerankResult `json:"results"`
}

// Rerank scores each document against the query and returns results sorted by
// descending relevance score. Returns at most topN results (0 = all).
func (r *Reranker) Rerank(ctx context.Context, query string, documents []string, topN int) ([]RerankResult, error) {
	if len(documents) == 0 {
		return nil, nil
	}

	// Truncate documents to stay within model context window.
	truncated := make([]string, len(documents))
	for i, doc := range documents {
		truncated[i] = truncateString(doc, maxRerankDocChars)
	}

	reqBody := rerankRequest{
		Model:     r.model,
		Query:     query,
		Documents: truncated,
	}
	if topN > 0 {
		reqBody.TopN = topN
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("reranker: marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, rerankTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("reranker: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if r.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+r.apiKey)
	}

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("reranker: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if readErr != nil {
			return nil, fmt.Errorf("reranker: HTTP %d (failed to read error body)", resp.StatusCode)
		}
		return nil, fmt.Errorf("reranker: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MB limit
	if err != nil {
		return nil, fmt.Errorf("reranker: read response: %w", err)
	}

	var rerankResp rerankResponse
	if err := json.Unmarshal(data, &rerankResp); err != nil {
		return nil, fmt.Errorf("reranker: unmarshal response: %w", err)
	}

	// Sort by relevance score descending.
	results := rerankResp.Results
	sort.Slice(results, func(i, j int) bool {
		return results[i].RelevanceScore > results[j].RelevanceScore
	})

	return results, nil
}

// truncateString truncates s to at most maxChars, respecting UTF-8 boundaries.
func truncateString(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	return string(runes[:maxChars])
}

// IsReachable checks if the local reranker server is responsive.
func IsReachable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DefaultRerankURL, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	// Any response (even 405) means the server is running.
	return true
}

// GetJinaAPIKey reads the Jina AI API key from the JINA_API_KEY environment variable.
// Returns empty string if not configured (reranking will be disabled).
func GetJinaAPIKey() string {
	return os.Getenv("JINA_API_KEY")
}

// IsLocalAIReachable checks if the local AI server responds to /v1/models.
func IsLocalAIReachable(baseURL string) bool {
	if baseURL == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
