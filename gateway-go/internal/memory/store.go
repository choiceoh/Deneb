// store.go — Structured memory store backed by SQLite.
// Replaces append-only MEMORY.md with fact-level granularity,
// importance scoring, and category-based organization.
// Inspired by Honcho's inference-layer memory architecture.
//
// Sub-files:
//   store_facts.go      — fact CRUD (InsertFact … ActiveFactCount)
//   store_meta.go       — user model, dreaming log, metadata
//   store_embeddings.go — embedding storage and loading
//   store_export.go     — ExportToMarkdown, ExportToFile, ImportFromMarkdown
package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
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
	DurationMs        int64     `json:"duration_ms"`
}

// Store is the structured memory database.
type Store struct {
	db       *sql.DB
	mu       sync.RWMutex
	reranker RerankFunc // optional cross-encoder reranker (nil = disabled)
	logger   *slog.Logger

	// In-memory embedding cache: avoids full table scan on every search.
	// Populated on first LoadEmbeddings call, invalidated on mutations.
	embCache      map[int64][]float32
	embCacheReady bool
}

// schema v1 for the memory store.
const schemaSQL = `
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS facts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	content TEXT NOT NULL,
	category TEXT NOT NULL DEFAULT 'context',
	importance REAL NOT NULL DEFAULT 0.5,
	source TEXT DEFAULT 'auto_extract',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	last_accessed_at TEXT,
	access_count INTEGER NOT NULL DEFAULT 0,
	verified_at TEXT,
	expires_at TEXT,
	superseded_by INTEGER REFERENCES facts(id),
	active INTEGER NOT NULL DEFAULT 1,
	merge_depth INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_facts_active ON facts(active);
CREATE INDEX IF NOT EXISTS idx_facts_category ON facts(category);
CREATE INDEX IF NOT EXISTS idx_facts_importance ON facts(importance DESC);
CREATE INDEX IF NOT EXISTS idx_facts_created ON facts(created_at DESC);

CREATE VIRTUAL TABLE IF NOT EXISTS facts_fts USING fts5(
	content,
	category,
	content=facts,
	content_rowid=id,
	tokenize='unicode61'
);

-- Trigram index for Korean/CJK substring matching (fallback when unicode61 misses).
CREATE VIRTUAL TABLE IF NOT EXISTS facts_fts_trigram USING fts5(
	content,
	content=facts,
	content_rowid=id,
	tokenize='trigram'
);

-- Triggers to keep FTS in sync with facts table.
CREATE TRIGGER IF NOT EXISTS facts_ai AFTER INSERT ON facts BEGIN
	INSERT INTO facts_fts(rowid, content, category)
	VALUES (new.id, new.content, new.category);
	INSERT INTO facts_fts_trigram(rowid, content)
	VALUES (new.id, new.content);
END;

CREATE TRIGGER IF NOT EXISTS facts_ad AFTER DELETE ON facts BEGIN
	INSERT INTO facts_fts(facts_fts, rowid, content, category)
	VALUES ('delete', old.id, old.content, old.category);
	INSERT INTO facts_fts_trigram(facts_fts_trigram, rowid, content)
	VALUES ('delete', old.id, old.content);
END;

CREATE TRIGGER IF NOT EXISTS facts_au AFTER UPDATE OF content, category ON facts BEGIN
	INSERT INTO facts_fts(facts_fts, rowid, content, category)
	VALUES ('delete', old.id, old.content, old.category);
	INSERT INTO facts_fts(rowid, content, category)
	VALUES (new.id, new.content, new.category);
	INSERT INTO facts_fts_trigram(facts_fts_trigram, rowid, content)
	VALUES ('delete', old.id, old.content);
	INSERT INTO facts_fts_trigram(rowid, content)
	VALUES (new.id, new.content);
END;

CREATE TABLE IF NOT EXISTS fact_embeddings (
	fact_id INTEGER PRIMARY KEY REFERENCES facts(id) ON DELETE CASCADE,
	embedding BLOB NOT NULL,
	model_name TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS user_model (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	confidence REAL NOT NULL DEFAULT 0.5,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS dreaming_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	ran_at TEXT NOT NULL,
	facts_verified INTEGER NOT NULL DEFAULT 0,
	facts_merged INTEGER NOT NULL DEFAULT 0,
	facts_expired INTEGER NOT NULL DEFAULT 0,
	patterns_extracted INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0
);

-- Metadata for tracking turn counts and other state.
CREATE TABLE IF NOT EXISTS metadata (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

// migrateSchema applies incremental schema changes for existing databases.
func migrateSchema(db *sql.DB) {
	// v1 → v2: add facts_pruned column to dreaming_log.
	_, _ = db.Exec(`ALTER TABLE dreaming_log ADD COLUMN facts_pruned INTEGER NOT NULL DEFAULT 0`)
	// v2 → v3: add merge_depth for cascade prevention in fact merging.
	_, _ = db.Exec(`ALTER TABLE facts ADD COLUMN merge_depth INTEGER NOT NULL DEFAULT 0`)
	// v3 → v4: explicit index on fact_embeddings.fact_id for DELETE CASCADE
	// performance. Older databases created before fact_id was the PRIMARY KEY
	// may not have an implicit B-tree index, causing O(N) scans on fact deletion.
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_fact_embeddings_fact_id ON fact_embeddings(fact_id)`)
}

// NewStore opens or creates a memory database at dbPath.
func NewStore(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("memory store: create dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("memory store: open db: %w", err)
	}

	// Single connection for WAL mode simplicity.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("memory store: init schema: %w", err)
	}

	// Schema migrations for existing databases.
	migrateSchema(db)

	store := &Store{db: db, logger: slog.Default()}

	// One-time compaction: clean accumulated low-quality noise on first upgrade.
	ctx := context.Background()
	if v, _ := store.GetMeta(ctx, "compaction_v1"); v == "" {
		if n, err := store.CompactMemory(ctx); err == nil && n > 0 {
			// Log to stderr since slog may not be available yet.
			fmt.Fprintf(os.Stderr, "aurora-memory: one-time compaction removed %d noise facts\n", n)
		}
		_ = store.SetMeta(ctx, "compaction_v1", "done")
	}

	return store, nil
}

// SetReranker configures an optional cross-encoder reranker for search results.
// When set, SearchFacts will rerank results after hybrid scoring.
func (s *Store) SetReranker(fn RerankFunc) {
	s.reranker = fn
}

// Close closes the database connection.
func (s *Store) Close() error {
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

// float32sToBlob converts a float32 slice to little-endian bytes.
// Matches the pattern in core-rs/vega/src/db/schema.rs for chunk_embeddings.
func float32sToBlob(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// blobToFloat32s converts little-endian bytes back to float32 slice.
func blobToFloat32s(blob []byte) []float32 {
	n := len(blob) / 4
	vec := make([]float32, n)
	for i := range n {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return vec
}

// cosineSimilarity computes cosine similarity between two float32 vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
