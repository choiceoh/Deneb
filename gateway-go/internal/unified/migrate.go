package unified

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// migrateSchema applies incremental schema changes for existing unified
// databases that were created with an older schema version.
func (s *Store) migrateSchema() error {
	// summaries: add structured section columns (Phase 2).
	stmts := []string{
		`ALTER TABLE summaries ADD COLUMN narrative TEXT`,
		`ALTER TABLE summaries ADD COLUMN decisions TEXT`,
		`ALTER TABLE summaries ADD COLUMN pending TEXT`,
		`ALTER TABLE summaries ADD COLUMN refs TEXT`,
		`ALTER TABLE summaries ADD COLUMN importance REAL NOT NULL DEFAULT 0.3`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil && !isIgnorableAlterError(err) {
			return fmt.Errorf("schema migration failed (%s): %w", stmt, err)
		}
	}

	// Knowledge graph tables (fact_relations, entities, fact_entities).
	graphDDL := []string{
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
			entity_type   TEXT NOT NULL DEFAULT 'unknown' CHECK(entity_type IN ('person','project','tool','system','concept','organization')),
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
	for _, ddl := range graphDDL {
		if _, err := s.db.Exec(ddl); err != nil {
			return fmt.Errorf("schema migration failed (%s): %w", ddl, err)
		}
	}

	return nil
}

func (s *Store) repairMemoryIndex() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin memory index repair: %w", err)
	}
	defer tx.Rollback()

	if err := buildMemoryIndex(tx, s.logger); err != nil {
		return fmt.Errorf("repair memory index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit memory index repair: %w", err)
	}

	InvalidateTier1Cache()
	return nil
}

// migrateFromLegacy detects legacy aurora.db and memory.db files and
// imports their data into the unified store. After successful migration,
// legacy files are renamed to *.migrated.
//
// This is idempotent: if the unified DB already has data from both
// sources, migration is skipped.
func (s *Store) migrateFromLegacy(dir string) error {
	auroraPath := filepath.Join(dir, "aurora.db")
	memoryDir := filepath.Join(dir, "memory")
	memoryPath := filepath.Join(memoryDir, "memory.db")

	auroraExists := fileExists(auroraPath)
	memoryExists := fileExists(memoryPath)

	if !auroraExists && !memoryExists {
		return nil
	}

	// Check if we already migrated (have data from both sources).
	var msgCount, factCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&msgCount)
	s.db.QueryRow(`SELECT COUNT(*) FROM facts`).Scan(&factCount)

	if msgCount > 0 && factCount > 0 {
		s.logger.Debug("unified store: migration skipped, already has data from both sources")
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}
	defer tx.Rollback()

	if auroraExists && msgCount == 0 {
		if err := migrateAurora(tx, auroraPath, s.logger); err != nil {
			return fmt.Errorf("migrate aurora: %w", err)
		}
	}

	if memoryExists && factCount == 0 {
		if err := migrateMemory(tx, memoryPath, s.logger); err != nil {
			return fmt.Errorf("migrate memory: %w", err)
		}
	}

	// Build the memory_index from imported data.
	if err := buildMemoryIndex(tx, s.logger); err != nil {
		return fmt.Errorf("build memory index: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}

	// Rename legacy files so we don't re-migrate.
	if auroraExists {
		renameLegacy(auroraPath, s.logger)
	}
	if memoryExists {
		renameLegacy(memoryPath, s.logger)
	}

	s.logger.Info("unified store: legacy migration complete")
	return nil
}

// migrateAurora imports all data from a legacy aurora.db.
func migrateAurora(tx *sql.Tx, dbPath string, logger *slog.Logger) error {
	logger.Info("unified store: migrating aurora.db", "path", dbPath)

	src, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open aurora.db: %w", err)
	}
	defer src.Close()

	// sequences
	rows, err := src.Query(`SELECT name, value FROM sequences`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			var value int64
			rows.Scan(&name, &value)
			tx.Exec(`INSERT OR REPLACE INTO sequences (name, value) VALUES (?, ?)`, name, value)
		}
	}

	// messages
	msgCount, err := copyTable(tx, src, "messages",
		`SELECT message_id, conversation_id, seq, role, content, token_count, created_at FROM messages`,
		`INSERT OR IGNORE INTO messages (message_id, conversation_id, seq, role, content, token_count, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		7)
	if err != nil {
		return fmt.Errorf("copy messages: %w", err)
	}

	// summaries (without structured columns — they don't exist in legacy)
	sumCount, err := copyTable(tx, src, "summaries",
		`SELECT summary_id, conversation_id, kind, depth, content, token_count, file_ids, earliest_at, latest_at, descendant_count, descendant_token_count, source_message_token_count, created_at FROM summaries`,
		`INSERT OR IGNORE INTO summaries (summary_id, conversation_id, kind, depth, content, token_count, file_ids, earliest_at, latest_at, descendant_count, descendant_token_count, source_message_token_count, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		13)
	if err != nil {
		return fmt.Errorf("copy summaries: %w", err)
	}

	// context_items
	if _, err := copyTable(tx, src, "context_items",
		`SELECT conversation_id, ordinal, item_type, message_id, summary_id, created_at FROM context_items`,
		`INSERT OR IGNORE INTO context_items (conversation_id, ordinal, item_type, message_id, summary_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		6); err != nil {
		return fmt.Errorf("copy context_items: %w", err)
	}

	// summary_parents
	if _, err := copyTable(tx, src, "summary_parents",
		`SELECT summary_id, parent_id FROM summary_parents`,
		`INSERT OR IGNORE INTO summary_parents (summary_id, parent_id) VALUES (?, ?)`,
		2); err != nil {
		return fmt.Errorf("copy summary_parents: %w", err)
	}

	// summary_messages
	if _, err := copyTable(tx, src, "summary_messages",
		`SELECT summary_id, message_id FROM summary_messages`,
		`INSERT OR IGNORE INTO summary_messages (summary_id, message_id) VALUES (?, ?)`,
		2); err != nil {
		return fmt.Errorf("copy summary_messages: %w", err)
	}

	// compaction_events
	if _, err := copyTable(tx, src, "compaction_events",
		`SELECT conversation_id, pass, level, tokens_before, tokens_after, created_summary_id, created_at FROM compaction_events`,
		`INSERT INTO compaction_events (conversation_id, pass, level, tokens_before, tokens_after, created_summary_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		7); err != nil {
		return fmt.Errorf("copy compaction_events: %w", err)
	}

	// transferred_summaries
	if _, err := copyTable(tx, src, "transferred_summaries",
		`SELECT summary_id, transferred_at FROM transferred_summaries`,
		`INSERT OR IGNORE INTO transferred_summaries (summary_id, transferred_at) VALUES (?, ?)`,
		2); err != nil {
		return fmt.Errorf("copy transferred_summaries: %w", err)
	}

	logger.Info("unified store: aurora migration done", "messages", msgCount, "summaries", sumCount)
	return nil
}

// migrateMemory imports all data from a legacy memory.db using ATTACH.
// This is more efficient than opening a separate connection.
func migrateMemory(tx *sql.Tx, dbPath string, logger *slog.Logger) error {
	logger.Info("unified store: migrating memory.db", "path", dbPath)

	// ATTACH the memory DB to the current transaction's connection.
	if _, err := tx.Exec(`ATTACH DATABASE ? AS memdb`, dbPath); err != nil {
		// Fall back to row-by-row copy if ATTACH fails.
		logger.Warn("unified store: ATTACH failed, using row-by-row copy", "error", err)
		return migrateMemoryRowByRow(tx, dbPath, logger)
	}
	defer tx.Exec(`DETACH DATABASE memdb`)

	// Bulk copy using INSERT ... SELECT (much faster than row-by-row).
	tables := []struct {
		name  string
		query string
	}{
		{"facts", `INSERT OR IGNORE INTO main.facts SELECT * FROM memdb.facts`},
		{"fact_embeddings", `INSERT OR IGNORE INTO main.fact_embeddings SELECT * FROM memdb.fact_embeddings`},
		{"user_model", `INSERT OR REPLACE INTO main.user_model SELECT * FROM memdb.user_model`},
		{"dreaming_log", `INSERT INTO main.dreaming_log (ran_at, facts_verified, facts_merged, facts_expired, facts_pruned, patterns_extracted, duration_ms) SELECT ran_at, facts_verified, facts_merged, facts_expired, facts_pruned, patterns_extracted, duration_ms FROM memdb.dreaming_log`},
		{"metadata", `INSERT OR REPLACE INTO main.metadata SELECT * FROM memdb.metadata`},
	}

	for _, t := range tables {
		if _, err := tx.Exec(t.query); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "no such table") {
				logger.Debug("unified store: skip table (missing in legacy db)", "table", t.name, "error", err)
				continue
			}
			return fmt.Errorf("copy %s via attach: %w", t.name, err)
		}
	}

	// Rebuild FTS indices from facts data (FTS5 virtual tables can't be copied via ATTACH).
	if _, err := tx.Exec(`INSERT INTO facts_fts(rowid, content, category) SELECT id, content, category FROM facts WHERE active = 1`); err != nil {
		return fmt.Errorf("rebuild facts_fts after attach copy: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO facts_fts_trigram(rowid, content) SELECT id, content FROM facts WHERE active = 1`); err != nil {
		return fmt.Errorf("rebuild facts_fts_trigram after attach copy: %w", err)
	}

	var factCount int
	tx.QueryRow(`SELECT COUNT(*) FROM facts`).Scan(&factCount)
	logger.Info("unified store: memory migration done", "facts", factCount)
	return nil
}

// migrateMemoryRowByRow is the fallback when ATTACH is not available.
func migrateMemoryRowByRow(tx *sql.Tx, dbPath string, logger *slog.Logger) error {
	src, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open memory.db: %w", err)
	}
	defer src.Close()

	if _, err := copyTable(tx, src, "facts",
		`SELECT id, content, category, importance, source, created_at, updated_at, last_accessed_at, access_count, verified_at, expires_at, superseded_by, active, merge_depth FROM facts`,
		`INSERT OR IGNORE INTO facts (id, content, category, importance, source, created_at, updated_at, last_accessed_at, access_count, verified_at, expires_at, superseded_by, active, merge_depth) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		14); err != nil {
		return fmt.Errorf("copy facts: %w", err)
	}
	if _, err := copyTable(tx, src, "fact_embeddings",
		`SELECT fact_id, embedding, model_name, updated_at FROM fact_embeddings`,
		`INSERT OR IGNORE INTO fact_embeddings (fact_id, embedding, model_name, updated_at) VALUES (?, ?, ?, ?)`,
		4); err != nil {
		return fmt.Errorf("copy fact_embeddings: %w", err)
	}
	if _, err := copyTable(tx, src, "user_model",
		`SELECT key, value, confidence, updated_at FROM user_model`,
		`INSERT OR REPLACE INTO user_model (key, value, confidence, updated_at) VALUES (?, ?, ?, ?)`,
		4); err != nil {
		return fmt.Errorf("copy user_model: %w", err)
	}
	if _, err := copyTable(tx, src, "metadata",
		`SELECT key, value FROM metadata`,
		`INSERT OR REPLACE INTO metadata (key, value) VALUES (?, ?)`,
		2); err != nil {
		return fmt.Errorf("copy metadata: %w", err)
	}

	return nil
}

// buildMemoryIndex populates the memory_index table and FTS indices from
// the existing messages, summaries, and facts tables.
func buildMemoryIndex(tx *sql.Tx, logger *slog.Logger) error {
	// Rebuild the cross-tier index from source-of-truth tables. This is cheap on
	// startup for single-user databases and guarantees recovery from any stale or
	// missing rows that predate the DB triggers.
	if _, err := tx.Exec(`DELETE FROM memory_index WHERE item_type IN ('message', 'summary', 'fact')`); err != nil {
		return fmt.Errorf("clear memory index: %w", err)
	}

	inserts := []struct {
		name string
		sql  string
	}{
		{
			name: "messages",
			sql: `INSERT INTO memory_index (item_type, item_id, tier, importance, created_at, updated_at)
				SELECT 'message', CAST(message_id AS TEXT), 'short', 0.0, created_at, NULL
				FROM messages`,
		},
		{
			name: "summaries",
			sql: `INSERT INTO memory_index (item_type, item_id, tier, importance, created_at, updated_at)
				SELECT 'summary', summary_id, 'medium', COALESCE(importance, 0.3), created_at, NULL
				FROM summaries`,
		},
		{
			name: "facts",
			sql: `INSERT INTO memory_index (item_type, item_id, tier, importance, created_at, updated_at)
				SELECT
					'fact',
					CAST(id AS TEXT),
					'long',
					importance,
					COALESCE(
						CASE
							WHEN created_at GLOB '[0-9]*' THEN CAST(created_at AS INTEGER)
							WHEN strftime('%s', created_at) IS NOT NULL THEN CAST(strftime('%s', created_at) AS INTEGER) * 1000
						END,
						CAST(strftime('%s', 'now') AS INTEGER) * 1000
					),
					CASE
						WHEN updated_at GLOB '[0-9]*' THEN CAST(updated_at AS INTEGER)
						WHEN strftime('%s', updated_at) IS NOT NULL THEN CAST(strftime('%s', updated_at) AS INTEGER) * 1000
					END
				FROM facts
				WHERE active = 1`,
		},
	}
	for _, stmt := range inserts {
		if _, err := tx.Exec(stmt.sql); err != nil {
			return fmt.Errorf("insert %s index rows: %w", stmt.name, err)
		}
	}

	// Populate unified FTS from indexed items.
	// We need to join memory_index with source tables to get content.
	if err := rebuildFTS(tx); err != nil {
		return fmt.Errorf("rebuild FTS: %w", err)
	}

	var indexed int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM memory_index`).Scan(&indexed); err != nil {
		return fmt.Errorf("count memory index rows: %w", err)
	}

	logger.Info("unified store: memory index reconciled", "indexed", indexed)
	return nil
}

// rebuildFTS rebuilds the unified FTS indices from scratch.
func rebuildFTS(tx *sql.Tx) error {
	// Clear existing FTS data.
	if _, err := tx.Exec(`INSERT INTO memory_fts(memory_fts) VALUES('delete-all')`); err != nil {
		return fmt.Errorf("clear memory_fts: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO memory_fts_trigram(memory_fts_trigram) VALUES('delete-all')`); err != nil {
		return fmt.Errorf("clear memory_fts_trigram: %w", err)
	}

	// Insert message content.
	_, err := tx.Exec(`
		INSERT INTO memory_fts(rowid, content)
		SELECT mi.id, m.content
		FROM memory_index mi
		JOIN messages m ON CAST(m.message_id AS TEXT) = mi.item_id
		WHERE mi.item_type = 'message'
	`)
	if err != nil {
		return fmt.Errorf("FTS messages: %w", err)
	}

	// Insert summary content (use narrative if available, else full content).
	_, err = tx.Exec(`
		INSERT INTO memory_fts(rowid, content)
		SELECT mi.id, COALESCE(s.narrative, s.content)
		FROM memory_index mi
		JOIN summaries s ON s.summary_id = mi.item_id
		WHERE mi.item_type = 'summary'
	`)
	if err != nil {
		return fmt.Errorf("FTS summaries: %w", err)
	}

	// Insert fact content.
	_, err = tx.Exec(`
		INSERT INTO memory_fts(rowid, content)
		SELECT mi.id, f.content
		FROM memory_index mi
		JOIN facts f ON CAST(f.id AS TEXT) = mi.item_id
		WHERE mi.item_type = 'fact'
	`)
	if err != nil {
		return fmt.Errorf("FTS facts: %w", err)
	}

	// Repeat for trigram index.
	if _, err = tx.Exec(`
		INSERT INTO memory_fts_trigram(rowid, content)
		SELECT mi.id, m.content
		FROM memory_index mi
		JOIN messages m ON CAST(m.message_id AS TEXT) = mi.item_id
		WHERE mi.item_type = 'message'
	`); err != nil {
		return fmt.Errorf("FTS trigram messages: %w", err)
	}
	if _, err = tx.Exec(`
		INSERT INTO memory_fts_trigram(rowid, content)
		SELECT mi.id, COALESCE(s.narrative, s.content)
		FROM memory_index mi
		JOIN summaries s ON s.summary_id = mi.item_id
		WHERE mi.item_type = 'summary'
	`); err != nil {
		return fmt.Errorf("FTS trigram summaries: %w", err)
	}
	if _, err = tx.Exec(`
		INSERT INTO memory_fts_trigram(rowid, content)
		SELECT mi.id, f.content
		FROM memory_index mi
		JOIN facts f ON CAST(f.id AS TEXT) = mi.item_id
		WHERE mi.item_type = 'fact'
	`); err != nil {
		return fmt.Errorf("FTS trigram facts: %w", err)
	}

	return nil
}

// copyTable copies rows from src to dst using the given queries.
// Returns the number of rows copied.
func copyTable(tx *sql.Tx, src *sql.DB, tableName, selectSQL, insertSQL string, colCount int) (int, error) {
	rows, err := src.Query(selectSQL)
	if err != nil {
		// Table might not exist in legacy DB — that's OK.
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return 0, nil
		}
		return 0, fmt.Errorf("query %s: %w", tableName, err)
	}
	defer rows.Close()

	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return 0, fmt.Errorf("prepare insert for %s: %w", tableName, err)
	}
	defer stmt.Close()

	count := 0
	for rows.Next() {
		vals := make([]any, colCount)
		ptrs := make([]any, colCount)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return count, fmt.Errorf("scan row %d in %s: %w", count+1, tableName, err)
		}
		if _, err := stmt.Exec(vals...); err != nil {
			return count, fmt.Errorf("insert row %d in %s: %w", count+1, tableName, err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, fmt.Errorf("iterate %s: %w", tableName, err)
	}
	return count, nil
}

func isIgnorableAlterError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column name") ||
		strings.Contains(msg, "already exists")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func renameLegacy(path string, logger *slog.Logger) {
	dest := path + ".migrated"
	if err := os.Rename(path, dest); err != nil {
		logger.Warn("unified store: could not rename legacy file", "path", path, "error", err)
	} else {
		logger.Info("unified store: renamed legacy file", "from", path, "to", dest)
	}
}
