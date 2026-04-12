// Package embedding provides a client for the BGE-M3 embedding server.
// Used by Polaris compaction for MMR-based extractive fallback when LLM
// summarization is unavailable.
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

const (
	defaultBaseURL    = "http://127.0.0.1:8001"
	defaultTimeout    = 30 * time.Second
	healthCheckPeriod = 30 * time.Second
	maxTextsPerBatch  = 256
)

// Client communicates with the BGE-M3 embedding server.
type Client struct {
	baseURL string
	http    *http.Client
	healthy atomic.Bool
	logger  *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a Client and starts background health checking.
func New(baseURL string, logger *slog.Logger) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		baseURL: baseURL,
		http: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
	// Initial probe (non-blocking).
	go c.probe()
	go c.healthLoop()
	return c
}

// Shutdown stops background health checks.
func (c *Client) Shutdown() { c.cancel() }

// IsHealthy returns whether the embedding server is reachable.
func (c *Client) IsHealthy() bool { return c.healthy.Load() }

type embedRequest struct {
	Texts []string `json:"texts"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Dimensions int         `json:"dimensions"`
	Count      int         `json:"count"`
}

// Embed returns dense embeddings for the given texts.
// Returns one embedding vector per input text.
// Returns an error immediately if the server is known to be unhealthy,
// avoiding a wasted 30s timeout on every compaction attempt.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if !c.healthy.Load() {
		return nil, fmt.Errorf("embedding: server unhealthy")
	}
	if len(texts) > maxTextsPerBatch {
		return nil, fmt.Errorf("embedding: batch size %d exceeds max %d", len(texts), maxTextsPerBatch)
	}

	body, err := json.Marshal(embedRequest{Texts: texts})
	if err != nil {
		return nil, fmt.Errorf("embedding: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		c.healthy.Store(false)
		return nil, fmt.Errorf("embedding: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embedding: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("embedding: decode: %w", err)
	}
	if len(result.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embedding: expected %d embeddings, got %d", len(texts), len(result.Embeddings))
	}
	return result.Embeddings, nil
}

// --- health checking ---

func (c *Client) healthLoop() {
	ticker := time.NewTicker(healthCheckPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.probe()
		}
	}
}

func (c *Client) probe() {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", http.NoBody)
	if err != nil {
		c.healthy.Store(false)
		return
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if c.healthy.Load() {
			c.logger.Warn("embedding server unhealthy", "error", err)
		}
		c.healthy.Store(false)
		return
	}
	resp.Body.Close()

	wasHealthy := c.healthy.Load()
	c.healthy.Store(resp.StatusCode == http.StatusOK)
	if !wasHealthy && resp.StatusCode == http.StatusOK {
		c.logger.Info("embedding server healthy", "url", c.baseURL)
	}
}
