package aurora

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// testSchemaSQL is the minimal DDL required for aurora store tests.
// Production code uses the unified store schema (unified/schema.go);
// this is a test-only copy to avoid circular imports.
const testSchemaSQL = `
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS sequences (
	name  TEXT PRIMARY KEY,
	value INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS context_items (
	conversation_id INTEGER NOT NULL,
	ordinal         INTEGER NOT NULL,
	item_type       TEXT NOT NULL,
	message_id      INTEGER,
	summary_id      TEXT,
	created_at      INTEGER NOT NULL,
	PRIMARY KEY (conversation_id, ordinal)
);

CREATE INDEX IF NOT EXISTS idx_ci_conv ON context_items(conversation_id);

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

CREATE TABLE IF NOT EXISTS summaries (
	summary_id                TEXT PRIMARY KEY,
	conversation_id           INTEGER NOT NULL,
	kind                      TEXT NOT NULL,
	depth                     INTEGER NOT NULL DEFAULT 0,
	content                   TEXT NOT NULL,
	narrative                 TEXT,
	decisions                 TEXT,
	pending                   TEXT,
	refs                      TEXT,
	goal                      TEXT,
	next_steps                TEXT,
	critical_context          TEXT,
	token_count               INTEGER NOT NULL,
	file_ids                  TEXT NOT NULL DEFAULT '[]',
	earliest_at               INTEGER,
	latest_at                 INTEGER,
	descendant_count          INTEGER NOT NULL DEFAULT 0,
	descendant_token_count    INTEGER NOT NULL DEFAULT 0,
	source_message_token_count INTEGER NOT NULL DEFAULT 0,
	created_at                INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sum_conv ON summaries(conversation_id);

CREATE TABLE IF NOT EXISTS summary_parents (
	summary_id TEXT NOT NULL,
	parent_id  TEXT NOT NULL,
	PRIMARY KEY (summary_id, parent_id)
);

CREATE TABLE IF NOT EXISTS summary_messages (
	summary_id TEXT NOT NULL,
	message_id INTEGER NOT NULL,
	PRIMARY KEY (summary_id, message_id)
);

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

CREATE TABLE IF NOT EXISTS transferred_summaries (
	summary_id    TEXT PRIMARY KEY,
	transferred_at INTEGER NOT NULL
);
`

// openTestDB creates a temp SQLite database with the aurora schema for tests.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_aurora.db")
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
