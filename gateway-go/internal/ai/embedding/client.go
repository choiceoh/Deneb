// Package embedding provides a client for the BGE-M3 embedding server.
// Used by Polaris compaction for MMR-based extractive fallback when LLM
// summarization is unavailable.
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
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

	identityMu sync.RWMutex
	model      string
	dimensions int

	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a Client and starts background health checking.
func New(baseURL string, logger *slog.Logger) *Client {
	if baseURL == "" {
		// Cutover/rollback lever (Nemotron 2026-07-18): point the gateway at an
		// alternate embedding sidecar without a code change. The semantic caches
		// key on EmbeddingFingerprint (model:dims), so flipping this re-embeds
		// automatically and flipping back restores the old cache.
		baseURL = strings.TrimSpace(os.Getenv("DENEB_EMBEDDING_URL"))
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if logger == nil {
		logger = slog.Default()
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
	// Initial probe (non-blocking). Wrapped with panic recovery so a misbehaving
	// HTTP response cannot take down the whole process.
	safego.GoWithSlog(logger, "embedding-probe", c.probe)
	safego.GoWithSlog(logger, "embedding-health-loop", c.healthLoop)
	return c
}

// Shutdown stops background health checks.
func (c *Client) Shutdown() { c.cancel() }

// IsHealthy returns whether the embedding server is reachable.
func (c *Client) IsHealthy() bool { return c.healthy.Load() }

// EmbeddingFingerprint identifies the vector semantics exposed by the active
// sidecar. It intentionally stays empty until /health reports both model and
// dimensions, so cache users do not invalidate a valid cache during the
// asynchronous startup probe.
func (c *Client) EmbeddingFingerprint() string {
	c.identityMu.RLock()
	defer c.identityMu.RUnlock()
	if c.model == "" || c.dimensions <= 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", c.model, c.dimensions)
}

// EmbeddingDimensions returns the probed or most recently observed vector
// width. Zero means the sidecar identity has not been established yet.
func (c *Client) EmbeddingDimensions() int {
	c.identityMu.RLock()
	defer c.identityMu.RUnlock()
	return c.dimensions
}

func (c *Client) recordIdentity(model string, dimensions int) {
	c.identityMu.Lock()
	defer c.identityMu.Unlock()
	if strings.TrimSpace(model) != "" {
		c.model = strings.TrimSpace(model)
	}
	if dimensions > 0 {
		c.dimensions = dimensions
	}
}

type embedRequest struct {
	Texts []string `json:"texts"`
	// Kind marks the retrieval role for asymmetric models ("query" | "passage");
	// empty means passage/default. The BGE sidecar ignores the field (pydantic
	// drops unknown keys), so the same client speaks to both server generations.
	Kind string `json:"kind,omitempty"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Dimensions int         `json:"dimensions"`
	Count      int         `json:"count"`
}

// Embed returns dense embeddings for the given texts (passage/default role).
// Returns one embedding vector per input text.
// Returns an error immediately if the server is known to be unhealthy,
// avoiding a wasted 30s timeout on every compaction attempt.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return c.EmbedKind(ctx, "", texts)
}

// EmbedKind embeds texts in an explicit retrieval role. Asymmetric models
// (Nemotron) are trained with distinct query/passage prefixes — the sidecar
// applies them from this field; symmetric sidecars (BGE) ignore it. Search
// paths pass "query"; indexing/refresh paths use Embed (passage/default).
func (c *Client) EmbedKind(ctx context.Context, kind string, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if !c.healthy.Load() {
		return nil, fmt.Errorf("embedding: server unhealthy")
	}
	if len(texts) > maxTextsPerBatch {
		return nil, fmt.Errorf("embedding: batch size %d exceeds max %d", len(texts), maxTextsPerBatch)
	}

	body, err := json.Marshal(embedRequest{Texts: texts, Kind: kind})
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
		// A caller-side cancellation or timeout (e.g. recall's 1.5s preflight
		// budget) is not a server fault. Flipping health here would disable
		// semantic search, re-embedding, and SuggestRelated for the full
		// healthCheckPeriod (30s) over a transient client deadline. Only a
		// genuine transport failure marks the server unhealthy.
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			c.healthy.Store(false)
		}
		return nil, fmt.Errorf("embedding: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("embedding: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result embedResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("embedding: decode: %w", err)
	}
	if len(result.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embedding: expected %d embeddings, got %d", len(texts), len(result.Embeddings))
	}
	if result.Count > 0 && result.Count != len(result.Embeddings) {
		return nil, fmt.Errorf("embedding: response count %d does not match %d embeddings", result.Count, len(result.Embeddings))
	}
	for i, vector := range result.Embeddings {
		if len(vector) == 0 {
			return nil, fmt.Errorf("embedding: embedding %d is empty", i)
		}
		if result.Dimensions > 0 && len(vector) != result.Dimensions {
			return nil, fmt.Errorf("embedding: embedding %d has %d dimensions, expected %d", i, len(vector), result.Dimensions)
		}
	}
	c.recordIdentity("", result.Dimensions)
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
	defer resp.Body.Close()

	var health struct {
		Model      string `json:"model"`
		Dimensions int    `json:"dimensions"`
	}
	if resp.StatusCode == http.StatusOK {
		// Identity metadata is diagnostic, not a liveness prerequisite. Older
		// sidecars that return only {"status":"ok"} remain healthy.
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&health)
		c.recordIdentity(health.Model, health.Dimensions)
	}

	wasHealthy := c.healthy.Load()
	c.healthy.Store(resp.StatusCode == http.StatusOK)
	if !wasHealthy && resp.StatusCode == http.StatusOK {
		c.logger.Info("embedding server healthy", "url", c.baseURL)
	}
}
