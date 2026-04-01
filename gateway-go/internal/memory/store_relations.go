// store_relations.go — Fact relation CRUD for the knowledge graph.
package memory

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Relation types for fact-to-fact edges.
const (
	RelationEvolves     = "evolves"
	RelationContradicts = "contradicts"
	RelationSupports    = "supports"
	RelationCauses      = "causes"
	RelationRelated     = "related"
)

// FactRelation represents a directed edge between two facts.
type FactRelation struct {
	ID           int64     `json:"id"`
	FromFactID   int64     `json:"from_fact_id"`
	ToFactID     int64     `json:"to_fact_id"`
	RelationType string    `json:"relation_type"`
	Confidence   float64   `json:"confidence"`
	CreatedAt    time.Time `json:"created_at"`
}

// RelatedFact pairs a fact with its relation metadata.
type RelatedFact struct {
	Fact         Fact   `json:"fact"`
	RelationType string `json:"relation_type"`
	Direction    string `json:"direction"` // "outgoing" or "incoming"
}

// InsertRelation creates a directed relation between two facts.
// Upserts on conflict (same from+to+type): updates confidence if higher.
func (s *Store) InsertRelation(ctx context.Context, fromID, toID int64, relationType string, confidence float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO fact_relations (from_fact_id, to_fact_id, relation_type, confidence, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(from_fact_id, to_fact_id, relation_type)
		 DO UPDATE SET confidence = MAX(fact_relations.confidence, excluded.confidence)`,
		fromID, toID, relationType, confidence, now,
	)
	if err != nil {
		return fmt.Errorf("insert relation: %w", err)
	}
	return nil
}

// GetRelatedFacts returns all facts related to a given fact, with relation info.
// Includes both outgoing (from_fact_id = factID) and incoming (to_fact_id = factID) edges.
func (s *Store) GetRelatedFacts(ctx context.Context, factID int64) ([]RelatedFact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT r.relation_type, 'outgoing', f.*
		 FROM fact_relations r
		 JOIN facts f ON f.id = r.to_fact_id
		 WHERE r.from_fact_id = ? AND f.active = 1
		 UNION ALL
		 SELECT r.relation_type, 'incoming', f.*
		 FROM fact_relations r
		 JOIN facts f ON f.id = r.from_fact_id
		 WHERE r.to_fact_id = ? AND f.active = 1
		 ORDER BY relation_type`,
		factID, factID,
	)
	if err != nil {
		return nil, fmt.Errorf("get related facts: %w", err)
	}
	defer rows.Close()

	var results []RelatedFact
	for rows.Next() {
		var rf RelatedFact
		var createdAt, updatedAt string
		var lastAccessedAt, verifiedAt, expiresAt sql.NullString
		var supersededBy sql.NullInt64
		var activeInt int

		err := rows.Scan(
			&rf.RelationType, &rf.Direction,
			&rf.Fact.ID, &rf.Fact.Content, &rf.Fact.Category, &rf.Fact.Importance, &rf.Fact.Source,
			&createdAt, &updatedAt, &lastAccessedAt,
			&rf.Fact.AccessCount, &verifiedAt, &expiresAt,
			&supersededBy, &activeInt, &rf.Fact.MergeDepth,
		)
		if err != nil {
			return nil, fmt.Errorf("scan related fact: %w", err)
		}

		rf.Fact.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		rf.Fact.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		rf.Fact.Active = activeInt == 1
		if lastAccessedAt.Valid {
			t, _ := time.Parse(time.RFC3339, lastAccessedAt.String)
			rf.Fact.LastAccessedAt = &t
		}
		if verifiedAt.Valid {
			t, _ := time.Parse(time.RFC3339, verifiedAt.String)
			rf.Fact.VerifiedAt = &t
		}
		if expiresAt.Valid {
			t, _ := time.Parse(time.RFC3339, expiresAt.String)
			rf.Fact.ExpiresAt = &t
		}
		if supersededBy.Valid {
			rf.Fact.SupersededBy = &supersededBy.Int64
		}

		results = append(results, rf)
	}
	return results, rows.Err()
}

// GetRelationChain follows a chain of relations of a given type starting from factID.
// For example, following "evolves" edges traces how a fact changed over time.
// Max depth 5 to prevent runaway queries.
func (s *Store) GetRelationChain(ctx context.Context, factID int64, relationType string, maxDepth int) ([]Fact, error) {
	if maxDepth <= 0 || maxDepth > 5 {
		maxDepth = 5
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var chain []Fact
	visited := map[int64]bool{factID: true}
	currentID := factID

	for range maxDepth {
		var nextID int64
		err := s.db.QueryRowContext(ctx,
			`SELECT to_fact_id FROM fact_relations
			 WHERE from_fact_id = ? AND relation_type = ?
			 LIMIT 1`,
			currentID, relationType,
		).Scan(&nextID)
		if err != nil {
			break // no more edges
		}
		if visited[nextID] {
			break // cycle
		}
		visited[nextID] = true

		f, err := s.scanFact(ctx, `SELECT * FROM facts WHERE id = ?`, nextID)
		if err != nil {
			break
		}
		chain = append(chain, *f)
		currentID = nextID
	}

	return chain, nil
}

// resolveRelations finds the best matching existing fact for a new fact's relation_type
// and creates the relation. Uses same-category + same-entity overlap as signals.
// Called from post-processing after fact insertion. Best-effort: errors are logged, not fatal.
func (s *Store) resolveRelations(ctx context.Context, newFactID int64, ef ExtractedFact, logger *slog.Logger) {
	if ef.RelationType == "" {
		return
	}

	// Find candidate facts with overlapping entities in the same category.
	var candidateID int64
	if len(ef.Entities) > 0 {
		s.mu.RLock()
		for _, entityName := range ef.Entities {
			err := s.db.QueryRowContext(ctx,
				`SELECT f.id FROM facts f
				 JOIN fact_entities fe ON fe.fact_id = f.id
				 JOIN entities e ON e.id = fe.entity_id
				 WHERE e.name = ? AND f.category = ? AND f.active = 1 AND f.id != ?
				 ORDER BY f.created_at DESC
				 LIMIT 1`,
				entityName, ef.Category, newFactID,
			).Scan(&candidateID)
			if err == nil {
				break
			}
		}
		s.mu.RUnlock()
	}

	// Fallback: FTS to find a similar fact in the same category.
	if candidateID == 0 {
		ftsQuery := escapeFTS(ef.Content)
		if ftsQuery != "" {
			s.mu.RLock()
			_ = s.db.QueryRowContext(ctx,
				`SELECT f.id FROM facts_fts fts
				 JOIN facts f ON f.id = fts.rowid
				 WHERE facts_fts MATCH ? AND f.active = 1 AND f.category = ? AND f.id != ?
				 ORDER BY fts.rank
				 LIMIT 1`,
				ftsQuery, ef.Category, newFactID,
			).Scan(&candidateID)
			s.mu.RUnlock()
		}
	}

	if candidateID == 0 {
		return
	}

	// Insert the relation via the locked InsertRelation method.
	if err := s.InsertRelation(ctx, candidateID, newFactID, ef.RelationType, 0.7); err != nil {
		logger.Debug("resolve relation: insert failed", "error", err)
	}
}
