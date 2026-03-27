// store.go — Structured memory store backed by SQLite.
// Replaces append-only MEMORY.md with fact-level granularity,
// importance scoring, and category-based organization.
// Inspired by Honcho's inference-layer memory architecture.
package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
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
	SourceAutoExtract = "auto_extract"
	SourceDreaming    = "dreaming"
	SourceManual      = "manual"
)

// Fact represents a single stored memory fact.
type Fact struct {
	ID             int64     `json:"id"`
	Content        string    `json:"content"`
	Category       string    `json:"category"`
	Importance     float64   `json:"importance"`
	Source         string    `json:"source"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastAccessedAt *time.Time `json:"last_accessed_at,omitempty"`
	AccessCount    int       `json:"access_count"`
	VerifiedAt     *time.Time `json:"verified_at,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	SupersededBy   *int64    `json:"superseded_by,omitempty"`
	Active         bool      `json:"active"`
}

// UserModelEntry is a key-value pair in the user model table.
type UserModelEntry struct {
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// DreamingLogEntry records a dreaming cycle execution.
type DreamingLogEntry struct {
	ID                int64     `json:"id"`
	RanAt             time.Time `json:"ran_at"`
	FactsVerified     int       `json:"facts_verified"`
	FactsMerged       int       `json:"facts_merged"`
	FactsExpired      int       `json:"facts_expired"`
	PatternsExtracted int       `json:"patterns_extracted"`
	DurationMs        int64     `json:"duration_ms"`
}

// Store is the structured memory database.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
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
	active INTEGER NOT NULL DEFAULT 1
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

	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// InsertFact stores a new fact and returns its ID.
// Checks for exact content duplicates before inserting.
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

	// Dedup: skip if an active fact with identical content exists.
	var existingID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM facts WHERE content = ? AND active = 1 LIMIT 1`,
		f.Content,
	).Scan(&existingID)
	if err == nil {
		// Exact duplicate exists — update importance if new one is higher.
		if f.Importance > 0 {
			_, _ = s.db.ExecContext(ctx,
				`UPDATE facts SET importance = MAX(importance, ?), updated_at = ? WHERE id = ?`,
				f.Importance, now, existingID,
			)
		}
		return existingID, nil
	}

	result, err := s.db.ExecContext(ctx,
		`INSERT INTO facts (content, category, importance, source, created_at, updated_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		f.Content, f.Category, f.Importance, f.Source,
		now, now, nullTimeStr(f.ExpiresAt),
	)
	if err != nil {
		return 0, fmt.Errorf("insert fact: %w", err)
	}
	return result.LastInsertId()
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

// GetActiveFacts returns all active facts, ordered by importance desc.
func (s *Store) GetActiveFacts(ctx context.Context) ([]Fact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.queryFacts(ctx,
		`SELECT * FROM facts WHERE active = 1 ORDER BY importance DESC, created_at DESC`)
}

// GetFactsByCategory returns active facts of a given category.
func (s *Store) GetFactsByCategory(ctx context.Context, category string) ([]Fact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.queryFacts(ctx,
		`SELECT * FROM facts WHERE active = 1 AND category = ? ORDER BY importance DESC`, category)
}

// GetFactsForDreaming returns active facts not verified in the last 24 hours.
func (s *Store) GetFactsForDreaming(ctx context.Context) ([]Fact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cutoff := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	return s.queryFacts(ctx,
		`SELECT * FROM facts WHERE active = 1 AND (verified_at IS NULL OR verified_at < ?)
		 ORDER BY created_at ASC`, cutoff)
}

// UpdateImportance sets a fact's importance score.
func (s *Store) UpdateImportance(ctx context.Context, id int64, importance float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE facts SET importance = ?, updated_at = ? WHERE id = ?`,
		importance, now, id,
	)
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
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE facts SET active = 0, updated_at = ? WHERE id = ?`,
		now, id,
	)
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
	return result.RowsAffected()
}

// SupersedeFact marks oldID as superseded by newID and deactivates it.
func (s *Store) SupersedeFact(ctx context.Context, oldID, newID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE facts SET active = 0, superseded_by = ?, updated_at = ? WHERE id = ?`,
		newID, now, oldID,
	)
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

// --- User Model ---

// SetUserModel upserts a user model entry.
func (s *Store) SetUserModel(ctx context.Context, key, value string, confidence float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_model (key, value, confidence, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, confidence = excluded.confidence, updated_at = excluded.updated_at`,
		key, value, confidence, now,
	)
	return err
}

// GetUserModel returns all user model entries.
func (s *Store) GetUserModel(ctx context.Context) ([]UserModelEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `SELECT key, value, confidence, updated_at FROM user_model ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []UserModelEntry
	for rows.Next() {
		var e UserModelEntry
		var updatedAt string
		if err := rows.Scan(&e.Key, &e.Value, &e.Confidence, &updatedAt); err != nil {
			return nil, err
		}
		e.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// --- Dreaming Log ---

// InsertDreamingLog records a dreaming cycle.
func (s *Store) InsertDreamingLog(ctx context.Context, entry DreamingLogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dreaming_log (ran_at, facts_verified, facts_merged, facts_expired, patterns_extracted, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		entry.RanAt.UTC().Format(time.RFC3339),
		entry.FactsVerified, entry.FactsMerged, entry.FactsExpired,
		entry.PatternsExtracted, entry.DurationMs,
	)
	return err
}

// --- Metadata ---

// GetMeta retrieves a metadata value.
func (s *Store) GetMeta(ctx context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var val string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key = ?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

// SetMeta upserts a metadata value.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO metadata (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

// --- Embeddings ---

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
	return err
}

// LoadEmbeddings loads all active fact embeddings for similarity search.
func (s *Store) LoadEmbeddings(ctx context.Context) (map[int64][]float32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

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
	return result, rows.Err()
}

// --- Export ---

// ExportToMarkdown generates MEMORY.md content from active facts.
func (s *Store) ExportToMarkdown(ctx context.Context) (string, error) {
	facts, err := s.GetActiveFacts(ctx)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("# Memory\n\nAuto-recorded learnings and decisions.\n\n")

	// Group by category for readability.
	categories := []string{CategoryDecision, CategoryPreference, CategorySolution, CategoryContext, CategoryUserModel, CategoryMutual}
	categoryNames := map[string]string{
		CategoryDecision:   "결정사항",
		CategoryPreference: "선호도",
		CategorySolution:   "해결방법",
		CategoryContext:    "맥락",
		CategoryUserModel:  "사용자 모델",
		CategoryMutual:     "상호 인식",
	}

	for _, cat := range categories {
		var catFacts []Fact
		for _, f := range facts {
			if f.Category == cat {
				catFacts = append(catFacts, f)
			}
		}
		if len(catFacts) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("## %s\n\n", categoryNames[cat]))
		for _, f := range catFacts {
			date := f.CreatedAt.Format("2006-01-02")
			sb.WriteString(fmt.Sprintf("- [%.1f] %s (%s)\n", f.Importance, f.Content, date))
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// ExportToFile writes the markdown export to MEMORY.md in the given directory.
func (s *Store) ExportToFile(ctx context.Context, dir string) error {
	content, err := s.ExportToMarkdown(ctx)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(content), 0o644)
}

// ImportFromMarkdown parses a legacy MEMORY.md file and imports its entries as facts.
// Handles the format produced by sglang_hooks.go: "## YYYY-MM-DD HH:MM\n\n- bullet\n- bullet\n"
// Returns the number of imported facts.
func (s *Store) ImportFromMarkdown(ctx context.Context, path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("import memory: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	imported := 0
	var currentDate string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Parse date header: "## 2026-01-15 14:30"
		if strings.HasPrefix(line, "## ") {
			currentDate = strings.TrimPrefix(line, "## ")
			continue
		}

		// Parse bullet entries: "- fact content"
		if (strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ")) && len(line) > 3 {
			content := strings.TrimPrefix(strings.TrimPrefix(line, "- "), "* ")
			if content == "" {
				continue
			}

			fact := Fact{
				Content:    content,
				Category:   CategoryContext,
				Importance: 0.5,
				Source:      "migration",
			}

			// Try to parse the date for created_at.
			if currentDate != "" {
				if t, err := time.Parse("2006-01-02 15:04", currentDate); err == nil {
					fact.CreatedAt = t
				}
			}

			if _, err := s.InsertFact(ctx, fact); err == nil {
				imported++
			}
		}
	}

	return imported, nil
}

// --- Internal helpers ---

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
		&supersededBy, &activeInt,
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
		&supersededBy, &activeInt,
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
