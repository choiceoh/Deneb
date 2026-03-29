package vega

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
)

// EnhancedBackend wraps RustBackend with Gemini embedding, query expansion,
// and cross-encoder reranking via Jina Reranker API.
type EnhancedBackend struct {
	rust     *RustBackend
	embedder *embedding.GeminiEmbedder
	reranker *Reranker
	expander *LLMExpander
	logger   *slog.Logger
	cache    *searchCache
}

// EnhancedBackendConfig configures the EnhancedBackend.
type EnhancedBackendConfig struct {
	Logger      *slog.Logger
	SglangURL   string // e.g. "http://127.0.0.1:30000/v1" — used for chat/expansion
	SglangModel string // e.g. "Qwen/Qwen3.5-35B-A3B" — chat model for expansion

	// Embedder is the Gemini embedding client. If nil, search falls back to FTS-only.
	Embedder *embedding.GeminiEmbedder

	// JinaAPIKey enables cross-encoder reranking via the Jina Reranker API.
	// If empty, reranking is skipped and results use cosine+BM25 fusion order.
	JinaAPIKey string
}

// NewEnhancedBackend creates a Vega backend with Gemini embedding and query expansion.
func NewEnhancedBackend(cfg EnhancedBackendConfig) *EnhancedBackend {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	rust := NewRustBackend(RustBackendConfig{Logger: cfg.Logger})
	expander := NewLLMExpander(cfg.SglangURL, cfg.SglangModel, cfg.Logger)

	// Reranker uses Jina Reranker API if API key is configured.
	reranker := NewReranker(RerankConfig{
		APIKey: cfg.JinaAPIKey,
		Logger: cfg.Logger,
	})

	cfg.Logger.Info("vega: FFI ready (EnhancedBackend)",
		"embedding", cfg.Embedder != nil,
		"reranking", reranker != nil,
	)
	return &EnhancedBackend{
		rust:     rust,
		embedder: cfg.Embedder,
		reranker: reranker,
		expander: expander,
		logger:   cfg.Logger,
		cache:    newSearchCache(),
	}
}

// Execute delegates to the Rust backend.
func (eb *EnhancedBackend) Execute(ctx context.Context, cmd string, args map[string]any) (json.RawMessage, error) {
	return eb.rust.Execute(ctx, cmd, args)
}

// Search runs a Vega search with Gemini embedding, query expansion, and cross-encoder reranking.
//
// Pipeline:
//  1. Parallel: Gemini query embedding + SGLang query expansion
//  2. If embedding succeeded, pass vector to Rust for cosine similarity search
//  3. If expansion succeeded and FTS results are sparse, run supplemental FTS
//  4. Cross-encoder reranking via Jina Reranker API if available
func (eb *EnhancedBackend) Search(ctx context.Context, query string, opts SearchOpts) ([]SearchResult, error) {
	// Check cache first — avoids redundant SGLang + FFI calls.
	cacheKey := searchCacheKey(query, opts)
	if cached, ok := eb.cache.get(cacheKey); ok {
		eb.logger.Debug("vega: cache hit", "query", query)
		return cached, nil
	}

	var (
		queryVec []float32
		expanded []string
		mu       sync.Mutex
	)

	// Phase 1: Parallel SGLang calls (embedding + expansion).
	// Both are best-effort — failures fall back to FTS-only.
	var wg sync.WaitGroup

	if eb.embedder != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vec, err := eb.embedder.EmbedQuery(ctx, query)
			if err != nil {
				eb.logger.Debug("vega: embedding failed, falling back to FTS", "error", err)
				return
			}
			mu.Lock()
			queryVec = vec
			mu.Unlock()
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		terms := eb.expander.Expand(ctx, query)
		if len(terms) > 0 {
			mu.Lock()
			expanded = terms
			mu.Unlock()
		}
	}()

	wg.Wait()

	// Phase 2: Search with embedding vector via Rust FFI.
	results, err := eb.searchWithVector(ctx, query, queryVec, opts)
	if err != nil {
		return nil, err
	}

	// Phase 3: Supplemental expanded FTS if results are sparse.
	if len(results) < 5 && len(expanded) > 0 {
		expandedQuery := BuildExpandedQuery(query, expanded)
		eb.logger.Debug("vega: running expanded FTS", "query", expandedQuery)
		moreResults, err := eb.rust.Search(ctx, expandedQuery, opts)
		if err == nil && len(moreResults) > 0 {
			results = mergeResults(results, moreResults)
		}
	}

	// Phase 4: Cross-encoder reranking (optional).
	if eb.reranker != nil && len(results) > 1 {
		results = eb.rerankResults(ctx, query, results)
	}

	// Apply limit.
	if opts.Limit > 0 && len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	// Cache results for future identical queries.
	eb.cache.put(cacheKey, results)

	return results, nil
}

// searchWithVector calls Rust FFI with an optional query embedding vector.
// When queryVec is provided, Rust performs cosine similarity against chunk_embeddings.
// When nil, Rust performs FTS-only search.
func (eb *EnhancedBackend) searchWithVector(ctx context.Context, query string, queryVec []float32, opts SearchOpts) ([]SearchResult, error) {
	payload := map[string]any{
		"query": query,
	}
	if len(queryVec) > 0 {
		payload["query_embedding"] = queryVec
	}

	queryJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("vega enhanced: marshal query: %w", err)
	}

	eb.logger.Debug("vega enhanced: search",
		"query", query,
		"has_embedding", len(queryVec) > 0,
	)

	resultBytes, err := ffi.VegaSearch(string(queryJSON))
	if err != nil {
		return nil, fmt.Errorf("vega enhanced: ffi search: %w", err)
	}

	return parseSearchResults(resultBytes)
}

// parseSearchResults parses the Rust FFI search response.
func parseSearchResults(resultBytes []byte) ([]SearchResult, error) {
	var rawResult struct {
		Unified []struct {
			ProjectID   int64   `json:"project_id"`
			ProjectName string  `json:"project_name"`
			Heading     string  `json:"heading"`
			Content     string  `json:"content"`
			Score       float64 `json:"score"`
		} `json:"unified"`
		Error  string `json:"error,omitempty"`
		Detail string `json:"detail,omitempty"`
	}

	if err := json.Unmarshal(resultBytes, &rawResult); err != nil {
		return nil, fmt.Errorf("vega enhanced: unmarshal results: %w", err)
	}

	if rawResult.Error != "" {
		return nil, fmt.Errorf("vega enhanced: %s: %s", rawResult.Error, rawResult.Detail)
	}

	results := make([]SearchResult, 0, len(rawResult.Unified))
	for _, u := range rawResult.Unified {
		results = append(results, SearchResult{
			ProjectID:   int(u.ProjectID),
			ProjectName: u.ProjectName,
			Section:     u.Heading,
			Content:     u.Content,
			Score:       u.Score,
		})
	}

	return results, nil
}

// mergeResults merges additional results into existing results, deduplicating by ProjectID + Section.
func mergeResults(existing, additional []SearchResult) []SearchResult {
	seen := make(map[string]bool, len(existing))
	for _, r := range existing {
		key := fmt.Sprintf("%d:%s", r.ProjectID, r.Section)
		seen[key] = true
	}

	for _, r := range additional {
		key := fmt.Sprintf("%d:%s", r.ProjectID, r.Section)
		if !seen[key] {
			existing = append(existing, r)
			seen[key] = true
		}
	}
	return existing
}

// rerankResults reorders search results using the cross-encoder reranker.
// On failure, returns the original results unchanged (graceful fallback).
func (eb *EnhancedBackend) rerankResults(ctx context.Context, query string, results []SearchResult) []SearchResult {
	docs := make([]string, len(results))
	for i, r := range results {
		if r.Section != "" {
			docs[i] = r.Section + ": " + r.Content
		} else {
			docs[i] = r.Content
		}
	}

	ranked, err := eb.reranker.Rerank(ctx, query, docs, len(results))
	if err != nil {
		eb.logger.Debug("vega: reranking failed, using fusion order", "error", err)
		return results
	}

	reranked := make([]SearchResult, 0, len(ranked))
	for _, r := range ranked {
		if r.Index >= 0 && r.Index < len(results) {
			res := results[r.Index]
			res.Score = r.RelevanceScore
			reranked = append(reranked, res)
		}
	}

	if len(reranked) == 0 {
		return results
	}
	return reranked
}

// HealthCheck probes each external dependency and returns a status report.
// Each check runs in parallel with a short timeout.
func (eb *EnhancedBackend) HealthCheck(ctx context.Context) HealthStatus {
	var (
		components []ComponentHealth
		mu         sync.Mutex
		wg         sync.WaitGroup
	)

	// Check Gemini Embedding API.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := ComponentHealth{Name: "embedding (Gemini)"}
		if eb.embedder == nil {
			ch.Available = false
			ch.Detail = "not configured (GEMINI_API_KEY not set)"
		} else {
			probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			start := time.Now()
			_, err := eb.embedder.EmbedQuery(probeCtx, "health check")
			elapsed := time.Since(start)
			ch.Latency = elapsed.Round(time.Millisecond).String()
			if err != nil {
				ch.Available = false
				ch.Detail = err.Error()
			} else {
				ch.Available = true
				ch.Detail = "gemini-embedding-2-preview"
			}
		}
		mu.Lock()
		components = append(components, ch)
		mu.Unlock()
	}()

	// Check Jina Reranker API.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := ComponentHealth{Name: "reranker (Jina)"}
		if eb.reranker == nil {
			ch.Available = false
			ch.Detail = "not configured (JINA_API_KEY not set)"
		} else {
			probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			start := time.Now()
			_, err := eb.reranker.Rerank(probeCtx, "test", []string{"health check probe"}, 1)
			elapsed := time.Since(start)
			ch.Latency = elapsed.Round(time.Millisecond).String()
			if err != nil {
				ch.Available = false
				ch.Detail = err.Error()
			} else {
				ch.Available = true
				ch.Detail = eb.reranker.model
			}
		}
		mu.Lock()
		components = append(components, ch)
		mu.Unlock()
	}()

	// Check SGLang (query expansion).
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := ComponentHealth{Name: "sglang"}
		if eb.expander == nil || eb.expander.client == nil {
			ch.Available = false
			ch.Detail = "not configured"
		} else {
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			start := time.Now()
			terms := eb.expander.Expand(probeCtx, "health check test query")
			elapsed := time.Since(start)
			ch.Latency = elapsed.Round(time.Millisecond).String()
			if terms == nil {
				// Expand returns nil on failure — try a simpler connectivity check.
				ch.Available = false
				ch.Detail = fmt.Sprintf("model=%s, expansion failed (server may be down)", eb.expander.model)
			} else {
				ch.Available = true
				ch.Detail = fmt.Sprintf("model=%s, expansion ok (%d terms)", eb.expander.model, len(terms))
			}
		}
		mu.Lock()
		components = append(components, ch)
		mu.Unlock()
	}()

	wg.Wait()
	return HealthStatus{Components: components}
}

// Close is a no-op.
func (eb *EnhancedBackend) Close() error {
	return nil
}
