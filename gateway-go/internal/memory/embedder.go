// embedder.go — Embeds facts via Gemini Embedding API.
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/embedding"
)

const embedFactTimeout = 15 * time.Second

// Embedder generates and stores embeddings for facts via Gemini.
type Embedder struct {
	gemini *embedding.GeminiEmbedder
	store  *Store
	logger *slog.Logger
}

// NewEmbedder creates a fact embedder that calls the Gemini Embedding API.
func NewEmbedder(gemini *embedding.GeminiEmbedder, store *Store, logger *slog.Logger) *Embedder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Embedder{
		gemini: gemini,
		store:  store,
		logger: logger,
	}
}

// EmbedAndStore embeds a fact's content and stores the vector.
func (e *Embedder) EmbedAndStore(ctx context.Context, factID int64, content string) error {
	ctx, cancel := context.WithTimeout(ctx, embedFactTimeout)
	defer cancel()

	vec, err := e.gemini.EmbedQuery(ctx, content)
	if err != nil {
		return fmt.Errorf("embed fact %d: %w", factID, err)
	}

	return e.store.StoreEmbedding(ctx, factID, vec, "gemini-embedding-2-preview")
}

// EmbedQuery returns a normalized embedding for a search query.
func (e *Embedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, embedFactTimeout)
	defer cancel()
	return e.gemini.EmbedQuery(ctx, query)
}
