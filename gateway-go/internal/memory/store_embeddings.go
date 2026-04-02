// store_embeddings.go — Embedding storage and loading for the memory store.
package memory

import (
	"context"
	"time"
)

// StoreEmbedding saves a fact's embedding vector.
func (s *Store) StoreEmbedding(ctx context.Context, factID int64, vec []float32, modelName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	blob := float32sToBlob(vec)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO fact_embeddings (fact_id, embedding, model_name, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(fact_id) DO UPDATE SET embedding = excluded.embedding, model_name = excluded.model_name, updated_at = excluded.updated_at`,
		factID, blob, modelName, now,
	)
	if err == nil && s.embCacheReady {
		// Copy-on-write: callers that received a reference to the old cache map
		// via LoadEmbeddings may still be iterating it after we released the
		// read lock. Mutating the map in-place while another goroutine reads it
		// is a data race. Instead, build a new map and replace the reference so
		// existing snapshots remain stable and immutable.
		newCache := make(map[int64][]float32, len(s.embCache)+1)
		for k, v := range s.embCache {
			newCache[k] = v
		}
		newCache[factID] = vec
		s.embCache = newCache
	}
	return err
}

// LoadEmbeddings loads all active fact embeddings for similarity search.
// Results are cached in memory; subsequent calls return the cache until
// a mutation (insert, deactivate, prune, supersede) invalidates it.
func (s *Store) LoadEmbeddings(ctx context.Context) (map[int64][]float32, error) {
	s.mu.RLock()
	if s.embCacheReady {
		result := s.embCache
		s.mu.RUnlock()
		return result, nil
	}
	s.mu.RUnlock()

	// Cache miss — load from DB under write lock.
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock.
	if s.embCacheReady {
		return s.embCache, nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT fe.fact_id, fe.embedding
		 FROM fact_embeddings fe
		 JOIN facts f ON f.id = fe.fact_id
		 WHERE f.active = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64][]float32)
	for rows.Next() {
		var factID int64
		var blob []byte
		if err := rows.Scan(&factID, &blob); err != nil {
			return nil, err
		}
		result[factID] = blobToFloat32s(blob)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	s.embCache = result
	s.embCacheReady = true
	return result, nil
}

// PendingEmbeddings returns active facts that don't have embeddings yet.
// Used by the dreaming cycle to retry failed embeddings. Returns up to limit facts
// ordered by importance (most important first).
func (s *Store) PendingEmbeddings(ctx context.Context, limit int) ([]Fact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	return s.queryFacts(ctx,
		`SELECT f.* FROM facts f
		 LEFT JOIN fact_embeddings fe ON fe.fact_id = f.id
		 WHERE f.active = 1 AND fe.fact_id IS NULL
		 ORDER BY f.importance DESC
		 LIMIT ?`, limit)
}

// RetryPendingEmbeddings embeds facts that are missing embeddings using batch API.
// Returns the number of successfully embedded facts.
// Designed to run during dreaming Phase 0 as a best-effort recovery.
func (s *Store) RetryPendingEmbeddings(ctx context.Context, batchEmbedFn func(ctx context.Context, facts []struct{ ID int64; Content string }) (int, error)) (int, error) {
	pending, err := s.PendingEmbeddings(ctx, 50)
	if err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}

	batch := make([]struct{ ID int64; Content string }, len(pending))
	for i, f := range pending {
		batch[i] = struct{ ID int64; Content string }{ID: f.ID, Content: f.Content}
	}

	return batchEmbedFn(ctx, batch)
}

// LoadEmbeddingsForMerge returns embeddings, merge depths, and categories for
// active facts eligible for merging. Facts with merge_depth >= maxDepth are
// excluded to prevent cascading merges. Categories are returned so callers can
// restrict comparisons to same-category pairs, reducing O(n²) to O(n²/k).
func (s *Store) LoadEmbeddingsForMerge(ctx context.Context, maxDepth int) (map[int64][]float32, map[int64]int, map[int64]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT fe.fact_id, fe.embedding, f.merge_depth, f.category
		 FROM fact_embeddings fe
		 JOIN facts f ON f.id = fe.fact_id
		 WHERE f.active = 1 AND f.merge_depth < ?`, maxDepth)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()

	embeddings := make(map[int64][]float32)
	depths := make(map[int64]int)
	categories := make(map[int64]string)
	for rows.Next() {
		var factID int64
		var blob []byte
		var depth int
		var category string
		if err := rows.Scan(&factID, &blob, &depth, &category); err != nil {
			return nil, nil, nil, err
		}
		embeddings[factID] = blobToFloat32s(blob)
		depths[factID] = depth
		categories[factID] = category
	}
	return embeddings, depths, categories, rows.Err()
}
