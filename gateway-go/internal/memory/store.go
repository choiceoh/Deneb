// store.go — Structured memory store backed by SQLite.
// Replaces append-only MEMORY.md with fact-level granularity,
// importance scoring, and category-based organization.
// Inspired by Honcho's inference-layer memory architecture.
//
// Sub-files:
//
//	store_facts.go      — fact CRUD (InsertFact … ActiveFactCount)
//	store_meta.go       — user model, dreaming log, metadata
//	store_export.go     — ExportToMarkdown, ExportToFile, ImportFromMarkdown
package memory

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"
)

// Fact categories matching Honcho's structured memory model.
const (
	CategoryDecision   = "decision"
	CategoryPreference = "preference"
	CategorySolution   = "solution"
	CategoryContext    = "context"
	CategoryUserModel  = "user_model"
	CategoryMutual     = "mutual" // 상호 인식: AI-user relationship dynamics
)

// Fact sources.
const (
	SourceAutoExtract    = "auto_extract"
	SourceDreaming       = "dreaming"
	SourceManual         = "manual"
	SourceAuroraTransfer = "aurora_transfer" // graduated from Aurora compaction summaries
)

// ExportMinImportance is the minimum importance for a fact to appear in MEMORY.md.
const ExportMinImportance = 0.7

// Fact represents a single stored memory fact.
type Fact struct {
	ID             int64      `json:"id"`
	Content        string     `json:"content"`
	Category       string     `json:"category"`
	Importance     float64    `json:"importance"`
	Source         string     `json:"source"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	LastAccessedAt *time.Time `json:"last_accessed_at,omitempty"`
	AccessCount    int        `json:"access_count"`
	VerifiedAt     *time.Time `json:"verified_at,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	SupersededBy   *int64     `json:"superseded_by,omitempty"`
	Active         bool       `json:"active"`
	MergeDepth     int        `json:"merge_depth"`
}

// UserModelEntry is a key-value pair in the user model table.
type UserModelEntry struct {
	Key        string    `json:"key"`
	Value      string    `json:"value"`
	Confidence float64   `json:"confidence"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// DreamingLogEntry records a dreaming cycle execution.
type DreamingLogEntry struct {
	ID                int64     `json:"id"`
	RanAt             time.Time `json:"ran_at"`
	FactsVerified     int       `json:"facts_verified"`
	FactsMerged       int       `json:"facts_merged"`
	FactsExpired      int       `json:"facts_expired"`
	FactsPruned       int       `json:"facts_pruned"`
	PatternsExtracted int       `json:"patterns_extracted"`
	UserModelUpdated  int       `json:"user_model_updated"`
	MutualUpdated     int       `json:"mutual_updated"`
	DurationMs        int64     `json:"duration_ms"`
}

// Store is the structured memory database.
type Store struct {
	db     *sql.DB
	mu     sync.RWMutex
	logger *slog.Logger
	shared bool // true when DB is owned by unified store (don't close)

	// onFactMutate is called when high-importance facts are inserted/updated/deleted.
	// Used to invalidate the Tier-1 cache so new facts appear in the system prompt
	// immediately rather than waiting for the 5-minute cache TTL.
	onFactMutate func()

	// params overrides hardcoded search scoring constants when set.
	// Used by benchmark tests for autoresearch parameter optimization.
	// nil = use hardcoded defaults (zero production impact).
	params *SearchParams
}

// SetSearchParams sets optional scoring parameter overrides.
// Must be called before any concurrent search operations.
// Pass nil to revert to hardcoded defaults.
func (s *Store) SetSearchParams(p *SearchParams) {
	s.params = p
}

// searchParams returns the active search params, falling back to defaults.
// Safe to call under RLock since params is set once at init time.
func (s *Store) searchParams() SearchParams {
	if s.params != nil {
		return *s.params
	}
	return DefaultSearchParams()
}

// GraphSchemaSQL is the knowledge graph DDL shared by memory and unified stores.
// Single source of truth: any schema change to these tables only needs to be
// made here. Both store.go (initial schema) and migration code reference this.
const GraphSchemaSQL = `
-- Fact relations (knowledge graph edges between facts).
CREATE TABLE IF NOT EXISTS fact_relations (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	from_fact_id  INTEGER NOT NULL REFERENCES facts(id) ON DELETE CASCADE,
	to_fact_id    INTEGER NOT NULL REFERENCES facts(id) ON DELETE CASCADE,
	relation_type TEXT NOT NULL CHECK(relation_type IN ('evolves','contradicts','supports','causes','related')),
	confidence    REAL NOT NULL DEFAULT 1.0,
	created_at    TEXT NOT NULL,
	UNIQUE(from_fact_id, to_fact_id, relation_type)
);

CREATE INDEX IF NOT EXISTS idx_relations_from ON fact_relations(from_fact_id);
CREATE INDEX IF NOT EXISTS idx_relations_to ON fact_relations(to_fact_id);

-- Named entities for object-centric fact grouping.
CREATE TABLE IF NOT EXISTS entities (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	name          TEXT NOT NULL UNIQUE,
	entity_type   TEXT NOT NULL DEFAULT 'unknown' CHECK(entity_type IN ('person','project','tool','system','concept','organization','unknown')),
	first_seen    TEXT NOT NULL,
	last_seen     TEXT NOT NULL,
	mention_count INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_entities_name ON entities(name);

-- Fact-entity associations with role context.
CREATE TABLE IF NOT EXISTS fact_entities (
	fact_id   INTEGER NOT NULL REFERENCES facts(id) ON DELETE CASCADE,
	entity_id INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
	role      TEXT NOT NULL DEFAULT 'mentioned',
	PRIMARY KEY (fact_id, entity_id)
);

CREATE INDEX IF NOT EXISTS idx_fact_entities_entity ON fact_entities(entity_id);
`

// GraphMigrateDDL returns individual DDL statements for incremental migration.
// Used by migrateSchema when the graph tables may not yet exist.
func GraphMigrateDDL() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS fact_relations (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			from_fact_id  INTEGER NOT NULL REFERENCES facts(id) ON DELETE CASCADE,
			to_fact_id    INTEGER NOT NULL REFERENCES facts(id) ON DELETE CASCADE,
			relation_type TEXT NOT NULL CHECK(relation_type IN ('evolves','contradicts','supports','causes','related')),
			confidence    REAL NOT NULL DEFAULT 1.0,
			created_at    TEXT NOT NULL,
			UNIQUE(from_fact_id, to_fact_id, relation_type)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_relations_from ON fact_relations(from_fact_id)`,
		`CREATE INDEX IF NOT EXISTS idx_relations_to ON fact_relations(to_fact_id)`,
		`CREATE TABLE IF NOT EXISTS entities (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			name          TEXT NOT NULL UNIQUE,
			entity_type   TEXT NOT NULL DEFAULT 'unknown' CHECK(entity_type IN ('person','project','tool','system','concept','organization','unknown')),
			first_seen    TEXT NOT NULL,
			last_seen     TEXT NOT NULL,
			mention_count INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_name ON entities(name)`,
		`CREATE TABLE IF NOT EXISTS fact_entities (
			fact_id   INTEGER NOT NULL REFERENCES facts(id) ON DELETE CASCADE,
			entity_id INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
			role      TEXT NOT NULL DEFAULT 'mentioned',
			PRIMARY KEY (fact_id, entity_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_fact_entities_entity ON fact_entities(entity_id)`,
	}
}

// NewStoreFromDB creates a memory store using a pre-opened database connection.
// Used by the unified store to share a single DB across subsystems.
// The caller owns the DB lifecycle — Close() on this store is a no-op.
func NewStoreFromDB(db *sql.DB) (*Store, error) {
	store := &Store{
		db:     db,
		logger: slog.Default(),
		shared: true,
	}
	return store, nil
}

// SetFactMutateCallback registers a function called when facts are mutated
// (insert with importance >= Tier1Threshold, update importance, deactivate, supersede).
// Typically wired to unified.InvalidateTier1Cache to keep the system prompt fresh.
func (s *Store) SetFactMutateCallback(fn func()) {
	s.onFactMutate = fn
}

// notifyFactMutate calls the onFactMutate callback if set.
// Must NOT be called while holding s.mu to avoid potential deadlocks.
func (s *Store) notifyFactMutate() {
	if s.onFactMutate != nil {
		s.onFactMutate()
	}
}

// Close closes the database connection.
func (s *Store) Close() error {
	if s.shared {
		return nil // owned by unified store
	}
	return s.db.Close()
}

// ── Internal helpers ─────────────────────────────────────────────────────────

func (s *Store) scanFact(_ context.Context, query string, args ...any) (*Fact, error) {
	row := s.db.QueryRow(query, args...)
	return scanFactRow(row)
}

func (s *Store) queryFacts(ctx context.Context, query string, args ...any) ([]Fact, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var facts []Fact
	for rows.Next() {
		f, err := scanFactRows(rows)
		if err != nil {
			return nil, err
		}
		facts = append(facts, *f)
	}
	return facts, rows.Err()
}

// scanFactRow scans a single fact from a *sql.Row.
func scanFactRow(row *sql.Row) (*Fact, error) {
	var f Fact
	var createdAt, updatedAt string
	var lastAccessedAt, verifiedAt, expiresAt sql.NullString
	var supersededBy sql.NullInt64
	var activeInt int

	err := row.Scan(
		&f.ID, &f.Content, &f.Category, &f.Importance, &f.Source,
		&createdAt, &updatedAt, &lastAccessedAt,
		&f.AccessCount, &verifiedAt, &expiresAt,
		&supersededBy, &activeInt, &f.MergeDepth,
	)
	if err != nil {
		return nil, err
	}

	f.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	f.Active = activeInt == 1

	if lastAccessedAt.Valid {
		t, _ := time.Parse(time.RFC3339, lastAccessedAt.String)
		f.LastAccessedAt = &t
	}
	if verifiedAt.Valid {
		t, _ := time.Parse(time.RFC3339, verifiedAt.String)
		f.VerifiedAt = &t
	}
	if expiresAt.Valid {
		t, _ := time.Parse(time.RFC3339, expiresAt.String)
		f.ExpiresAt = &t
	}
	if supersededBy.Valid {
		f.SupersededBy = &supersededBy.Int64
	}

	return &f, nil
}

// scanFactRows scans a single fact from *sql.Rows.
func scanFactRows(rows *sql.Rows) (*Fact, error) {
	var f Fact
	var createdAt, updatedAt string
	var lastAccessedAt, verifiedAt, expiresAt sql.NullString
	var supersededBy sql.NullInt64
	var activeInt int

	err := rows.Scan(
		&f.ID, &f.Content, &f.Category, &f.Importance, &f.Source,
		&createdAt, &updatedAt, &lastAccessedAt,
		&f.AccessCount, &verifiedAt, &expiresAt,
		&supersededBy, &activeInt, &f.MergeDepth,
	)
	if err != nil {
		return nil, err
	}

	f.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	f.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	f.Active = activeInt == 1

	if lastAccessedAt.Valid {
		t, _ := time.Parse(time.RFC3339, lastAccessedAt.String)
		f.LastAccessedAt = &t
	}
	if verifiedAt.Valid {
		t, _ := time.Parse(time.RFC3339, verifiedAt.String)
		f.VerifiedAt = &t
	}
	if expiresAt.Valid {
		t, _ := time.Parse(time.RFC3339, expiresAt.String)
		f.ExpiresAt = &t
	}
	if supersededBy.Valid {
		f.SupersededBy = &supersededBy.Int64
	}

	return &f, nil
}

func nullTimeStr(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.UTC().Format(time.RFC3339), Valid: true}
}

