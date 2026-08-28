// Package rerank provides an optional HTTP client for a dedicated ranking
// sidecar. It supports the common /rerank response shapes used by TEI/Cohere-
// compatible services while keeping the wiki domain independent of HTTP.
package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const requestTimeout = 2 * time.Second

// ErrBusy means another optional rerank is already using the shared sidecar.
// Callers are expected to fail open to retrieval order rather than queue behind
// a GPU request and consume the rest of their turn deadline.
var ErrBusy = errors.New("rerank: busy")

type Client struct {
	baseURL string
	model   string
	http    *http.Client
	gate    chan struct{}

	requests      atomic.Uint64
	successes     atomic.Uint64
	failures      atomic.Uint64
	busy          atomic.Uint64
	lastLatencyMS atomic.Int64
	maxLatencyMS  atomic.Int64
}

// Stats is the bounded operational snapshot exposed through gateway health.
type Stats struct {
	Requests      uint64 `json:"requests"`
	Successes     uint64 `json:"successes"`
	Failures      uint64 `json:"failures"`
	Busy          uint64 `json:"busy"`
	InFlight      int    `json:"inFlight"`
	LastLatencyMS int64  `json:"lastLatencyMs"`
	MaxLatencyMS  int64  `json:"maxLatencyMs"`
}

// NewFromEnv enables reranking only when DENEB_RERANK_URL is explicitly set.
// DENEB_RERANK_MODEL is forwarded when the service hosts multiple models.
func NewFromEnv() *Client {
	return New(os.Getenv("DENEB_RERANK_URL"), os.Getenv("DENEB_RERANK_MODEL"))
}

func New(baseURL, model string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &Client{
		baseURL: baseURL,
		model:   strings.TrimSpace(model),
		http:    &http.Client{Timeout: requestTimeout},
		gate:    make(chan struct{}, 1),
	}
}

func (c *Client) Identity() string {
	if c == nil {
		return ""
	}
	if c.model != "" {
		return c.model
	}
	return "configured-reranker"
}

func (c *Client) Stats() Stats {
	if c == nil {
		return Stats{}
	}
	return Stats{
		Requests:      c.requests.Load(),
		Successes:     c.successes.Load(),
		Failures:      c.failures.Load(),
		Busy:          c.busy.Load(),
		InFlight:      len(c.gate),
		LastLatencyMS: c.lastLatencyMS.Load(),
		MaxLatencyMS:  c.maxLatencyMS.Load(),
	}
}

type rerankRequest struct {
	Model     string   `json:"model,omitempty"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n,omitempty"`
}

type rankedItem struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
	Score          float64 `json:"score"`
}

type rerankResponse struct {
	Scores  []float64    `json:"scores"`
	Pruned  []string     `json:"pruned"`
	Results []rankedItem `json:"results"`
	Data    []rankedItem `json:"data"`
}

// Rerank returns one score per input document in the original order.
func (c *Client) Rerank(ctx context.Context, query string, documents []string) ([]float64, error) {
	scores, _, err := c.RerankPruned(ctx, query, documents)
	return scores, err
}

// RerankPruned additionally returns the sidecar's query-conditioned pruned
// context per document, aligned with scores. The pruning rides the SAME
// cross-encoder forward pass (xprovence computes it either way), so asking for
// it costs no extra model work; a sidecar or model without pruning yields nil
// and callers must treat that as "no pruning", never as empty documents.
func (c *Client) RerankPruned(ctx context.Context, query string, documents []string) (scores []float64, pruned []string, err error) {
	if c == nil || c.baseURL == "" {
		return nil, nil, fmt.Errorf("rerank: client disabled")
	}
	if len(documents) == 0 {
		return nil, nil, nil
	}
	c.requests.Add(1)
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	default:
		c.busy.Add(1)
		return nil, nil, ErrBusy
	}
	started := time.Now()
	defer func() {
		latency := time.Since(started).Milliseconds()
		c.lastLatencyMS.Store(latency)
		for previous := c.maxLatencyMS.Load(); latency > previous && !c.maxLatencyMS.CompareAndSwap(previous, latency); previous = c.maxLatencyMS.Load() {
		}
		if err != nil {
			c.failures.Add(1)
		} else {
			c.successes.Add(1)
		}
	}()
	body, err := json.Marshal(rerankRequest{Model: c.model, Query: query, Documents: documents, TopN: len(documents)})
	if err != nil {
		return nil, nil, fmt.Errorf("rerank: marshal: %w", err)
	}
	endpoint := c.baseURL
	if !strings.HasSuffix(endpoint, "/rerank") {
		endpoint += "/rerank"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("rerank: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("rerank: request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("rerank: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("rerank: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var decoded rerankResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, nil, fmt.Errorf("rerank: decode: %w", err)
	}
	if len(decoded.Scores) == len(documents) {
		if len(decoded.Pruned) == len(documents) {
			pruned = decoded.Pruned
		}
		return decoded.Scores, pruned, nil
	}
	items := decoded.Results
	if len(items) == 0 {
		items = decoded.Data
	}
	if len(items) != len(documents) {
		return nil, nil, fmt.Errorf("rerank: expected %d scores, got %d", len(documents), max(len(decoded.Scores), len(items)))
	}
	scores = make([]float64, len(documents))
	seen := make([]bool, len(documents))
	for _, item := range items {
		if item.Index < 0 || item.Index >= len(documents) || seen[item.Index] {
			return nil, nil, fmt.Errorf("rerank: invalid result index %d", item.Index)
		}
		score := item.RelevanceScore
		if score == 0 {
			score = item.Score
		}
		scores[item.Index] = score
		seen[item.Index] = true
	}
	return scores, nil, nil
}

// Probe verifies the configured endpoint without relying on a vendor-specific
// health route.
func (c *Client) Probe(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("rerank: client disabled")
	}
	_, err := c.Rerank(ctx, "health check", []string{"health check", "unrelated"})
	return err
}
