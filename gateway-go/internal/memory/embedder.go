// embedder.go — Embeds facts via SGLang /v1/embeddings endpoint.
// Mirrors the pattern from vega/sglang_embedder.go.
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const embedFactTimeout = 15 * time.Second

// Embedder generates and stores embeddings for facts via SGLang.
type Embedder struct {
	client *llm.Client
	model  string
	store  *Store
	logger *slog.Logger
}

// NewEmbedder creates a fact embedder that calls the SGLang server.
func NewEmbedder(baseURL, model string, store *Store, logger *slog.Logger) *Embedder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Embedder{
		client: llm.NewClient(baseURL, "", llm.WithLogger(logger)),
		model:  model,
		store:  store,
		logger: logger,
	}
}

// EmbedAndStore embeds a fact's content and stores the vector.
func (e *Embedder) EmbedAndStore(ctx context.Context, factID int64, content string) error {
	ctx, cancel := context.WithTimeout(ctx, embedFactTimeout)
	defer cancel()

	vec, err := e.embed(ctx, content)
	if err != nil {
		return fmt.Errorf("embed fact %d: %w", factID, err)
	}

	return e.store.StoreEmbedding(ctx, factID, vec, e.model)
}

// EmbedQuery returns a normalized embedding for a search query.
func (e *Embedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, embedFactTimeout)
	defer cancel()
	return e.embed(ctx, query)
}

func (e *Embedder) embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := e.client.Embed(ctx, llm.EmbeddingRequest{
		Model: e.model,
		Input: []string{text},
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}

	vec := resp.Data[0].Embedding
	l2Normalize(vec)
	return vec, nil
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
