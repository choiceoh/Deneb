package unified

import (
	"database/sql"
	"fmt"
	"log/slog"
)

// migrateDreamingLogColumns adds user_model_updated and mutual_updated columns
// to dreaming_log for databases created before these columns existed.
// Idempotent — ALTER TABLE ADD COLUMN is a no-op if the column already exists
// (SQLite returns "duplicate column name" which we ignore).
func migrateDreamingLogColumns(db *sql.DB) {
	db.Exec(`ALTER TABLE dreaming_log ADD COLUMN user_model_updated INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE dreaming_log ADD COLUMN mutual_updated INTEGER NOT NULL DEFAULT 0`)
}

// migrateEntityConstraint fixes the entities CHECK constraint for databases
// created before 'unknown' was added to the allowed entity_type set.
// SQLite cannot ALTER a CHECK constraint, so we recreate the table.
// Idempotent — skipped if the constraint already includes 'unknown'.
func migrateEntityConstraint(db *sql.DB) {
	// Probe: if inserting 'unknown' works, the constraint is already correct.
	var hasUnknown bool
	row := db.QueryRow(`SELECT COUNT(*) > 0 FROM pragma_table_info('entities') WHERE name = 'entity_type'`)
	if err := row.Scan(&hasUnknown); err != nil || !hasUnknown {
		return // entities table doesn't exist or has no entity_type column
	}

	// Try a no-op insert with entity_type='unknown' to test the constraint.
	// Use a savepoint so a constraint failure doesn't abort any outer transaction.
	_, err := db.Exec(`SAVEPOINT entity_check_probe`)
	if err != nil {
		return
	}
	_, probeErr := db.Exec(`INSERT INTO entities (name, entity_type, first_seen, last_seen) VALUES ('__probe__', 'unknown', '', '')`)
	if probeErr == nil {
		// Constraint allows 'unknown' — clean up probe row and return.
		_, _ = db.Exec(`DELETE FROM entities WHERE name = '__probe__'`)
		_, _ = db.Exec(`RELEASE entity_check_probe`)
		return
	}
	_, _ = db.Exec(`ROLLBACK TO entity_check_probe`)
	_, _ = db.Exec(`RELEASE entity_check_probe`)

	// Constraint rejects 'unknown' — recreate the table with the fixed constraint.
	_, _ = db.Exec(`PRAGMA foreign_keys = OFF`)
	_, _ = db.Exec(`CREATE TABLE IF NOT EXISTS entities_v6 (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		name          TEXT NOT NULL UNIQUE,
		entity_type   TEXT NOT NULL DEFAULT 'unknown' CHECK(entity_type IN ('person','project','tool','system','concept','organization','unknown')),
		first_seen    TEXT NOT NULL,
		last_seen     TEXT NOT NULL,
		mention_count INTEGER NOT NULL DEFAULT 1
	)`)
	_, _ = db.Exec(`INSERT OR IGNORE INTO entities_v6 SELECT * FROM entities`)
	_, _ = db.Exec(`DROP TABLE IF EXISTS entities`)
	_, _ = db.Exec(`ALTER TABLE entities_v6 RENAME TO entities`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_entities_name ON entities(name)`)
	_, _ = db.Exec(`PRAGMA foreign_keys = ON`)
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

	logger.Debug("memory index reconciled", "indexed", indexed)
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
