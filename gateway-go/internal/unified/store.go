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
	"sync"

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

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("unified store: init schema: %w", err)
	}

	// Run entity constraint migration for existing databases that predate
	// the 'unknown' entity_type. SQLite cannot ALTER CHECK constraints, so
	// we recreate the table. Idempotent — no-op if constraint is correct.
	migrateEntityConstraint(db)

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
