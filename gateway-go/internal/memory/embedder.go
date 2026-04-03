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

// EmbedBatchAndStore embeds multiple facts in a single batch API call and stores
// the vectors. Returns the number of successfully stored embeddings.
func (e *Embedder) EmbedBatchAndStore(ctx context.Context, facts []struct {
	ID      int64
	Content string
}) (int, error) {
	if len(facts) == 0 {
		return 0, nil
	}

	texts := make([]string, len(facts))
	for i, f := range facts {
		texts[i] = f.Content
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(len(facts)+1)*embedFactTimeout)
	defer cancel()

	vecs, err := e.gemini.EmbedBatch(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("batch embed %d facts: %w", len(facts), err)
	}

	stored := 0
	for i, vec := range vecs {
		if err := e.store.StoreEmbedding(ctx, facts[i].ID, vec, "gemini-embedding-2-preview"); err != nil {
			e.logger.Debug("batch embed: store failed", "fact_id", facts[i].ID, "error", err)
			continue
		}
		stored++
	}
	return stored, nil
}

// EmbedQuery returns a normalized embedding for a search query.
func (e *Embedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, embedFactTimeout)
	defer cancel()
	return e.gemini.EmbedQuery(ctx, query)
}
