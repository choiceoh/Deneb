// Aurora context store backed by SQLite (WAL mode).
//
// Manages context_items, messages, summaries, and compaction_events
// that power the Rust Aurora hierarchical compaction engine via FFI.
// Migrated from single-file JSON persistence to SQLite for indexed
// lookups and transactional consistency.
// Optimized for single-user deployment (no concurrent access concerns).
package aurora

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// maxCompactionEvents is the maximum number of compaction events retained.
// Older entries are pruned to prevent unbounded growth.
const maxCompactionEvents = 500

// Store is the Aurora context store (SQLite-backed).
type Store struct {
	mu     sync.RWMutex
	db     *sql.DB
	dbPath string
	logger *slog.Logger

	closeOnce sync.Once
	closeErr  error
}

// CompactionEvent is a persisted compaction event record.
type CompactionEvent struct {
	ConversationID   uint64 `json:"conversationId"`
	Pass             string `json:"pass"`
	Level            string `json:"level"`
	TokensBefore     uint64 `json:"tokensBefore"`
	TokensAfter      uint64 `json:"tokensAfter"`
	CreatedSummaryID string `json:"createdSummaryId"`
	CreatedAt        int64  `json:"createdAt"`
}

// StoreConfig configures the Aurora store.
type StoreConfig struct {
	// DatabasePath is the SQLite database file path.
	// Default: ~/.deneb/aurora.db
	DatabasePath string `json:"databasePath"`
}

// DefaultStoreConfig returns production defaults for single-user DGX Spark.
func DefaultStoreConfig() StoreConfig {
	home, _ := os.UserHomeDir()
	return StoreConfig{
		DatabasePath: filepath.Join(home, ".deneb", "aurora.db"),
	}
}

// ── Data types matching Rust core-rs types ──────────────────────────────────

// ContextItem corresponds to core-rs compaction::ContextItem.
type ContextItem struct {
	ConversationID uint64  `json:"conversationId"`
	Ordinal        uint64  `json:"ordinal"`
	ItemType       string  `json:"itemType"` // "message" or "summary"
	MessageID      *uint64 `json:"messageId,omitempty"`
	SummaryID      *string `json:"summaryId,omitempty"`
	CreatedAt      int64   `json:"createdAt"` // epoch ms
}

// MessageRecord corresponds to core-rs compaction::MessageRecord.
type MessageRecord struct {
	MessageID      uint64 `json:"messageId"`
	ConversationID uint64 `json:"conversationId"`
	Seq            uint64 `json:"seq"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	TokenCount     uint64 `json:"tokenCount"`
	CreatedAt      int64  `json:"createdAt"` // epoch ms
}

// SummaryRecord corresponds to core-rs compaction::SummaryRecord.
type SummaryRecord struct {
	SummaryID               string   `json:"summaryId"`
	ConversationID          uint64   `json:"conversationId"`
	Kind                    string   `json:"kind"` // "leaf" or "condensed"
	Depth                   uint32   `json:"depth"`
	Content                 string   `json:"content"`
	TokenCount              uint64   `json:"tokenCount"`
	FileIDs                 []string `json:"fileIds"`
	EarliestAt              *int64   `json:"earliestAt,omitempty"`
	LatestAt                *int64   `json:"latestAt,omitempty"`
	DescendantCount         uint64   `json:"descendantCount"`
	DescendantTokenCount    uint64   `json:"descendantTokenCount"`
	SourceMessageTokenCount uint64   `json:"sourceMessageTokenCount"`
	CreatedAt               int64    `json:"createdAt"` // epoch ms
}

// SummaryStats holds aggregate summary info for context assembly.
type SummaryStats struct {
	MaxDepth           uint32 `json:"maxDepth"`
	CondensedCount     int    `json:"condensedCount"`
	LeafCount          int    `json:"leafCount"`
	TotalSummaryTokens uint64 `json:"totalSummaryTokens"`
}

// PersistLeafInput matches core-rs sweep::PersistLeafInput.
type PersistLeafInput struct {
	SummaryID               string   `json:"summaryId"`
	ConversationID          uint64   `json:"conversationId"`
	Content                 string   `json:"content"`
	TokenCount              uint64   `json:"tokenCount"`
	FileIDs                 []string `json:"fileIds"`
	EarliestAt              *int64   `json:"earliestAt,omitempty"`
	LatestAt                *int64   `json:"latestAt,omitempty"`
	SourceMessageTokenCount uint64   `json:"sourceMessageTokenCount"`
	MessageIDs              []uint64 `json:"messageIds"`
	StartOrdinal            uint64   `json:"startOrdinal"`
	EndOrdinal              uint64   `json:"endOrdinal"`
}

// PersistCondensedInput matches core-rs sweep::PersistCondensedInput.
type PersistCondensedInput struct {
	SummaryID               string   `json:"summaryId"`
	ConversationID          uint64   `json:"conversationId"`
	Depth                   uint32   `json:"depth"`
	Content                 string   `json:"content"`
	TokenCount              uint64   `json:"tokenCount"`
	FileIDs                 []string `json:"fileIds"`
	EarliestAt              *int64   `json:"earliestAt,omitempty"`
	LatestAt                *int64   `json:"latestAt,omitempty"`
	DescendantCount         uint64   `json:"descendantCount"`
	DescendantTokenCount    uint64   `json:"descendantTokenCount"`
	SourceMessageTokenCount uint64   `json:"sourceMessageTokenCount"`
	ParentSummaryIDs        []string `json:"parentSummaryIds"`
	StartOrdinal            uint64   `json:"startOrdinal"`
	EndOrdinal              uint64   `json:"endOrdinal"`
}

// PersistEventInput matches core-rs sweep::PersistEventInput.
type PersistEventInput struct {
	ConversationID   uint64 `json:"conversationId"`
	Pass             string `json:"pass"`
	Level            string `json:"level"`
	TokensBefore     uint64 `json:"tokensBefore"`
	TokensAfter      uint64 `json:"tokensAfter"`
	CreatedSummaryID string `json:"createdSummaryId"`
}

// ── Schema ──────────────────────────────────────────────────────────────────

const schemaSQL = `
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;

-- Monotonic ID generators (replaces NextOrdinalVal / NextMessageID).
CREATE TABLE IF NOT EXISTS sequences (
	name  TEXT PRIMARY KEY,
	value INTEGER NOT NULL DEFAULT 0
);

-- Context items with composite index on (conversation_id, ordinal).
CREATE TABLE IF NOT EXISTS context_items (
	conversation_id INTEGER NOT NULL,
	ordinal         INTEGER NOT NULL,
	item_type       TEXT NOT NULL,  -- 'message' or 'summary'
	message_id      INTEGER,
	summary_id      TEXT,
	created_at      INTEGER NOT NULL,
	PRIMARY KEY (conversation_id, ordinal)
);

CREATE INDEX IF NOT EXISTS idx_ci_conv ON context_items(conversation_id);

-- Chat messages.
CREATE TABLE IF NOT EXISTS messages (
	message_id      INTEGER PRIMARY KEY,
	conversation_id INTEGER NOT NULL,
	seq             INTEGER NOT NULL,
	role            TEXT NOT NULL,
	content         TEXT NOT NULL,
	token_count     INTEGER NOT NULL,
	created_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_msg_conv ON messages(conversation_id);

-- Summaries (leaf and condensed).
CREATE TABLE IF NOT EXISTS summaries (
	summary_id                TEXT PRIMARY KEY,
	conversation_id           INTEGER NOT NULL,
	kind                      TEXT NOT NULL,  -- 'leaf' or 'condensed'
	depth                     INTEGER NOT NULL DEFAULT 0,
	content                   TEXT NOT NULL,
	token_count               INTEGER NOT NULL,
	file_ids                  TEXT NOT NULL DEFAULT '[]',  -- JSON array
	earliest_at               INTEGER,
	latest_at                 INTEGER,
	descendant_count          INTEGER NOT NULL DEFAULT 0,
	descendant_token_count    INTEGER NOT NULL DEFAULT 0,
	source_message_token_count INTEGER NOT NULL DEFAULT 0,
	created_at                INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sum_conv ON summaries(conversation_id);

-- Summary parent relationships (DAG).
CREATE TABLE IF NOT EXISTS summary_parents (
	summary_id TEXT NOT NULL,
	parent_id  TEXT NOT NULL,
	PRIMARY KEY (summary_id, parent_id)
);

-- Summary-to-message links.
CREATE TABLE IF NOT EXISTS summary_messages (
	summary_id TEXT NOT NULL,
	message_id INTEGER NOT NULL,
	PRIMARY KEY (summary_id, message_id)
);

-- Compaction event audit log.
CREATE TABLE IF NOT EXISTS compaction_events (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	conversation_id  INTEGER NOT NULL,
	pass             TEXT NOT NULL,
	level            TEXT NOT NULL,
	tokens_before    INTEGER NOT NULL,
	tokens_after     INTEGER NOT NULL,
	created_summary_id TEXT NOT NULL,
	created_at       INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ce_conv ON compaction_events(conversation_id);

-- Memory transfer tracking.
CREATE TABLE IF NOT EXISTS transferred_summaries (
	summary_id    TEXT PRIMARY KEY,
	transferred_at INTEGER NOT NULL
);
`

// ── Constructor ─────────────────────────────────────────────────────────────

// NewStore opens or creates an Aurora SQLite store.
// If a legacy JSON file (aurora.json) exists alongside the DB path,
// it is migrated automatically.
func NewStore(cfg StoreConfig, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}

	dir := filepath.Dir(cfg.DatabasePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("aurora store: mkdir %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("aurora store: open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("aurora store: init schema: %w", err)
	}

	s := &Store{
		db:     db,
		dbPath: cfg.DatabasePath,
		logger: logger,
	}

	// One-time migration from legacy JSON file.
	if err := s.migrateFromJSON(dir); err != nil {
		logger.Warn("aurora store: json migration failed, starting fresh", "error", err)
	}

	// Log current state.
	var itemCount, msgCount, sumCount int
	db.QueryRow(`SELECT COUNT(*) FROM context_items`).Scan(&itemCount)
	db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&msgCount)
	db.QueryRow(`SELECT COUNT(*) FROM summaries`).Scan(&sumCount)

	logger.Info("aurora store opened", "path", cfg.DatabasePath,
		"items", itemCount, "messages", msgCount, "summaries", sumCount)
	return s, nil
}

// Sync is a no-op for SQLite (WAL mode auto-checkpoints).
// Retained for API compatibility.
func (s *Store) Sync() error {
	return nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		if s.db != nil {
			s.closeErr = s.db.Close()
		}
	})
	return s.closeErr
}

// ── Sequences ──────────────────────────────────────────────────────────────

// nextSequence atomically increments and returns the next value for a named sequence.
// Caller must hold s.mu write lock.
func (s *Store) nextSequence(tx *sql.Tx, name string) (uint64, error) {
	// Ensure the row exists.
	tx.Exec(`INSERT OR IGNORE INTO sequences (name, value) VALUES (?, 0)`, name)
	var val uint64
	if err := tx.QueryRow(`SELECT value FROM sequences WHERE name = ?`, name).Scan(&val); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE sequences SET value = ? WHERE name = ?`, val+1, name); err != nil {
		return 0, err
	}
	return val, nil
}
