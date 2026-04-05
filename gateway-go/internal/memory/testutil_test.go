package memory

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// testSchemaSQL is the minimal DDL required for memory store tests.
// Production code uses the unified store schema (unified/schema.go);
// this is a test-only copy to avoid circular imports.
const testSchemaSQL = `
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
CREATE INDEX IF NOT EXISTS idx_facts_active_importance ON facts(active, importance DESC);
CREATE INDEX IF NOT EXISTS idx_facts_active_category ON facts(active, category, importance DESC);
CREATE INDEX IF NOT EXISTS idx_facts_active_created ON facts(active, created_at DESC);

CREATE VIRTUAL TABLE IF NOT EXISTS facts_fts USING fts5(
	content, category, content=facts, content_rowid=id, tokenize='unicode61'
);

CREATE VIRTUAL TABLE IF NOT EXISTS facts_fts_trigram USING fts5(
	content, content=facts, content_rowid=id, tokenize='trigram'
);

CREATE TRIGGER IF NOT EXISTS facts_ai AFTER INSERT ON facts BEGIN
	INSERT INTO facts_fts(rowid, content, category) VALUES (new.id, new.content, new.category);
	INSERT INTO facts_fts_trigram(rowid, content) VALUES (new.id, new.content);
END;

CREATE TRIGGER IF NOT EXISTS facts_ad AFTER DELETE ON facts BEGIN
	INSERT INTO facts_fts(facts_fts, rowid, content, category) VALUES ('delete', old.id, old.content, old.category);
	INSERT INTO facts_fts_trigram(facts_fts_trigram, rowid, content) VALUES ('delete', old.id, old.content);
END;

CREATE TRIGGER IF NOT EXISTS facts_au AFTER UPDATE OF content, category ON facts BEGIN
	INSERT INTO facts_fts(facts_fts, rowid, content, category) VALUES ('delete', old.id, old.content, old.category);
	INSERT INTO facts_fts(rowid, content, category) VALUES (new.id, new.content, new.category);
	INSERT INTO facts_fts_trigram(facts_fts_trigram, rowid, content) VALUES ('delete', old.id, old.content);
	INSERT INTO facts_fts_trigram(rowid, content) VALUES (new.id, new.content);
END;

CREATE TABLE IF NOT EXISTS fact_embeddings (
	fact_id INTEGER PRIMARY KEY REFERENCES facts(id) ON DELETE CASCADE,
	embedding BLOB NOT NULL,
	model_name TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

` + GraphSchemaSQL + `

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
	facts_pruned INTEGER NOT NULL DEFAULT 0,
	patterns_extracted INTEGER NOT NULL DEFAULT 0,
	user_model_updated INTEGER NOT NULL DEFAULT 0,
	mutual_updated INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS metadata (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

// openTestDB creates a temp SQLite database with the memory schema for tests.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_memory.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(testSchemaSQL); err != nil {
		db.Close()
		t.Fatalf("init test schema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
