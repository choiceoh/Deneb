// store_facts.go — Fact CRUD operations for the memory store.
package memory

import (
	"context"
	"fmt"
	"time"
)

// CompactMemory is a one-time bulk cleanup that deactivates all low-importance
// noise facts regardless of age. Same criteria as PruneNoiseFacts but without
// the age restriction. Safe: uses soft-delete (active = 0).
func (s *Store) CompactMemory(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		`UPDATE facts SET active = 0, updated_at = ?
		 WHERE active = 1
		   AND importance <= 0.6
		   AND category = ?
		   AND source = ?
		   AND access_count = 0
		   AND verified_at IS NULL`,
		now, CategoryContext, SourceAutoExtract,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// insertDedupJaccardThreshold is the Jaccard similarity above which a new fact
// is considered a semantic duplicate of an existing fact during insertion.
// Lower than search dedup (0.60) to catch paraphrased duplicates at write time.
const insertDedupJaccardThreshold = 0.55

// tier1Threshold mirrors unified.Tier1Threshold to avoid an import cycle.
const tier1Threshold = 0.85

// InsertFact stores a new fact and returns its ID.
// Two-stage dedup: (1) exact content match, (2) semantic similarity via FTS + Jaccard.
func (s *Store) InsertFact(ctx context.Context, f Fact) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	if f.Category == "" {
		f.Category = CategoryContext
	}
	if f.Importance <= 0 {
		f.Importance = 0.5
	}
	if f.Source == "" {
		f.Source = SourceAutoExtract
	}

	// Stage 1: exact content match.
	var existingID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM facts WHERE content = ? AND active = 1 LIMIT 1`,
		f.Content,
	).Scan(&existingID)
	if err == nil {
		if f.Importance > 0 {
			_, _ = s.db.ExecContext(ctx,
				`UPDATE facts SET importance = MAX(importance, ?), updated_at = ? WHERE id = ?`,
				f.Importance, now, existingID,
			)
			if f.Importance >= tier1Threshold {
				s.mu.Unlock()
				s.notifyFactMutate()
				s.mu.Lock()
			}
		}
		return existingID, nil
	}

	// Stage 2: semantic dedup — find similar facts in the same category via FTS,
	// then check Jaccard similarity to catch paraphrased duplicates.
	if dupID := s.findSemanticDuplicate(ctx, f.Content, f.Category); dupID > 0 {
		if f.Importance > 0 {
			_, _ = s.db.ExecContext(ctx,
				`UPDATE facts SET importance = MAX(importance, ?), updated_at = ? WHERE id = ?`,
				f.Importance, now, dupID,
			)
			if f.Importance >= tier1Threshold {
				s.mu.Unlock()
				s.notifyFactMutate()
				s.mu.Lock()
			}
		}
		return dupID, nil
	}

	result, err := s.db.ExecContext(ctx,
		`INSERT INTO facts (content, category, importance, source, created_at, updated_at, expires_at, merge_depth)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		f.Content, f.Category, f.Importance, f.Source,
		now, now, nullTimeStr(f.ExpiresAt), f.MergeDepth,
	)
	if err != nil {
		return 0, fmt.Errorf("insert fact: %w", err)
	}

	if f.Importance >= tier1Threshold {
		s.mu.Unlock()
		s.notifyFactMutate()
		s.mu.Lock()
	}

	return result.LastInsertId()
}

// findSemanticDuplicate checks if a semantically similar fact already exists
// in the same category. Returns the existing fact ID, or 0 if no duplicate found.
// Uses FTS to find candidates, then Jaccard similarity for precise comparison.
// Must be called with s.mu held.
func (s *Store) findSemanticDuplicate(ctx context.Context, content, category string) int64 {
	ftsQuery := escapeFTS(content)
	if ftsQuery == "" {
		return 0
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, f.content
		 FROM facts_fts fts
		 JOIN facts f ON f.id = fts.rowid
		 WHERE facts_fts MATCH ? AND f.active = 1 AND f.category = ?
		 ORDER BY fts.rank
		 LIMIT 10`,
		ftsQuery, category,
	)
	if err != nil {
		return 0
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var existingContent string
		if err := rows.Scan(&id, &existingContent); err != nil {
			continue
		}
		if JaccardTextSimilarity(content, existingContent) >= insertDedupJaccardThreshold {
			return id
		}
	}
	return 0
}

// GetFact retrieves a fact by ID and increments its access count.
func (s *Store) GetFact(ctx context.Context, id int64) (*Fact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.ExecContext(ctx,
		`UPDATE facts SET access_count = access_count + 1, last_accessed_at = ? WHERE id = ?`,
		now, id,
	)

	return s.scanFact(ctx, `SELECT * FROM facts WHERE id = ?`, id)
}

// GetFactReadOnly retrieves a fact by ID without updating access counts.
// Use for internal operations (dreaming, merging) that shouldn't inflate access stats.
func (s *Store) GetFactReadOnly(ctx context.Context, id int64) (*Fact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scanFact(ctx, `SELECT * FROM facts WHERE id = ?`, id)
}

// GetActiveFacts returns all active facts, ordered by importance desc.
func (s *Store) GetActiveFacts(ctx context.Context) ([]Fact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.queryFacts(ctx,
		`SELECT * FROM facts WHERE active = 1 ORDER BY importance DESC, created_at DESC`)
}

// GetActiveFactsAboveImportance returns active facts at or above a minimum importance score.
func (s *Store) GetActiveFactsAboveImportance(ctx context.Context, minImportance float64) ([]Fact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.queryFacts(ctx,
		`SELECT * FROM facts WHERE active = 1 AND importance >= ? ORDER BY importance DESC, created_at DESC`,
		minImportance)
}

// GetFactsByCategory returns active facts of a given category.
func (s *Store) GetFactsByCategory(ctx context.Context, category string) ([]Fact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.queryFacts(ctx,
		`SELECT * FROM facts WHERE active = 1 AND category = ? ORDER BY importance DESC`, category)
}

// BrowseFacts returns active facts with optional category filter, paginated and sorted.
// sortOrder: "importance" (default), "recent" (updated_at DESC), "created" (created_at DESC).
// Returns (facts, totalCount, error) where totalCount is the unfiltered count for pagination.
func (s *Store) BrowseFacts(ctx context.Context, category, sortOrder string, limit, offset int) ([]Fact, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Determine ORDER BY clause.
	var orderBy string
	switch sortOrder {
	case "recent":
		orderBy = "updated_at DESC, importance DESC"
	case "created":
		orderBy = "created_at DESC, importance DESC"
	default: // "importance"
		orderBy = "importance DESC, created_at DESC"
	}

	// Count total matching facts for pagination.
	var total int
	if category != "" {
		err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM facts WHERE active = 1 AND category = ?`, category).Scan(&total)
		if err != nil {
			return nil, 0, fmt.Errorf("browse count: %w", err)
		}
	} else {
		err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM facts WHERE active = 1`).Scan(&total)
		if err != nil {
			return nil, 0, fmt.Errorf("browse count: %w", err)
		}
	}

	// Fetch paginated results.
	var query string
	var args []any
	if category != "" {
		query = fmt.Sprintf(`SELECT * FROM facts WHERE active = 1 AND category = ? ORDER BY %s LIMIT ? OFFSET ?`, orderBy)
		args = []any{category, limit, offset}
	} else {
		query = fmt.Sprintf(`SELECT * FROM facts WHERE active = 1 ORDER BY %s LIMIT ? OFFSET ?`, orderBy)
		args = []any{limit, offset}
	}

	facts, err := s.queryFacts(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("browse facts: %w", err)
	}
	return facts, total, nil
}

// GetFactsForDreaming returns active facts not verified in the last 24 hours.
func (s *Store) GetFactsForDreaming(ctx context.Context) ([]Fact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cutoff := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	return s.queryFacts(ctx,
		`SELECT * FROM facts WHERE active = 1 AND (verified_at IS NULL OR verified_at < ?)
		 ORDER BY created_at ASC LIMIT 500`, cutoff)
}

// UpdateImportance sets a fact's importance score.
func (s *Store) UpdateImportance(ctx context.Context, id int64, importance float64) error {
	s.mu.Lock()
	_, err := s.db.ExecContext(ctx,
		`UPDATE facts SET importance = ?, updated_at = ? WHERE id = ?`,
		importance, time.Now().UTC().Format(time.RFC3339), id,
	)
	s.mu.Unlock()
	if err == nil && importance >= tier1Threshold {
		s.notifyFactMutate()
	}
	return err
}

// MarkVerified updates the verified_at timestamp for a fact.
func (s *Store) MarkVerified(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE facts SET verified_at = ?, updated_at = ? WHERE id = ?`,
		now, now, id,
	)
	return err
}

// DeactivateFact marks a fact as inactive.
func (s *Store) DeactivateFact(ctx context.Context, id int64) error {
	s.mu.Lock()
	_, err := s.db.ExecContext(ctx,
		`UPDATE facts SET active = 0, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err == nil {
		s.embCacheReady = false
	}
	s.mu.Unlock()
	if err == nil {
		s.notifyFactMutate()
	}
	return err
}

// CleanupExpired deactivates all facts whose expires_at is in the past.
// Returns the number of expired facts.
func (s *Store) CleanupExpired(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		`UPDATE facts SET active = 0, updated_at = ?
		 WHERE active = 1 AND expires_at IS NOT NULL AND expires_at < ?`,
		now, now,
	)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if n > 0 {
		s.embCacheReady = false
	}
	return n, err
}

// PruneNoiseFacts deactivates low-quality noise facts matching ALL criteria:
// context category, auto_extract source, importance <= maxImportance,
// older than maxAge, never accessed, and never verified by dreaming.
func (s *Store) PruneNoiseFacts(ctx context.Context, maxImportance float64, maxAge time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	cutoff := now.Add(-maxAge).Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		`UPDATE facts SET active = 0, updated_at = ?
		 WHERE active = 1
		   AND importance <= ?
		   AND category = ?
		   AND source = ?
		   AND created_at < ?
		   AND access_count = 0
		   AND verified_at IS NULL`,
		now.Format(time.RFC3339), maxImportance, CategoryContext, SourceAutoExtract, cutoff,
	)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if n > 0 {
		s.embCacheReady = false
	}
	return n, err
}

// SupersedeFact marks oldID as superseded by newID and deactivates it.
// Also records an "evolves" relation in the knowledge graph.
func (s *Store) SupersedeFact(ctx context.Context, oldID, newID int64) error {
	s.mu.Lock()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE facts SET active = 0, superseded_by = ?, updated_at = ? WHERE id = ?`,
		newID, now, oldID,
	)
	if err == nil {
		s.embCacheReady = false
		// Record the supersede as an "evolves" relation (best-effort).
		_, _ = s.db.ExecContext(ctx,
			`INSERT INTO fact_relations (from_fact_id, to_fact_id, relation_type, confidence, created_at)
			 VALUES (?, ?, ?, 1.0, ?)
			 ON CONFLICT(from_fact_id, to_fact_id, relation_type) DO NOTHING`,
			oldID, newID, RelationEvolves, now,
		)
	}
	s.mu.Unlock()
	if err == nil {
		s.notifyFactMutate()
	}
	return err
}

// ActiveFactCount returns the number of active facts.
func (s *Store) ActiveFactCount(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM facts WHERE active = 1`).Scan(&count)
	return count, err
}

// CategoryCounts returns the number of active facts per category.
func (s *Store) CategoryCounts(ctx context.Context) (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.QueryContext(ctx,
		`SELECT category, COUNT(*) FROM facts WHERE active = 1 GROUP BY category ORDER BY COUNT(*) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var cat string
		var n int
		if err := rows.Scan(&cat, &n); err != nil {
			return nil, err
		}
		counts[cat] = n
	}
	return counts, rows.Err()
}

// Tier1FactCount returns the number of active facts eligible for always-on prompt injection.
func (s *Store) Tier1FactCount(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM facts WHERE active = 1 AND importance >= ? AND category IN ('decision','preference','user_model')`,
		tier1Threshold,
	).Scan(&count)
	return count, err
}

// EmbeddingCoverage returns (embedded count, total active count) for active facts.
func (s *Store) EmbeddingCoverage(ctx context.Context) (int, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var embedded, total int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM facts WHERE active = 1`).Scan(&total)
	if err != nil {
		return 0, 0, err
	}
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM fact_embeddings fe JOIN facts f ON f.id = fe.fact_id WHERE f.active = 1`).Scan(&embedded)
	if err != nil {
		return 0, 0, err
	}
	return embedded, total, nil
}
