// store_entities.go — Entity CRUD for object-centric fact grouping.
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Entity types.
const (
	EntityPerson       = "person"
	EntityProject      = "project"
	EntityTool         = "tool"
	EntitySystem       = "system"
	EntityConcept      = "concept"
	EntityOrganization = "organization"
	EntityUnknown      = "unknown"
)

// Entity represents a named object that facts can be grouped around.
type Entity struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	EntityType   string    `json:"entity_type"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
	MentionCount int       `json:"mention_count"`
}

// EntityRelation represents a co-occurrence relationship between two entities.
type EntityRelation struct {
	EntityA       string `json:"entity_a"`
	EntityB       string `json:"entity_b"`
	CoOccurrences int    `json:"co_occurrences"`
}

// UpsertEntity creates or updates an entity.
// On conflict (same name), updates last_seen, increments mention_count,
// and upgrades entity_type from "unknown" if a more specific type is provided.
func (s *Store) UpsertEntity(ctx context.Context, name, entityType string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	if entityType == "" {
		entityType = EntityUnknown
	}

	result, err := s.db.ExecContext(ctx,
		`INSERT INTO entities (name, entity_type, first_seen, last_seen, mention_count)
		 VALUES (?, ?, ?, ?, 1)
		 ON CONFLICT(name) DO UPDATE SET
			last_seen = excluded.last_seen,
			mention_count = entities.mention_count + 1,
			entity_type = CASE
				WHEN entities.entity_type = 'unknown' AND excluded.entity_type != 'unknown'
				THEN excluded.entity_type
				ELSE entities.entity_type
			END`,
		name, entityType, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert entity: %w", err)
	}

	// Return the entity ID (either new or existing).
	id, err := result.LastInsertId()
	if err != nil || id == 0 {
		// ON CONFLICT path: look up the existing ID.
		err = s.db.QueryRowContext(ctx,
			`SELECT id FROM entities WHERE name = ?`, name,
		).Scan(&id)
		if err != nil {
			return 0, fmt.Errorf("upsert entity lookup: %w", err)
		}
	}
	return id, nil
}

// LinkFactEntity associates a fact with an entity.
// No-op on conflict (same fact_id + entity_id).
func (s *Store) LinkFactEntity(ctx context.Context, factID, entityID int64, role string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if role == "" {
		role = "mentioned"
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO fact_entities (fact_id, entity_id, role)
		 VALUES (?, ?, ?)
		 ON CONFLICT(fact_id, entity_id) DO NOTHING`,
		factID, entityID, role,
	)
	if err != nil {
		return fmt.Errorf("link fact entity: %w", err)
	}
	return nil
}

// GetFactsByEntity returns all active facts mentioning a specific entity name.
func (s *Store) GetFactsByEntity(ctx context.Context, entityName string) ([]Fact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.queryFacts(ctx,
		`SELECT f.* FROM facts f
		 JOIN fact_entities fe ON fe.fact_id = f.id
		 JOIN entities e ON e.id = fe.entity_id
		 WHERE e.name = ? AND f.active = 1
		 ORDER BY f.importance DESC, f.created_at DESC`,
		entityName,
	)
}

// GetEntity retrieves an entity by name.
func (s *Store) GetEntity(ctx context.Context, name string) (*Entity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var e Entity
	var firstSeen, lastSeen string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, entity_type, first_seen, last_seen, mention_count
		 FROM entities WHERE name = ?`, name,
	).Scan(&e.ID, &e.Name, &e.EntityType, &firstSeen, &lastSeen, &e.MentionCount)
	if err != nil {
		return nil, err
	}
	e.FirstSeen, _ = time.Parse(time.RFC3339, firstSeen)
	e.LastSeen, _ = time.Parse(time.RFC3339, lastSeen)
	return &e, nil
}

// GetEntityNetwork returns entity pairs and their co-occurrence counts.
// Two entities co-occur when they are linked to the same fact.
func (s *Store) GetEntityNetwork(ctx context.Context) ([]EntityRelation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT e1.name, e2.name, COUNT(*) as co_occurrences
		 FROM fact_entities fe1
		 JOIN fact_entities fe2 ON fe1.fact_id = fe2.fact_id AND fe1.entity_id < fe2.entity_id
		 JOIN entities e1 ON e1.id = fe1.entity_id
		 JOIN entities e2 ON e2.id = fe2.entity_id
		 JOIN facts f ON f.id = fe1.fact_id AND f.active = 1
		 GROUP BY fe1.entity_id, fe2.entity_id
		 HAVING co_occurrences >= 2
		 ORDER BY co_occurrences DESC
		 LIMIT 100`,
	)
	if err != nil {
		return nil, fmt.Errorf("entity network: %w", err)
	}
	defer rows.Close()

	var results []EntityRelation
	for rows.Next() {
		var er EntityRelation
		if err := rows.Scan(&er.EntityA, &er.EntityB, &er.CoOccurrences); err != nil {
			return nil, err
		}
		results = append(results, er)
	}
	return results, rows.Err()
}

// inferEntityType guesses the entity type from the name using simple heuristics.
// Returns "unknown" if no pattern matches.
func inferEntityType(name string) string {
	// Known tool/system patterns.
	knownTools := map[string]bool{
		"Go": true, "Rust": true, "Python": true, "SQLite": true, "PostgreSQL": true,
		"Redis": true, "Docker": true, "Kubernetes": true, "Terraform": true,
		"git": true, "npm": true, "cargo": true, "buf": true, "protoc": true,
	}
	if knownTools[name] {
		return EntityTool
	}

	// Names with slashes tend to be projects (e.g., "Fred/JOCA Cable").
	for _, ch := range name {
		if ch == '/' {
			return EntityProject
		}
	}

	return EntityUnknown
}

// processEntities extracts entity names from an extracted fact, upserts them,
// and links them to the stored fact. Best-effort: errors are logged, not fatal.
func (s *Store) processEntities(ctx context.Context, factID int64, ef ExtractedFact, logger *slog.Logger) {
	if len(ef.Entities) == 0 {
		return
	}

	for _, entityName := range ef.Entities {
		if entityName == "" {
			continue
		}

		entityType := inferEntityType(entityName)
		entityID, err := s.UpsertEntity(ctx, entityName, entityType)
		if err != nil {
			logger.Debug("process entities: upsert failed", "entity", entityName, "error", err)
			continue
		}

		if err := s.LinkFactEntity(ctx, factID, entityID, "mentioned"); err != nil {
			logger.Debug("process entities: link failed", "entity", entityName, "fact_id", factID, "error", err)
		}
	}
}
