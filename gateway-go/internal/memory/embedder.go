// embedder.go — Embeds facts via a pluggable embedding provider.
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/embedding"
)

const embedFactTimeout = 15 * time.Second

// Embedder generates and stores embeddings for facts.
type Embedder struct {
	embed     embedding.Embedder
	store     *Store
	logger    *slog.Logger
	modelName string
}

// modelNamer is an optional interface for embedders that expose their model name.
type modelNamer interface {
	ModelName() string
}

// NewEmbedder creates a fact embedder using the given embedding provider.
func NewEmbedder(embed embedding.Embedder, store *Store, logger *slog.Logger) *Embedder {
	if logger == nil {
		logger = slog.Default()
	}

	name := "unknown"
	if mn, ok := embed.(modelNamer); ok {
		name = mn.ModelName()
	}

	return &Embedder{
		embed:     embed,
		store:     store,
		logger:    logger,
		modelName: name,
	}
}

// EmbedAndStore embeds a fact's content and stores the vector.
func (e *Embedder) EmbedAndStore(ctx context.Context, factID int64, content string) error {
	ctx, cancel := context.WithTimeout(ctx, embedFactTimeout)
	defer cancel()

	vec, err := e.embed.EmbedQuery(ctx, content)
	if err != nil {
		return fmt.Errorf("embed fact %d: %w", factID, err)
	}

	return e.store.StoreEmbedding(ctx, factID, vec, e.modelName)
}

// EmbedBatchAndStore embeds multiple facts in a single batch call and stores
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

	vecs, err := e.embed.EmbedBatch(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("batch embed %d facts: %w", len(facts), err)
	}

	stored := 0
	for i, vec := range vecs {
		if err := e.store.StoreEmbedding(ctx, facts[i].ID, vec, e.modelName); err != nil {
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
	return e.embed.EmbedQuery(ctx, query)
}
