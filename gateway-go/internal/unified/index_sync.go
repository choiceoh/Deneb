// memory_index synchronization — keeps the unified index in sync when
// messages, summaries, or facts are inserted or removed.
//
// These methods are called by the unified store adapters after mutations
// to the source tables. The FTS indices are updated in the same transaction
// when possible, or as best-effort otherwise.
package unified

import (
	"database/sql"
	"fmt"
	"time"
)

// IndexMessage adds a message to the unified memory index and FTS.
func (s *Store) IndexMessage(tx *sql.Tx, messageID uint64, content string, createdAt int64) error {
	itemID := fmt.Sprintf("%d", messageID)
	id, err := insertIndex(tx, "message", itemID, "short", 0.0, createdAt)
	if err != nil {
		return err
	}
	insertFTS(tx, id, content)
	return nil
}

// IndexSummary adds a summary to the unified memory index and FTS.
func (s *Store) IndexSummary(tx *sql.Tx, summaryID string, content string, importance float64, createdAt int64) error {
	id, err := insertIndex(tx, "summary", summaryID, "medium", importance, createdAt)
	if err != nil {
		return err
	}
	insertFTS(tx, id, content)
	return nil
}

// IndexFact adds a fact to the unified memory index and FTS.
func (s *Store) IndexFact(tx *sql.Tx, factID int64, content string, importance float64) error {
	itemID := fmt.Sprintf("%d", factID)
	nowMs := time.Now().UnixMilli()
	id, err := insertIndex(tx, "fact", itemID, "long", importance, nowMs)
	if err != nil {
		return err
	}
	insertFTS(tx, id, content)
	return nil
}

// RemoveFromIndex removes an item from the memory index and FTS.
func (s *Store) RemoveFromIndex(tx *sql.Tx, itemType, itemID string) {
	var id int64
	err := tx.QueryRow(
		`SELECT id FROM memory_index WHERE item_type = ? AND item_id = ?`,
		itemType, itemID).Scan(&id)
	if err != nil {
		return
	}

	// Remove from FTS first (needs the rowid).
	var content string
	tx.QueryRow(`SELECT content FROM memory_fts WHERE rowid = ?`, id).Scan(&content)
	if content != "" {
		tx.Exec(`INSERT INTO memory_fts(memory_fts, rowid, content) VALUES('delete', ?, ?)`, id, content)
		tx.Exec(`INSERT INTO memory_fts_trigram(memory_fts_trigram, rowid, content) VALUES('delete', ?, ?)`, id, content)
	}

	tx.Exec(`DELETE FROM embeddings WHERE memory_index_id = ?`, id)
	tx.Exec(`DELETE FROM memory_index WHERE id = ?`, id)
}

// RemoveMessagesFromIndex removes compacted messages from the index.
func (s *Store) RemoveMessagesFromIndex(tx *sql.Tx, messageIDs []uint64) {
	for _, mid := range messageIDs {
		s.RemoveFromIndex(tx, "message", fmt.Sprintf("%d", mid))
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func insertIndex(tx *sql.Tx, itemType, itemID, tier string, importance float64, createdAt int64) (int64, error) {
	result, err := tx.Exec(
		`INSERT OR IGNORE INTO memory_index (item_type, item_id, tier, importance, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		itemType, itemID, tier, importance, createdAt)
	if err != nil {
		return 0, fmt.Errorf("insert index: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil || id == 0 {
		// Already existed — fetch the existing ID.
		tx.QueryRow(
			`SELECT id FROM memory_index WHERE item_type = ? AND item_id = ?`,
			itemType, itemID).Scan(&id)
	}
	return id, nil
}

func insertFTS(tx *sql.Tx, indexID int64, content string) {
	if content == "" || indexID == 0 {
		return
	}
	tx.Exec(`INSERT OR REPLACE INTO memory_fts(rowid, content) VALUES (?, ?)`, indexID, content)
	tx.Exec(`INSERT OR REPLACE INTO memory_fts_trigram(rowid, content) VALUES (?, ?)`, indexID, content)
}
