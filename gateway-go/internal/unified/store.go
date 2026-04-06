// Unified memory store — single SQLite DB (deneb.db) combining Aurora context
// (messages, summaries, compaction DAG) with structured long-term memory
// (facts, embeddings, user model, dreaming).
//
// Memory tiers:
//   - short: raw messages (protected fresh tail)
//   - medium: leaf/condensed summaries with structured sections
//   - long: extracted facts with importance, category, expiry
//
// The memory_index table provides a unified search layer across all tiers,
// backed by FTS5 indices and optional vector embeddings.
package unified

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store is the unified memory store backed by a single SQLite database.
// It provides access to all memory tiers (messages, summaries, facts)
// through a shared connection with WAL journaling.
type Store struct {
	mu     sync.RWMutex
	db     *sql.DB
	dbPath string
	logger *slog.Logger

	closeOnce sync.Once
	closeErr  error
}

// Config configures the unified store.
type Config struct {
	// DatabasePath is the path to the unified SQLite database.
	// Default: ~/.deneb/deneb.db
	DatabasePath string `json:"databasePath"`
}

// DefaultConfig returns production defaults for single-user DGX Spark.
func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		DatabasePath: filepath.Join(home, ".deneb", "deneb.db"),
	}
}

// New opens or creates a unified memory store.
func New(cfg Config, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}

	dir := filepath.Dir(cfg.DatabasePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("unified store: mkdir %s: %w", dir, err)
	}

	db, err := sql.Open("sqlite", cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("unified store: open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	// Retry schema init with backoff — another process (e.g. production
	// gateway) may hold a write lock on the shared deneb.db file.
	var schemaErr error
	for attempt := range 4 {
		if _, schemaErr = db.Exec(schemaSQL); schemaErr == nil {
			break
		}
		if !isSQLiteBusy(schemaErr) {
			db.Close()
			return nil, fmt.Errorf("unified store: init schema: %w", schemaErr)
		}
		backoff := time.Duration(1<<attempt) * time.Second // 1s, 2s, 4s
		logger.Warn("unified store: schema init locked, retrying",
			"attempt", attempt+1, "backoff", backoff, "error", schemaErr)
		time.Sleep(backoff)
	}
	if schemaErr != nil {
		db.Close()
		return nil, fmt.Errorf("unified store: init schema: %w", schemaErr)
	}

	// Run entity constraint migration for existing databases that predate
	// the 'unknown' entity_type. SQLite cannot ALTER CHECK constraints, so
	// we recreate the table. Idempotent — no-op if constraint is correct.
	migrateEntityConstraint(db)

	// Add user_model_updated and mutual_updated columns to dreaming_log
	// for databases that predate these columns.
	migrateDreamingLogColumns(db)

	// Add goal, next_steps, critical_context columns to summaries
	// for the structured compression template.
	migrateSummaryStructuredColumns(db)

	s := &Store{
		db:     db,
		dbPath: cfg.DatabasePath,
		logger: logger,
	}

	if err := s.repairMemoryIndex(); err != nil {
		logger.Warn("unified store: memory index repair failed", "error", err)
	}

	s.logStats()
	return s, nil
}

// DB returns the underlying database connection for use by subsystem
// adapters (aurora operations, memory operations). Callers must respect
// the store's locking protocol.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.db.Close()
	})
	return s.closeErr
}

// isSQLiteBusy returns true if the error is SQLITE_BUSY / "database is locked".
func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "SQLITE_BUSY")
}

// MessageRecord is a single row from the messages table.
type MessageRecord struct {
	MessageID      int64
	ConversationID int64
	Seq            int64
	Role           string
	Content        string
	TokenCount     int64
	CreatedAt      int64 // epoch ms
}

// RecentMessages returns the most recent `limit` messages for a conversation,
// ordered oldest-first (chronological). Used by RLM for fresh-tail loading.
func (s *Store) RecentMessages(convID uint64, limit int) ([]MessageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT message_id, conversation_id, seq, role, content, token_count, created_at
		FROM messages
		WHERE conversation_id = ?
		ORDER BY seq DESC
		LIMIT ?
	`
	rows, err := s.db.Query(query, convID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent messages: %w", err)
	}
	defer rows.Close()

	var msgs []MessageRecord
	for rows.Next() {
		var m MessageRecord
		if err := rows.Scan(&m.MessageID, &m.ConversationID, &m.Seq, &m.Role,
			&m.Content, &m.TokenCount, &m.CreatedAt); err != nil {
			continue
		}
		msgs = append(msgs, m)
	}
	// Reverse to chronological order (oldest first).
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// AllMessages returns all messages for a conversation in chronological order.
// Used by RLM to populate the Starlark REPL's context variable.
func (s *Store) AllMessages(convID uint64) ([]MessageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT message_id, conversation_id, seq, role, content, token_count, created_at
		FROM messages
		WHERE conversation_id = ?
		ORDER BY seq ASC
	`
	rows, err := s.db.Query(query, convID)
	if err != nil {
		return nil, fmt.Errorf("all messages: %w", err)
	}
	defer rows.Close()

	var msgs []MessageRecord
	for rows.Next() {
		var m MessageRecord
		if err := rows.Scan(&m.MessageID, &m.ConversationID, &m.Seq, &m.Role,
			&m.Content, &m.TokenCount, &m.CreatedAt); err != nil {
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (s *Store) logStats() {
	var msgCount, sumCount, factCount, activeFactCount, indexCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&msgCount)
	s.db.QueryRow(`SELECT COUNT(*) FROM summaries`).Scan(&sumCount)
	s.db.QueryRow(`SELECT COUNT(*) FROM facts`).Scan(&factCount)
	s.db.QueryRow(`SELECT COUNT(*) FROM facts WHERE active = 1`).Scan(&activeFactCount)
	s.db.QueryRow(`SELECT COUNT(*) FROM memory_index`).Scan(&indexCount)

	s.logger.Info("unified store opened",
		"path", s.dbPath,
		"messages", msgCount,
		"summaries", sumCount,
		"facts", factCount,
		"active_facts", activeFactCount,
		"indexed", indexCount,
	)
}
