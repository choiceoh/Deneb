package vega

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const embedTimeout = 30 * time.Second

// SglangEmbedder generates embeddings via SGLang's /v1/embeddings endpoint.
type SglangEmbedder struct {
	client *llm.Client
	model  string
	logger *slog.Logger
}

// NewSglangEmbedder creates an embedder that calls the SGLang server.
func NewSglangEmbedder(baseURL, model string, logger *slog.Logger) *SglangEmbedder {
	if logger == nil {
		logger = slog.Default()
	}
	return &SglangEmbedder{
		client: llm.NewClient(baseURL, "", llm.WithLogger(logger)),
		model:  model,
		logger: logger,
	}
}

// EmbedQuery embeds a single query text and returns an L2-normalized f32 vector.
func (e *SglangEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, embedTimeout)
	defer cancel()

	resp, err := e.client.Embed(ctx, llm.EmbeddingRequest{
		Model: e.model,
		Input: []string{query},
	})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("embed query: empty response")
	}

	vec := resp.Data[0].Embedding
	l2Normalize(vec)
	return vec, nil
}

// EmbedBatch embeds multiple texts for chunk ingest.
// Returns L2-normalized vectors in the same order as input.
func (e *SglangEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, embedTimeout)
	defer cancel()

	resp, err := e.client.Embed(ctx, llm.EmbeddingRequest{
		Model: e.model,
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("embed batch: %w", err)
	}

	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("embed batch: expected %d results, got %d", len(texts), len(resp.Data))
	}

	result := make([][]float32, len(texts))
	for _, d := range resp.Data {
		if d.Index < 0 || d.Index >= len(texts) {
			continue
		}
		vec := d.Embedding
		l2Normalize(vec)
		result[d.Index] = vec
	}
	return result, nil
}

// l2Normalize normalizes a vector in-place to unit length.
func l2Normalize(vec []float32) {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return
	}
	norm := float32(math.Sqrt(sum))
	for i := range vec {
		vec[i] /= norm
	}
}
